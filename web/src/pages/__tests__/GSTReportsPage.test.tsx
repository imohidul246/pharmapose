import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import GSTReportsPage, {
  currentPeriod,
  fiscalMonths,
  fiscalYear,
  parseGSTR2BFile,
  periodGSTNCode,
  periodLabel,
} from '../GSTReportsPage'

const PERIOD = currentPeriod()

function jsonResponse(body: unknown) {
  return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
}

function blobResponse(content: string, type = 'application/json') {
  return {
    ok: true,
    status: 200,
    blob: () => Promise.resolve(new Blob([content], { type })),
  } as Response
}

function mkLine(taxable: number, igst: number, cgst: number, sgst: number, cess: number) {
  return { taxable_value: taxable, igst, cgst, sgst, cess, total: taxable + igst + cgst + sgst + cess }
}

function gstr3bFixture() {
  return {
    gstin: '27AAAAA1111A1ZW',
    period: PERIOD,
    financial_year: fiscalYear(PERIOD),
    gstn_period_code: periodGSTNCode(PERIOD),
    filing_date: '2026-08-31',
    state_code: '27',
    '3_1_a_outward_taxable_supplies': mkLine(168000, 0, 10080, 10080, 0),
    '3_1_b_reverse_charge': mkLine(0, 0, 0, 0, 0),
    '3_1_c_zero_rated': mkLine(0, 0, 0, 0, 0),
    '3_1_d_exempt_nil_rated': mkLine(0, 0, 0, 0, 0),
    '4_a_eligible_itc': mkLine(56000, 0, 3360, 3360, 0),
    '4_b_ineligible_itc': mkLine(0, 0, 0, 0, 0),
    '6_1_net_liability': {
      liability: mkLine(168000, 0, 10080, 10080, 0),
      itc_credit: mkLine(56000, 0, 3360, 3360, 0),
      payable: mkLine(112000, 0, 6720, 6720, 0),
    },
    itc_at_risk: 0,
    unmatched_docs: 0,
  }
}

function batchesFixture() {
  return [
    {
      id: 'b1',
      store_id: null,
      gstin: '',
      period: '2026-07',
      file_name: 'gstr2b_2026-07.json',
      doc_count: 3,
      matched_count: 2,
      unmatched_count: 1,
      status: 'imported',
      created_at: '2026-08-05T10:00:00Z',
    },
  ]
}

function docsFixture() {
  return [
    {
      id: 'd1',
      import_batch_id: 'b1',
      store_id: null,
      supplier_gstin: '27AAPBC1234F1ZV',
      doc_type: 'INV',
      period: '2026-07',
      invoice_no: 'SUP/001',
      invoice_date: '2026-07-12',
      taxable_value: 1000,
      igst_amount: 120,
      cgst_amount: 0,
      sgst_amount: 0,
      cess_amount: 0,
      total_value: 1120,
      match_status: 'matched',
      matched_purchase_id: 'p1',
      matched_difference: null,
      notes: '',
      created_at: '2026-08-05T10:00:00Z',
    },
  ]
}

function installFetchMock(opts?: { previewError?: boolean }) {
  const previewError = opts?.previewError ?? false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'

    if (url.includes('/api/gst/gstr2b/import') && method === 'POST') {
      return jsonResponse({
        batch_id: 'b1',
        period: PERIOD,
        gstin: '',
        total_docs: 1,
        matched: 1,
        unmatched: 0,
        amount_mismatch: 0,
        matched_taxable_value: 1000,
        unmatched_taxable_value: 0,
      })
    }
    if (url.includes('/api/gst/gstr2b/batches/b1')) {
      return jsonResponse({ batch: batchesFixture()[0], docs: docsFixture() })
    }
    if (url.includes('/api/gst/gstr2b/batches')) {
      return jsonResponse(batchesFixture())
    }
    if (url.includes('/api/gst/gstr3b')) {
      return jsonResponse(gstr3bFixture())
    }
    if (url.includes('/api/gst/gstr1/preview')) {
      if (previewError) {
        return { ok: false, status: 500, json: () => Promise.resolve({ error: 'db down' }) } as Response
      }
      return jsonResponse({
        taxable_value: 150000,
        cgst_total: 9000,
        sgst_total: 9000,
        igst_total: 0,
        b2b_count: 5,
        b2c_count: 12,
      })
    }
    if (url.includes('/api/gst/gstr1/excel')) {
      return blobResponse('invoice_no,invoice_date\nINV/26-27/00001,01-08-2026\n', 'text/csv')
    }
    if (url.includes('/api/gst/gstr1?')) {
      return blobResponse(JSON.stringify({ gstin: '27AABCU9603R1ZM', b2b: [] }))
    }
    throw new Error('unexpected fetch ' + url)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('GSTReportsPage', () => {
  it('loads GSTR-1 preview, GSTR-3B key totals and GSTR-2B batches on mount', async () => {
    installFetchMock()
    render(<GSTReportsPage />)

    // GSTR-1 preview
    expect(await screen.findByText('₹1,50,000.00')).toBeInTheDocument()
    expect(screen.getAllByText('₹9,000.00')).toHaveLength(2)
    expect(screen.getByText('5')).toBeInTheDocument()

    // GSTR-3B key totals
    expect(await screen.findByText('₹1,68,000.00')).toBeInTheDocument()
    expect(screen.getByText('₹62,720.00')).toBeInTheDocument()
    expect(screen.getByText('₹1,25,440.00')).toBeInTheDocument()

    // GSTR-2B batches
    expect(await screen.findByText('gstr2b_2026-07.json')).toBeInTheDocument()
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
  })

  it('switches the period and refetches the period-bound returns', async () => {
    const fetchMock = installFetchMock()
    const user = userEvent.setup()
    render(<GSTReportsPage />)

    await screen.findByText('₹1,50,000.00')
    const callsBefore = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/gst/gstr1/preview'),
    ).length

    await user.click(screen.getByRole('button', { name: /Next month/ }))

    expect(fetchMock.mock.calls.length).toBeGreaterThan(callsBefore + 1)
    const previewCalls = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/gst/gstr1/preview'),
    )
    const lastPreview = String(previewCalls[previewCalls.length - 1][0])
    expect(lastPreview).toContain('period=')
  })

  it('renders the period stamp for the selected month', async () => {
    installFetchMock()
    render(<GSTReportsPage />)

    expect(screen.getByRole('region', { name: new RegExp(`Return period: ${periodLabel(PERIOD)}`) })).toBeInTheDocument()
    expect(screen.getByText(new RegExp(periodGSTNCode(PERIOD)))).toBeInTheDocument()
  })

  it('shows an error banner when the GSTR-1 preview fails', async () => {
    installFetchMock({ previewError: true })
    render(<GSTReportsPage />)

    expect(await screen.findByText(/db down/i)).toBeInTheDocument()
  })

  it('has enabled download buttons once loaded', async () => {
    installFetchMock()
    render(<GSTReportsPage />)

    await screen.findByText('₹1,50,000.00')
    const jsonBtn = screen.getByRole('button', { name: /Download GSTR-1 JSON/ })
    const csvBtn = screen.getByRole('button', { name: /Download CSV/ })
    const gstr3bBtn = screen.getByRole('button', { name: /Download GSTR-3B JSON/ })
    expect(jsonBtn).not.toBeDisabled()
    expect(csvBtn).not.toBeDisabled()
    expect(gstr3bBtn).not.toBeDisabled()
  })

  it('imports a GSTR-2B JSON file and shows the reconciliation', async () => {
    const fetchMock = installFetchMock()
    const user = userEvent.setup()
    render(<GSTReportsPage />)

    const file = new File(
      [
        JSON.stringify([
          {
            supplier_gstin: '27AAPBC1234F1ZV',
            invoice_no: 'SUP/001',
            invoice_date: '12-07-2026',
            doc_type: 'INV',
            taxable_value: 1000,
            igst: 120,
          },
        ]),
      ],
      'gstr2b.json',
      { type: 'application/json' },
    )

    const input = screen.getByLabelText(/Import GSTR-2B file/)
    await user.upload(input, file)

    const importCalls = fetchMock.mock.calls.filter(([input]) =>
      String(input).includes('/api/gst/gstr2b/import'),
    )
    expect(importCalls).toHaveLength(1)
    await screen.findByText(/Imported 1 document/i)
    expect(screen.getByText(/matched, 0 unmatched/)).toBeInTheDocument()
  })

  it('expands a batch to show its supplier documents', async () => {
    const fetchMock = installFetchMock()
    const user = userEvent.setup()
    render(<GSTReportsPage />)

    await screen.findByText('gstr2b_2026-07.json')
    await user.click(screen.getByText('gstr2b_2026-07.json'))

    expect(await screen.findByText('SUP/001')).toBeInTheDocument()
    expect(screen.getByText('27AAPBC1234F1ZV')).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/api/gst/gstr2b/batches/b1')),
    ).toBe(true)
  })
})

describe('period helpers', () => {
  it('fiscalMonths walks the fiscal year from April to March', () => {
    const p = fiscalMonths('2026-08')
    expect(p[0].value).toBe('2026-04')
    expect(p[11].value).toBe('2027-03')
  })

  it('fiscalYear brackets the period', () => {
    expect(fiscalYear('2026-01')).toBe('2025-26')
    expect(fiscalYear('2026-04')).toBe('2026-27')
  })

  it('periodGSTNCode renders MMYYYY', () => {
    expect(periodGSTNCode('2026-08')).toBe('082026')
  })
})

describe('parseGSTR2BFile', () => {
  it('parses a GSTN-style JSON document array', () => {
    const { docs } = parseGSTR2BFile(
      JSON.stringify([
        {
          supplier_gstin: '27AAPBC1234F1ZV',
          invoice_no: 'INV-1',
          invoice_date: '07/08/2026',
          doc_type: 'Invoice',
          taxable_value: 100,
          igst: 12,
        },
      ]),
      'export.json',
    )
    expect(docs).toHaveLength(1)
    expect(docs[0]).toMatchObject({
      invoice_no: 'INV-1',
      invoice_date: '2026-08-07',
      doc_type: 'INV',
      taxable_value: 100,
      igst: 12,
    })
  })

  it('parses a CSV export with GSTN column names', () => {
    const csv = 'invoice_no,invoice_date,taxable_value,cgst,sgst,doc_type\nINV-2,18-08-2026,200,12,12,INV\n'
    const { docs } = parseGSTR2BFile(csv, 'gstr2b.csv')
    expect(docs).toHaveLength(1)
    expect(docs[0]).toMatchObject({
      invoice_no: 'INV-2',
      invoice_date: '2026-08-18',
      taxable_value: 200,
      cgst: 12,
      sgst: 12,
      doc_type: 'INV',
    })
  })

  it('rejects an empty CSV with a helpful message', () => {
    expect(() => parseGSTR2BFile('invoice_no,invoice_date\n', 'x.csv')).toThrow(/header row/)
  })
})