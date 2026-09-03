import { openDB, type DBSchema, type IDBPDatabase } from 'idb'
import type { Customer, HSNWithRate, MedicineTaxConfig, MedicineWithBatches, SyncInventoryResponse, SyncTaxResponse } from '../types'

interface PMSDB extends DBSchema {
  medicines_cache: {
    key: string
    value: MedicineWithBatches
  }
  customers_cache: {
    key: string
    value: Customer
    indexes: { 'by-name': string }
  }
  hsn_codes_cache: {
    key: string
    value: HSNWithRate
    indexes: { 'by-code': string }
  }
  medicine_tax_cache: {
    key: string
    value: MedicineTaxConfig
  }
}

const DB_NAME = 'pms-cache'
const DB_VERSION = 2

let dbPromise: Promise<IDBPDatabase<PMSDB>> | null = null

function getDB(): Promise<IDBPDatabase<PMSDB>> {
  if (!dbPromise) {
    dbPromise = openDB<PMSDB>(DB_NAME, DB_VERSION, {
      upgrade(db) {
        if (!db.objectStoreNames.contains('medicines_cache')) {
          db.createObjectStore('medicines_cache', { keyPath: 'id' })
        }
        if (!db.objectStoreNames.contains('customers_cache')) {
          const store = db.createObjectStore('customers_cache', { keyPath: 'id' })
          store.createIndex('by-name', 'name')
        }
        if (!db.objectStoreNames.contains('hsn_codes_cache')) {
          const store = db.createObjectStore('hsn_codes_cache', { keyPath: 'id' })
          store.createIndex('by-code', 'code')
        }
        if (!db.objectStoreNames.contains('medicine_tax_cache')) {
          db.createObjectStore('medicine_tax_cache', { keyPath: 'medicine_id' })
        }
      },
    })
  }
  return dbPromise
}

export interface SyncResult {
  syncedAt: Date
  medicineCount: number
  customerCount: number
  hsnCount: number
  taxConfigCount: number
}

/**
 * Pulls the full inventory, customer and HSN/tax snapshot from the server and
 * replaces the local IndexedDB caches. Executed on the login lifecycle and via
 * manual refresh. All three endpoints are store-scoped server-side, so the
 * cached HSN/tax data is isolated per store.
 */
export async function syncLocalCache(): Promise<SyncResult> {
  const [invRes, custRes, taxRes] = await Promise.all([
    fetch('/api/sync/inventory'),
    fetch('/api/sync/customers'),
    fetch('/api/sync/tax'),
  ])
  if (!invRes.ok) throw new Error(await readError(invRes))
  if (!custRes.ok) throw new Error(await readError(custRes))
  if (!taxRes.ok) throw new Error(await readError(taxRes))

  const inv = (await invRes.json()) as SyncInventoryResponse
  const customers = ((await custRes.json()) as { customers: Customer[] }).customers
  const tax = (await taxRes.json()) as SyncTaxResponse

  const db = await getDB()
  const txM = db.transaction('medicines_cache', 'readwrite')
  await txM.store.clear()
  for (const m of inv.medicines) txM.store.put(m)
  await txM.done

  const txC = db.transaction('customers_cache', 'readwrite')
  await txC.store.clear()
  for (const c of customers) txC.store.put(c)
  await txC.done

  const txH = db.transaction('hsn_codes_cache', 'readwrite')
  await txH.store.clear()
  for (const h of tax.hsn_codes) txH.store.put(h)
  await txH.done

  const txT = db.transaction('medicine_tax_cache', 'readwrite')
  await txT.store.clear()
  for (const cfg of tax.tax_configs) txT.store.put(cfg)
  await txT.done

  return {
    syncedAt: new Date(inv.synced_at),
    medicineCount: inv.medicines.length,
    customerCount: customers.length,
    hsnCount: tax.hsn_codes.length,
    taxConfigCount: tax.tax_configs.length,
  }
}

export async function loadCachedMedicines(): Promise<MedicineWithBatches[]> {
  const db = await getDB()
  return db.getAll('medicines_cache')
}

export async function loadCachedCustomers(): Promise<Customer[]> {
  const db = await getDB()
  return db.getAllFromIndex('customers_cache', 'by-name')
}

/** Returns all cached HSN + active-tax-rate entries, sorted by code. */
export async function loadCachedHSNCodes(): Promise<HSNWithRate[]> {
  const db = await getDB()
  const all = await db.getAll('hsn_codes_cache')
  all.sort((a, b) => a.code.localeCompare(b.code))
  return all
}

/** Returns all cached medicine tax configs keyed by medicine. */
export async function loadCachedMedicineTaxConfigs(): Promise<MedicineTaxConfig[]> {
  const db = await getDB()
  return db.getAll('medicine_tax_cache')
}

/** Returns a single medicine's cached tax config, or null when absent. */
export async function getCachedMedicineTax(medicineId: string): Promise<MedicineTaxConfig | null> {
  const db = await getDB()
  return (await db.get('medicine_tax_cache', medicineId)) ?? null
}

// Adds or replaces entries after an HSN/tax mutation so the dropdown and tax
// auto-fill reflect edits immediately without a full re-sync.
export async function upsertCachedHSNWithRate(h: HSNWithRate): Promise<void> {
  const db = await getDB()
  await db.put('hsn_codes_cache', h)
}

export async function removeCachedHSNWithRate(hsnId: string): Promise<void> {
  const db = await getDB()
  await db.delete('hsn_codes_cache', hsnId)
}

export async function upsertCachedMedicineTax(cfg: MedicineTaxConfig): Promise<void> {
  const db = await getDB()
  await db.put('medicine_tax_cache', cfg)
}

// Adds or replaces a single customer in the cache. Used after inline customer
// creation in the POS flow so a freshly created B2B buyer is available
// immediately (and on future loads) without a full re-sync.
export async function upsertCachedCustomer(c: Customer): Promise<void> {
  const db = await getDB()
  await db.put('customers_cache', c)
}

async function readError(res: Response): Promise<string> {
  try {
    const body = await res.json()
    if (body && typeof body.error === 'string') return body.error
  } catch {
    /* fall through */
  }
  return `request failed (${res.status})`
}
