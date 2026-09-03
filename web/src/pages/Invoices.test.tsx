import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Invoices from './Invoices'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const salesRows = Array.from({ length: 12 }, (_, i) => ({
  id: `sale-${i + 1}`,
  invoice_no: `INV/26-27/${String(i + 1).padStart(5, '0')}`,
  customer_id: null,
  customer_name: '',
  payment_type: 'CASH',
  total_amount: 100 + i,
  discount_total: 0,
  item_count: 1,
  units_sold: 2,
  created_at: '2026-08-24T20:28:09Z',
}))

const purchaseRow = {
  id: 'pur-1',
  invoice_no: 'PINV-TEST-1',
  supplier_name: 'Test Supplier',
  total_amount: 207,
  item_count: 1,
  units_purchased: 12,
  created_at: '2026-08-24T20:26:48Z',
}

function jsonResponse(body: unknown) {
  return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
}

function salesDetail(id = 'sale-1') {
  const num = id.split('-')[1] ?? '1'
  return jsonResponse({
    invoice: {
      id,
      invoice_no: `INV/26-27/${String(num).padStart(5, '0')}`,
      payment_type: 'CASH',
      total_amount: 66,
      discount_total: 9,
      created_at: '2026-08-24T20:28:09Z',
    },
    customer_name: '',
    items: [
      {
        id: 'i1',
        invoice_id: id,
        medicine_id: 'm1',
        batch_id: 'b1',
        quantity: 3,
        unit_sale_price: 25,
        subtotal: 66,
        discount_type: 'percent',
        discount_value: 12,
        discount_amount: 9,
        medicine_name: 'Dolo 650',
        batch_number: 'b-3',
      },
    ],
  })
}

function installFetchMock(opts?: { failSalesDetail?: boolean }) {
  let salesDetailBroken = opts?.failSalesDetail ?? false
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/sales/invoices/')) {
      if (salesDetailBroken) {
        salesDetailBroken = false
        return { ok: false, status: 500, json: () => Promise.resolve({ error: 'kaboom' }) } as Response
      }
      const id = url.split('/').pop() as string
      return salesDetail(id)
    }
    if (url.includes('/api/purchases/invoices/')) {
      return jsonResponse({
        invoice: {
          id: 'pur-1',
          invoice_no: 'PINV-TEST-1',
          supplier_name: 'Test Supplier',
          total_amount: 207,
          created_at: '2026-08-24T20:26:48Z',
        },
        items: [
          {
            id: 'pi1',
            purchase_id: 'pur-1',
            medicine_id: 'm1',
            batch_number: 'b-3',
            expiry_date: '2027-08-24',
            quantity: 12,
            purchase_price: 17.25,
            sale_price: 25,
            medicine_name: 'Dolo 650',
          },
        ],
      })
    }
    if (url.includes('/api/sales/invoices')) return jsonResponse({ invoices: salesRows })
    if (url.includes('/api/purchases/invoices')) return jsonResponse({ invoices: [purchaseRow] })
    throw new Error('unexpected fetch ' + url)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('Invoices page', () => {
  it('paginates the default result and pages through it', async () => {
    installFetchMock()
    const user = userEvent.setup()
    render(<Invoices />)

    expect(await screen.findByText('Page 1 of 2')).toBeInTheDocument()
    // Sales section shows only the first page slice (8 of 12).
    expect(screen.getAllByText(/INV\/26-27\/\d+/)).toHaveLength(8)

    await user.click(screen.getByRole('button', { name: /Next/ }))
    expect(await screen.findByText('Page 2 of 2')).toBeInTheDocument()
    expect(screen.getAllByText(/INV\/26-27\/\d+/)).toHaveLength(4)
  })

  it('opens sales invoice details from the unsearched default list', async () => {
    installFetchMock()
    const user = userEvent.setup()
    render(<Invoices />)

    await screen.findByText('Page 1 of 2')
    // The last View button on page 1 belongs to the sales section.
    const viewButtons = screen.getAllByRole('button', { name: 'View' })
    await user.click(viewButtons[viewButtons.length - 1])

    expect(await screen.findByText('Net payable')).toBeInTheDocument()
    expect(screen.getByText('Dolo 650')).toBeInTheDocument()
    expect(screen.getByText(/Gross amount/)).toBeInTheDocument()
  })

  it('opens purchase invoice details from the unsearched default list', async () => {
    installFetchMock()
    const user = userEvent.setup()
    render(<Invoices />)

    await screen.findByText('PINV-TEST-1')
    await user.click(screen.getAllByRole('button', { name: 'View' })[0])

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Purchase total')).toBeInTheDocument()
    expect(within(dialog).getByText('Test Supplier')).toBeInTheDocument()
    expect(within(dialog).getByText('2027-08-24')).toBeInTheDocument()
  })

  it('shows a retryable error instead of a blank dialog when detail load fails', async () => {
    installFetchMock({ failSalesDetail: true })
    const user = userEvent.setup()
    render(<Invoices />)

    await screen.findByText('Page 1 of 2')
    const viewButtons = screen.getAllByRole('button', { name: 'View' })
    await user.click(viewButtons[viewButtons.length - 1])

    expect(await screen.findByText(/Could not load this invoice: kaboom/)).toBeInTheDocument()

    // The mock flips back to success after the failed call; retry recovers.
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Net payable')).toBeInTheDocument()
  })

  it('closes the dialog with Escape', async () => {
    installFetchMock()
    const user = userEvent.setup()
    render(<Invoices />)

    await screen.findByText('PINV-TEST-1')
    await user.click(screen.getAllByRole('button', { name: 'View' })[0])
    expect(await screen.findByText('Purchase total')).toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(screen.queryByText('Purchase total')).not.toBeInTheDocument()
  })
})
