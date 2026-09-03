import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { openDB } from 'idb'
import TaxEditor from './TaxEditor'
import { getCachedMedicineTax } from '../lib/db'
import type { HSNWithRate, MedicineTaxConfig, TaxRate } from '../types'

/**
 * Regression test for the HSN reassignment (Operation B) flow on the frontend:
 * after a medicine is reassigned to a *different* HSN and saved, the local
 * IndexedDB medicine_tax_cache and the TaxEditor onSaved callback must reflect
 * the NEW HSN — never the old one. This is the client-side contract that makes
 * the Medicine / Purchase / Billing pages display the reassigned HSN.
 */

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
}

const hsnA: HSNWithRate = {
  id: 'hsn-A',
  code: '3004',
  description: 'Original HSN',
  created_at: '2026-01-01T00:00:00Z',
  gst_rate: 12,
  cgst_rate: 6,
  sgst_rate: 6,
  igst_rate: 12,
  cess_rate: 0,
}
const hsnB: HSNWithRate = {
  id: 'hsn-B',
  code: '3005',
  description: 'Reassigned HSN',
  created_at: '2026-01-01T00:00:00Z',
  gst_rate: 18,
  cgst_rate: 9,
  sgst_rate: 9,
  igst_rate: 18,
  cess_rate: 0,
}

const oldCfg: MedicineTaxConfig = {
  id: 'cfg-old',
  medicine_id: 'med-1',
  hsn_code_id: hsnA.id,
  tax_rate_id: 'tr-old',
  price_includes_tax: true,
  effective_from: '2026-01-01T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  hsn_code: hsnA.code,
}

const savedTaxRate: TaxRate = {
  id: 'tr-new',
  hsn_code_id: hsnB.id,
  gst_rate: 18,
  cgst_rate: 9,
  sgst_rate: 9,
  igst_rate: 18,
  cess_rate: 0,
  effective_from: '2026-08-30T00:00:00Z',
  created_at: '2026-08-30T00:00:00Z',
}

const savedCfg: MedicineTaxConfig = {
  id: 'cfg-new',
  medicine_id: 'med-1',
  hsn_code_id: hsnB.id,
  tax_rate_id: savedTaxRate.id,
  price_includes_tax: true,
  effective_from: '2026-08-30T00:00:00Z',
  created_at: '2026-08-30T00:00:00Z',
}

// fetch mock for TaxEditor: list HSNs on mount, then the save flow.
function installFetchMock() {
  const calls: Array<{ method?: string; url: string; body?: unknown }> = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (method === 'GET' && url.includes('/api/hsn')) {
      return json({ hsn_codes: [{ id: hsnA.id, code: hsnA.code, description: hsnA.description, created_at: hsnA.created_at }, { id: hsnB.id, code: hsnB.code, description: hsnB.description, created_at: hsnB.created_at }] })
    }
    if (method === 'PUT' && url.includes('/api/hsn/') && url.includes('/tax-rate')) {
      return json(savedTaxRate)
    }
    if (method === 'PUT' && url.includes('/api/medicines/') && url.includes('/tax-config')) {
      return json(savedCfg)
    }
    throw new Error(`unexpected fetch: ${method} ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, calls }
}

function clearCacheStores() {
  return openDB('pms-cache', 2, {
    upgrade(db) {
      if (!db.objectStoreNames.contains('medicine_tax_cache')) db.createObjectStore('medicine_tax_cache', { keyPath: 'medicine_id' })
      if (!db.objectStoreNames.contains('hsn_codes_cache')) {
        const store = db.createObjectStore('hsn_codes_cache', { keyPath: 'id' })
        store.createIndex('by-code', 'code')
      }
    },
  }).then(async (db) => {
    await db.clear('medicine_tax_cache')
    await db.clear('hsn_codes_cache')
    db.close()
  })
}

describe('TaxEditor HSN reassignment (Operation B)', () => {
  beforeEach(async () => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    await clearCacheStores()
  })

  it('persists the reassigned HSN to the API and the local cache (not the old one)', async () => {
    const user = userEvent.setup()
    const { fetchMock, calls } = installFetchMock()
    const onSaved = vi.fn()
    render(<TaxEditor medicineId="med-1" medicineName="Example Tablet" taxConfig={oldCfg} onClose={() => {}} onSaved={onSaved} />)

    // Let the mount fetch of /api/hsn settle and the HSN options render.
    await screen.findByRole('combobox')
    await screen.findByText(/3005/)

    // Reassign to the OTHER HSN.
    await user.selectOptions(screen.getByRole('combobox'), hsnB.id)
    await user.click(screen.getByRole('button', { name: 'Save tax config' }))

    // API must receive the NEW hsn_code_id.
    const cfgPut = calls.find((c) => c.url.includes('/api/medicines/') && c.url.includes('/tax-config'))
    expect(cfgPut).toBeDefined()
    expect((cfgPut!.body as { hsn_code_id?: string }).hsn_code_id).toBe(hsnB.id)

    // Local cache must map the medicine to the NEW HSN.
    const cached = await getCachedMedicineTax('med-1')
    expect(cached?.hsn_code).toBe(hsnB.code)
    expect(cached?.hsn_code_id).toBe(hsnB.id)
    expect(cached?.hsn_code).not.toBe(hsnA.code)

    // onSaved carries the enriched NEW HSN config.
    expect(onSaved).toHaveBeenCalledTimes(1)
    const saved = onSaved.mock.calls[0][0] as MedicineTaxConfig
    expect(saved.hsn_code).toBe(hsnB.code)
    expect(saved.hsn_code_id).toBe(hsnB.id)

    // Sanity: fetch mock actually exercised the save endpoints.
    expect(fetchMock).toHaveBeenCalled()
  })

  it('does not send the old HSN id when saving a reassignment', async () => {
    const user = userEvent.setup()
    const { calls } = installFetchMock()
    render(<TaxEditor medicineId="med-1" medicineName="Example Tablet" taxConfig={oldCfg} onClose={() => {}} onSaved={() => {}} />)

    await screen.findByRole('combobox')
    await screen.findByText(/3005/)
    await user.selectOptions(screen.getByRole('combobox'), hsnB.id)
    await user.click(screen.getByRole('button', { name: 'Save tax config' }))

    const cfgPut = calls.find((c) => c.url.includes('/api/medicines/') && c.url.includes('/tax-config'))
    expect((cfgPut!.body as { hsn_code_id?: string }).hsn_code_id).not.toBe(hsnA.id)
  })
})
