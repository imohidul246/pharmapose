import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { openDB } from 'idb'
import POS from './POS'
import type { Customer, MedicineWithBatches } from '../types'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

let customers: Customer[]
let checkoutBodies: Record<string, unknown>[] = []

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  checkoutBodies = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/customers') && !init?.method) {
      const params = new URL(url, 'http://localhost').searchParams
      const q = (params.get('search') ?? '').toLowerCase()
      const type = params.get('type')
      const pool = customers.filter(
        (c) =>
          (c.customer_type ?? 'B2C') === type &&
          (c.name.toLowerCase().includes(q) ||
            c.phone.toLowerCase().includes(q) ||
            (!!c.gstin && c.gstin.toLowerCase().includes(q))),
      )
      return json({ customers: pool })
    }
    if (url === '/api/customers' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Customer
      const created: Customer = {
        id: 'c-new',
        name: body.name,
        phone: body.phone,
        credit_limit: body.credit_limit ?? 0,
        current_balance: 0,
        gstin: body.gstin ?? null,
        customer_type: body.customer_type ?? 'B2C',
        state: body.state ?? null,
        state_code: body.state_code ?? null,
        billing_address: body.billing_address ?? null,
        shipping_address: null,
      }
      customers = [created, ...customers]
      return json(created)
    }
    if (url === '/api/sales/checkout') {
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>
      checkoutBodies.push(body)
      return json({
        invoice: {
          id: 'inv1',
          invoice_no: 'S1',
          customer_id: body.customer_id,
          payment_type: body.payment_type,
          total_amount: 14,
          discount_total: 0,
          invoice_date: '2026-08-28',
          financial_year: '2026-27',
          created_at: '',
          sale_type: body.sale_type,
        },
        items: [],
      })
    }
    return json({}, 404)
  }))
})

async function seedCache(meds: MedicineWithBatches[], custs: Customer[]) {
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
  for (const c of custs) await db.put('customers_cache', c)
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

const retail: Customer = {
  id: 'c1',
  name: 'Ramesh Kumar',
  phone: '9876500001',
  credit_limit: 5000,
  current_balance: 200,
  customer_type: 'B2C',
  state: 'Maharashtra',
  state_code: '27',
  gstin: null,
  billing_address: null,
  shipping_address: null,
}

const b2b: Customer = {
  id: 'c2',
  name: 'Apollo Pharmacy Distributors',
  phone: '9876500002',
  credit_limit: 100000,
  current_balance: 0,
  customer_type: 'B2B',
  state: 'Delhi',
  state_code: '07',
  gstin: '07AAPBC1234F1Z5',
  billing_address: 'Connaught Place, Delhi',
  shipping_address: null,
}

async function addOneItem(user: ReturnType<typeof userEvent.setup>) {
  const box = await screen.findByPlaceholderText(/Search brand or salt/i)
  await user.type(box, 'testmed')
  await user.keyboard('{Enter}')
  await screen.findByText(/batches ranked by nearest expiry/i)
  await user.keyboard('{Enter}')
  await screen.findByText(/Batch EARLY-B2/)
}

describe('POS customer flow', () => {
  it('blocks a credit sale until a customer is chosen, with a clear message', async () => {
    await seedCache([med], [])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    await addOneItem(user)
    await user.click(screen.getByRole('button', { name: /Credit \/ Udhaar/i }))

    const complete = screen.getByRole('button', { name: /Complete Sale —/i })
    expect(complete).toBeDisabled()
    expect(
      screen.getByText(/Customer is required for credit sales — select or create one above/i),
    ).toBeInTheDocument()
    expect(checkoutBodies).toHaveLength(0)
  })

  it('search-selects a retail customer, keeps the cart, and sends place of supply', async () => {
    customers = [retail]
    await seedCache([med], [retail])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    await addOneItem(user)
    await user.click(screen.getByRole('button', { name: /Credit \/ Udhaar/i }))

    await user.type(
      screen.getByPlaceholderText(/Search retail customer by name or phone/i),
      'ramesh',
    )
    await user.click(await screen.findByText('Ramesh Kumar'))

    expect(screen.getByText('Ramesh Kumar')).toBeInTheDocument()
    expect(screen.getByText(/Batch EARLY-B2/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Complete Sale —/i }))
    await screen.findByText('S1')

    expect(checkoutBodies).toHaveLength(1)
    expect(checkoutBodies[0]).toMatchObject({
      customer_id: 'c1',
      payment_type: 'CREDIT',
      place_of_supply: '27',
      sale_type: 'RETAIL',
    })
  })

  it('creates a retail customer inline, auto-selects it, and keeps the cart', async () => {
    customers = []
    await seedCache([med], [])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    await addOneItem(user)
    await user.click(screen.getByRole('button', { name: /Credit \/ Udhaar/i }))
    await user.click(screen.getByRole('button', { name: /\+ New customer/i }))

    await user.type(screen.getByPlaceholderText('Customer name'), 'Sunita Sharma')
    await user.type(screen.getByPlaceholderText('Mobile number'), '9876500009')
    const limitInput = screen.getByLabelText(/Credit limit/)
    await user.clear(limitInput)
    await user.type(limitInput, '10000')
    await user.selectOptions(screen.getByRole('combobox', { name: /State/i }), '27')

    await user.click(screen.getByRole('button', { name: /Create & select/i }))

    expect(await screen.findByText('Sunita Sharma')).toBeInTheDocument()
    expect(screen.getByText(/Batch EARLY-B2/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Complete Sale —/i }))
    await screen.findByText('S1')

    expect(checkoutBodies[0]).toMatchObject({
      customer_id: 'c-new',
      payment_type: 'CREDIT',
      place_of_supply: '27',
    })
  })

  it('B2B search matches by GSTIN', async () => {
    customers = [b2b]
    await seedCache([med], [b2b])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    await addOneItem(user)
    await user.click(screen.getByRole('button', { name: /B2B Wholesale/i }))

    await user.type(
      screen.getByPlaceholderText(/Search B2B customer/i),
      '07AAPBC',
    )
    await user.click(await screen.findByText('Apollo Pharmacy Distributors'))

    expect(await screen.findByText(/07AAPBC1234F1Z5/)).toBeInTheDocument()
  })

  it('B2B creation auto-fills buyer details and completes a B2B sale', async () => {
    customers = []
    await seedCache([med], [])
    const user = userEvent.setup()
    render(<POS cacheVersion={1} />)

    await addOneItem(user)
    await user.click(screen.getByRole('button', { name: /B2B Wholesale/i }))
    await user.click(screen.getByRole('button', { name: /\+ New customer \(not in base yet\)/i }))

    await user.type(screen.getByPlaceholderText('Customer name'), 'MedPlus Wholesale')
    await user.type(screen.getByPlaceholderText('Mobile number'), '9876500011')
    await user.type(screen.getByPlaceholderText('22AAAAA0000A1Z5'), '07AAPBC1234F1Z5')
    await user.selectOptions(screen.getByRole('combobox', { name: /State/i }), '07')

    await user.click(screen.getByRole('button', { name: /Create & use for this sale/i }))

    const buyerName = await screen.findByPlaceholderText(/Buyer \/ Business Name/i)
    expect(buyerName).toHaveValue('MedPlus Wholesale')
    expect(screen.getByPlaceholderText('GSTIN (optional)')).toHaveValue('07AAPBC1234F1Z5')

    await user.click(screen.getByRole('button', { name: /Complete B2B Sale —/i }))
    await screen.findByText('S1')

    expect(checkoutBodies[0]).toMatchObject({
      sale_type: 'B2B',
      customer_id: 'c-new',
      buyer_name: 'MedPlus Wholesale',
      buyer_gstin: '07AAPBC1234F1Z5',
      place_of_supply: '07',
    })
  })
})