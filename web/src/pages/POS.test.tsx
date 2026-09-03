import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { openDB } from 'idb'
import POS from './POS'
import type { MedicineWithBatches } from '../types'

afterEach(cleanup)

async function seedCache(meds: MedicineWithBatches[]) {
  const db = await openDB('pms-cache', 2, {
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
  for (const m of meds) await db.put('medicines_cache', m)
}

const med: MedicineWithBatches = {
  id: 'm1',
  name: 'TestMed 500 Tablet',
  salt_composition: 'Paracetamol 500mg',
  manufacturer: 'TestPharma',
  min_reorder_level: 5,
  packing: '',
  uqc: 'TAB',
  batches: [
    { id: 'b1', medicine_id: 'm1', batch_number: 'KEEP-B1', expiry_date: '2027-06-01', purchase_price: 10, sale_price: 15, current_stock: 50 },
    { id: 'b2', medicine_id: 'm1', batch_number: 'EARLY-B2', expiry_date: '2026-12-01', purchase_price: 10, sale_price: 14, current_stock: 30 },
  ],
}

describe('POS batch picker', () => {
  it('lists the synced catalog immediately, before any typing', async () => {
    await seedCache([med])
    render(<POS cacheVersion={1} />)

    expect(await screen.findByText('TestMed 500 Tablet')).toBeInTheDocument()
    expect(screen.getByText(/in catalog/i)).toBeInTheDocument()
    expect(screen.queryByText(/local cache is empty/i)).not.toBeInTheDocument()
  })

  it('opens the popup when a medicine is chosen with Enter', async () => {
    await seedCache([med])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    const box = await screen.findByPlaceholderText(/Search brand or salt/i)
    await user.type(box, 'testmed')
    await user.keyboard('{Enter}')

    expect(await screen.findByText(/batches ranked by nearest expiry/i)).toBeInTheDocument()
  })

  it('opens the popup on mouse click too', async () => {
    await seedCache([med])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    const box = await screen.findByPlaceholderText(/Search brand or salt/i)
    await user.type(box, 'testmed')
    await user.click(await screen.findByText('TestMed 500 Tablet'))

    expect(await screen.findByText(/batches ranked by nearest expiry/i)).toBeInTheDocument()
  })

  it('adds the FEFO-highlighted batch on Enter and closes the popup', async () => {
    await seedCache([med])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    const box = await screen.findByPlaceholderText(/Search brand or salt/i)
    await user.type(box, 'testmed')
    await user.keyboard('{Enter}')
    await screen.findByText(/batches ranked by nearest expiry/i)

    await user.keyboard('{Enter}')
    expect(await screen.findByText(/Batch EARLY-B2/)).toBeInTheDocument()
    expect(screen.queryByText(/batches ranked by nearest expiry/i)).not.toBeInTheDocument()
  })

  it('quick-adds a specific batch with number keys', async () => {
    await seedCache([med])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    const box = await screen.findByPlaceholderText(/Search brand or salt/i)
    await user.type(box, 'testmed')
    await user.keyboard('{Enter}')
    await screen.findByText(/batches ranked by nearest expiry/i)

    await user.keyboard('{2}')
    expect(await screen.findByText(/Batch KEEP-B1/)).toBeInTheDocument()
  })
})
