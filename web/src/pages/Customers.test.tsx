import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Customers from './Customers'
import type { Customer } from '../types'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

let fullList: Customer[]

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/customers') && !init?.method) {
      const params = new URL(url, 'http://localhost').searchParams
      const search = params.get('search')
      if (!search) return json({ customers: fullList })
      const q = search.toLowerCase()
      const type = params.get('type')
      const filtered = fullList.filter(
        (c) =>
          (c.customer_type ?? 'B2C') === type &&
          (c.name.toLowerCase().includes(q) ||
            c.phone.toLowerCase().includes(q) ||
            (!!c.gstin && c.gstin.toLowerCase().includes(q))),
      )
      return json({ customers: filtered })
    }
    return json({}, 404)
  }))
})

const retail1: Customer = {
  id: 'c1', name: 'Ramesh Kumar', phone: '9876500001', credit_limit: 5000,
  current_balance: 200, customer_type: 'B2C', state_code: '27', state: 'Maharashtra',
  gstin: null, billing_address: null, shipping_address: null,
}
const retail2: Customer = {
  id: 'c2', name: 'Suresh Iyer', phone: '9876500002', credit_limit: 1000,
  current_balance: 0, customer_type: 'B2C', state_code: null, state: null,
  gstin: null, billing_address: null, shipping_address: null,
}
const b2b1: Customer = {
  id: 'c3', name: 'Apollo Pharmacy Distributors', phone: '9876500003', credit_limit: 100000,
  current_balance: 50000, customer_type: 'B2B', state_code: '07', state: 'Delhi',
  gstin: '07AAPBC1234F1Z5', billing_address: null, shipping_address: null,
}

const renderKhata = () => render(<Customers onMutated={async () => {}} />)

describe('Khata (Customers page)', () => {
  it('splits credit customers into B2C and B2B sections', async () => {
    fullList = [retail1, retail2, b2b1]
    renderKhata()

    expect(await screen.findByText('B2C credit customers')).toBeInTheDocument()
    expect(screen.getByText('B2B credit customers')).toBeInTheDocument()

    expect(screen.getByText('Ramesh Kumar')).toBeInTheDocument()
    expect(screen.getByText('Suresh Iyer')).toBeInTheDocument()
    expect(screen.getByText('Apollo Pharmacy Distributors')).toBeInTheDocument()
  })

  it('has no inline create-customer form anymore', async () => {
    fullList = []
    renderKhata()

    await screen.findByText('B2C credit customers')
    expect(screen.queryByText(/Add credit customer/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Create customer/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/No B2C credit customers yet — create one from the Billing page/i)).toBeInTheDocument()
  })

  it('filters each section with its own server search and keeps the other intact', async () => {
    fullList = [retail1, retail2, b2b1]
    renderKhata()
    await screen.findByText('Ramesh Kumar')

    const user = userEvent.setup()
    await user.type(
      screen.getByPlaceholderText(/Search B2C customers by name or phone/i),
      'ramesh',
    )

    await waitFor(() => expect(screen.queryByText('Suresh Iyer')).not.toBeInTheDocument())
    expect(screen.getByText('Ramesh Kumar')).toBeInTheDocument()
    expect(screen.getByText('Apollo Pharmacy Distributors')).toBeInTheDocument()

    const calls = vi.mocked(fetch).mock.calls.map((c) => String(c[0]))
    const searchCall = calls.find((u) => u.includes('search='))
    expect(searchCall).toBeTruthy()
    expect(searchCall).toContain('type=B2C')
  })

  it('clearing the search returns to the full local list', async () => {
    fullList = [retail1, retail2, b2b1]
    renderKhata()
    await screen.findByText('Ramesh Kumar')

    const user = userEvent.setup()
    const search = screen.getByPlaceholderText(/Search B2C customers by name or phone/i)
    await user.type(search, 'suresh')
    await waitFor(() => expect(screen.queryByText('Ramesh Kumar')).not.toBeInTheDocument())
    expect(screen.getByText('Suresh Iyer')).toBeInTheDocument()

    await user.clear(search)
    expect(await screen.findByText('Ramesh Kumar')).toBeInTheDocument()
    expect(screen.getByText('Suresh Iyer')).toBeInTheDocument()
  })

  it('opens invoice details in-page when a credit-sale ledger note is clicked', async () => {
    fullList = [retail1]
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith('/api/customers/c1/ledger')) {
        return json({
          customer: retail1,
          entries: [
            {
              id: 'e1',
              customer_id: 'c1',
              entry_type: 'CREDIT_SALE',
              amount: 500,
              balance_after: 700,
              notes: 'Invoice INV/26-27/00001',
              created_at: '2026-08-24T20:28:09Z',
            },
            {
              id: 'e2',
              customer_id: 'c1',
              entry_type: 'PAYMENT',
              amount: -200,
              balance_after: 500,
              notes: 'Payment via UPI',
              created_at: '2026-08-25T10:00:00Z',
            },
          ],
        })
      }
      if (url.includes('/api/sales/invoices/resolve')) {
        return json({
          invoice: {
            id: 'sale-1',
            invoice_no: 'INV/26-27/00001',
            payment_type: 'CREDIT',
            total_amount: 500,
            discount_total: 0,
            created_at: '2026-08-24T20:28:09Z',
          },
          customer_name: 'Ramesh Kumar',
          items: [
            {
              id: 'i1',
              invoice_id: 'sale-1',
              medicine_id: 'm1',
              batch_id: 'b1',
              quantity: 2,
              unit_sale_price: 250,
              subtotal: 500,
              discount_amount: 0,
              medicine_name: 'Dolo 650',
              batch_number: 'b3',
            },
          ],
        })
      }
      if (url.startsWith('/api/customers')) return json({ customers: [retail1] })
      return json({}, 404)
    }))

    const user = userEvent.setup()
    renderKhata()
    await user.click((await screen.findAllByRole('button', { name: /Ledger/i }))[0])

    // Credit-sale note shows a clickable invoice number; payment note stays plain text.
    const invoiceBtn = await screen.findByRole('button', { name: 'INV/26-27/00001' })
    expect(screen.getByText('Payment via UPI')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Payment via UPI/i })).not.toBeInTheDocument()

    await user.click(invoiceBtn)
    expect(await screen.findByText('Dolo 650')).toBeInTheDocument()
    expect(screen.getByText('Net payable')).toBeInTheDocument()
    expect(screen.getAllByText(/₹500\.00/).length).toBeGreaterThan(0)
  })
})