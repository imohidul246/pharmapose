import { beforeEach, describe, expect, it, vi } from 'vitest'
import { openDB } from 'idb'
import {
  getCachedMedicineTax,
  loadCachedHSNCodes,
  loadCachedMedicineTaxConfigs,
  loadCachedMedicines,
  syncLocalCache,
  upsertCachedHSNWithRate,
  upsertCachedMedicineTax,
} from './db'
import type { HSNWithRate, MedicineTaxConfig } from '../types'

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
}

const hsn: HSNWithRate = {
  id: 'hsn1',
  code: '3004',
  description: 'Medicaments',
  created_at: '2026-01-01T00:00:00Z',
  gst_rate: 12,
  cgst_rate: 6,
  sgst_rate: 6,
  igst_rate: 12,
  cess_rate: 0,
}

const taxCfg: MedicineTaxConfig = {
  id: 'cfg1',
  medicine_id: 'm1',
  hsn_code_id: 'hsn1',
  tax_rate_id: 'tr1',
  price_includes_tax: true,
  effective_from: '2026-01-01T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  hsn_code: '3004',
}

const medicine = {
  id: 'm1',
  name: 'Paracetamol 500mg',
  salt_composition: 'Paracetamol 500mg',
  manufacturer: 'Pharma',
  min_reorder_level: 5,
  packing: '',
  uqc: 'TAB',
  batches: [],
}

beforeEach(async () => {
  vi.restoreAllMocks()
  const db = await openDB('pms-cache', 2, {
    upgrade(db) {
      if (!db.objectStoreNames.contains('medicines_cache')) db.createObjectStore('medicines_cache', { keyPath: 'id' })
      if (!db.objectStoreNames.contains('customers_cache')) {
        const store = db.createObjectStore('customers_cache', { keyPath: 'id' })
        store.createIndex('by-name', 'name')
      }
      if (!db.objectStoreNames.contains('hsn_codes_cache')) {
        const store = db.createObjectStore('hsn_codes_cache', { keyPath: 'id' })
        store.createIndex('by-code', 'code')
      }
      if (!db.objectStoreNames.contains('medicine_tax_cache')) db.createObjectStore('medicine_tax_cache', { keyPath: 'medicine_id' })
    },
  })
  for (const store of ['medicines_cache' as const, 'customers_cache' as const, 'hsn_codes_cache' as const, 'medicine_tax_cache' as const]) {
    await db.clear(store)
  }
  db.close()
})

describe('syncLocalCache', () => {
  it('pulls inventory, customers and the HSN/tax snapshot into all caches', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/sync/inventory')) return json({ synced_at: '2026-01-01T00:00:00Z', medicines: [medicine] })
      if (url.includes('/api/sync/customers')) return json({ customers: [] })
      if (url.includes('/api/sync/tax')) return json({ synced_at: '2026-01-01T00:00:00Z', hsn_codes: [hsn], tax_configs: [taxCfg] })
      throw new Error(`unexpected fetch: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await syncLocalCache()

    expect(result.hsnCount).toBe(1)
    expect(result.taxConfigCount).toBe(1)
    expect(result.medicineCount).toBe(1)
    expect(fetchMock).toHaveBeenCalledTimes(3)

    const meds = await loadCachedMedicines()
    expect(meds).toHaveLength(1)
    expect(meds[0].id).toBe('m1')

    const hsns = await loadCachedHSNCodes()
    expect(hsns).toHaveLength(1)
    expect(hsns[0].code).toBe('3004')

    const cfgs = await loadCachedMedicineTaxConfigs()
    expect(cfgs).toHaveLength(1)
    expect(cfgs[0].medicine_id).toBe('m1')

    const single = await getCachedMedicineTax('m1')
    expect(single?.hsn_code).toBe('3004')
    expect(await getCachedMedicineTax('missing')).toBeNull()
  })

  it('throws when the tax sync endpoint fails', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/sync/inventory')) return json({ synced_at: 'x', medicines: [] })
      if (url.includes('/api/sync/customers')) return json({ customers: [] })
      if (url.includes('/api/sync/tax')) return json({ error: 'nope' }, 500)
      throw new Error(`unexpected fetch: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(syncLocalCache()).rejects.toThrow('nope')
  })
})

describe('tax cache upserts', () => {
  it('stores and retrieves an HSN with its active rates', async () => {
    await upsertCachedHSNWithRate(hsn)
    const hsns = await loadCachedHSNCodes()
    expect(hsns).toHaveLength(1)
    expect(hsns[0].gst_rate).toBe(12)
  })

  it('stores and retrieves a medicine tax config', async () => {
    await upsertCachedMedicineTax(taxCfg)
    const cfg = await getCachedMedicineTax('m1')
    expect(cfg?.tax_rate_id).toBe('tr1')
    const all = await loadCachedMedicineTaxConfigs()
    expect(all).toHaveLength(1)
  })
})
