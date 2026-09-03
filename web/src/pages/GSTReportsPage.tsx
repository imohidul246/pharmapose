import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { api } from '../lib/api'
import { money } from '../lib/format'
import { parseGSTR2BFile } from '../lib/gstr2b'
import type {
  GSTR1Preview,
  GSTR2BBatch,
  GSTR2BDoc,
  GSTR2BReconciliation,
  GSTR3B,
} from '../types'

export type { ParsedGSTR2BDoc } from '../lib/gstr2b'
export { parseGSTR2BFile }

export const MONTHS = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
]

export function currentPeriod(d: Date = new Date()): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export function shiftPeriod(period: string, delta: number): string {
  const [y, m] = period.split('-').map(Number)
  const d = new Date(y, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export function periodLabel(period: string): string {
  const [y, m] = period.split('-').map(Number)
  return `${MONTHS[m - 1]} ${y}`
}

// GSTN codes a return as MMYYYY.
export function periodGSTNCode(period: string): string {
  const [y, m] = period.split('-').map(Number)
  return `${String(m).padStart(2, '0')}${y}`
}

export function fiscalYear(period: string): string {
  const [y, m] = period.split('-').map(Number)
  const start = m >= 4 ? y : y - 1
  return `${start}-${String((start + 1) % 100).padStart(2, '0')}`
}

// Twelve fiscal pips for the stamp strip: the FY runs April-to-March and the
// selected period's pip is filled, which makes the annual rhythm visible.
export function fiscalMonths(period: string): { label: string; value: string }[] {
  const [y, m] = period.split('-').map(Number)
  const fyStartYear = m >= 4 ? y : y - 1
  const labels = ['A', 'M', 'J', 'J', 'A', 'S', 'O', 'N', 'D', 'J', 'F', 'M']
  return labels.map((label, i) => {
    const mm = i + 4
    const year = mm > 12 ? fyStartYear + 1 : fyStartYear
    const month = mm > 12 ? mm - 12 : mm
    return { label, value: `${year}-${String(month).padStart(2, '0')}` }
  })
}

function rs(v: number): string {
  return `₹${money(v)}`
}

function Panel({ title, right, children }: { title: string; right?: ReactNode; children: ReactNode }) {
  return (
    <section className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-line-soft px-4 py-3">
        <h2 className="font-display text-sm font-bold uppercase tracking-wide">{title}</h2>
        {right}
      </header>
      {children}
    </section>
  )
}

function CardError({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="rounded-lg border border-brick-bg bg-brick-bg p-3 text-xs text-brick-text">
      <p className="font-semibold">Could not load this return.</p>
      <p className="mt-0.5">{message}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-2 rounded-lg border border-brick bg-white px-2.5 py-1 text-xs font-semibold text-brick-text"
        >
          Retry
        </button>
      )}
    </div>
  )
}

function DocStatusChip({ status }: { status: string }) {
  const tone =
    status === 'matched'
      ? 'bg-safe-bg text-safe-text'
      : status === 'unmatched'
        ? 'bg-brick-bg text-brick-text'
        : status === 'amount_mismatch'
          ? 'bg-marigold-bg text-marigold-text'
          : 'bg-mint-100 text-pine-700'
  const label =
    status === 'matched' ? 'Matched'
      : status === 'amount_mismatch' ? 'Amount mismatch'
        : status === 'unmatched' ? 'Unmatched'
          : status
  return <span className={`rounded px-1.5 py-0.5 font-mono text-xs font-semibold ${tone}`}>{label}</span>
}

function BatchStatusChip({ unmatched }: { unmatched: number }) {
  return unmatched > 0 ? (
    <span className="rounded bg-marigold-bg px-1.5 py-0.5 font-mono text-xs font-semibold text-marigold-text">Needs review</span>
  ) : (
    <span className="rounded bg-safe-bg px-1.5 py-0.5 font-mono text-xs font-semibold text-safe-text">Complete</span>
  )
}

function StatCard({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="rounded-xl border border-line bg-mint-50/70 px-3 py-2.5">
      <p className="text-[10px] font-bold uppercase tracking-[0.14em] text-inksoft">{label}</p>
      <p className={`mt-1 truncate font-display text-xl font-black tabular-nums ${accent ? 'text-brick-text' : 'text-pine-900'}`}>
        {value}
      </p>
    </div>
  )
}

function GSTR1Card({ period }: { period: string }) {
  const [preview, setPreview] = useState<GSTR1Preview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [downloading, setDownloading] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setPreview(await api.gstr1Preview(period))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load the GSTR-1 preview')
    } finally {
      setLoading(false)
    }
  }, [period])

  useEffect(() => {
    void load()
  }, [load])

  const download = async (kind: 'json' | 'csv') => {
    setDownloading(kind)
    try {
      const blob = kind === 'json' ? await api.downloadGSTR1JSON(period) : await api.downloadGSTR1CSV(period)
      saveBlob(blob, `gstr1_${periodGSTNCode(period)}.${kind}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Download failed')
    } finally {
      setDownloading('')
    }
  }

  return (
    <Panel
      title="GSTR-1 · Outward supplies"
      right={<span className="font-mono text-[10px] uppercase tracking-widest text-inksoft">GST Portal offline-utility format</span>}
    >
      <div className="flex flex-col gap-4 p-4">
        {loading ? (
          <p className="py-8 text-center text-sm text-inksoft">Loading preview…</p>
        ) : error ? (
          <CardError message={error} onRetry={() => void load()} />
        ) : preview ? (
          <>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
              <StatCard label="Taxable value" value={rs(preview.taxable_value)} />
              <StatCard label="IGST" value={rs(preview.igst_total)} />
              <StatCard label="CGST" value={rs(preview.cgst_total)} />
              <StatCard label="SGST" value={rs(preview.sgst_total)} />
              <StatCard label="B2B invoices" value={String(preview.b2b_count)} />
              <StatCard label="B2C invoices" value={String(preview.b2c_count)} />
            </div>
            <div className="mt-auto flex flex-col gap-2 sm:flex-row">
              <button
                onClick={() => void download('json')}
                disabled={downloading !== ''}
                className="flex-1 rounded-lg bg-pine-700 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-800 disabled:opacity-50"
              >
                {downloading === 'json' ? 'Downloading…' : 'Download GSTR-1 JSON'}
              </button>
              <button
                onClick={() => void download('csv')}
                disabled={downloading !== ''}
                className="flex-1 rounded-lg border border-line px-4 py-2 text-sm font-semibold text-pine-700 transition-colors hover:bg-mint-50 disabled:opacity-50"
              >
                {downloading === 'csv' ? 'Downloading…' : 'Download CSV'}
              </button>
            </div>
          </>
        ) : null}
      </div>
    </Panel>
  )
}

const GSTR3B_ROWS: [string, keyof Pick<
  GSTR3B,
  '3_1_a_outward_taxable_supplies' | '3_1_b_reverse_charge' | '3_1_c_zero_rated' | '3_1_d_exempt_nil_rated' | '4_a_eligible_itc' | '4_b_ineligible_itc'
>][] = [
  ['3.1(a) Outward taxable supplies', '3_1_a_outward_taxable_supplies'],
  ['3.1(b) Reverse charge', '3_1_b_reverse_charge'],
  ['3.1(c) Zero rated', '3_1_c_zero_rated'],
  ['3.1(d) Exempt / nil rated', '3_1_d_exempt_nil_rated'],
  ['4(a) Eligible ITC', '4_a_eligible_itc'],
  ['4(b) Ineligible ITC', '4_b_ineligible_itc'],
]

function GSTR3BCard({ period }: { period: string }) {
  const [g, setG] = useState<GSTR3B | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showTable, setShowTable] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setG(await api.gstr3b(period))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load the GSTR-3B summary')
    } finally {
      setLoading(false)
    }
  }, [period])

  useEffect(() => {
    void load()
  }, [load])

  const download = async () => {
    if (!g) return
    saveBlob(new Blob([JSON.stringify(g, null, 2)], { type: 'application/json' }), `gstr3b_${periodGSTNCode(period)}.json`)
  }

  const outward = g?.['3_1_a_outward_taxable_supplies']
  const eligible = g?.['4_a_eligible_itc']
  const payable = g?.['6_1_net_liability']?.payable?.total ?? 0

  return (
    <Panel
      title="GSTR-3B · Monthly summary"
      right={
        g?.gstin ? (
          <span className="font-mono text-[10px] uppercase tracking-widest text-inksoft">{g.gstin}</span>
        ) : undefined
      }
    >
      <div className="flex flex-col gap-4 p-4">
        {loading ? (
          <p className="py-8 text-center text-sm text-inksoft">Loading summary…</p>
        ) : error ? (
          <CardError message={error} onRetry={() => void load()} />
        ) : g ? (
          <>
            <div className="grid grid-cols-2 gap-3">
              <StatCard label="Outward taxable" value={rs(outward?.taxable_value ?? 0)} />
              <StatCard label="Eligible ITC" value={rs(eligible?.total ?? 0)} />
              <StatCard label="Net payable" value={rs(payable)} accent />
              <StatCard label="ITC at risk" value={String(g.itc_at_risk ?? 0)} />
            </div>
            {g.itc_at_risk > 0 && (
              <p className="rounded-lg bg-marigold-bg px-3 py-2 text-xs text-marigold-text">
                Flag {g.itc_at_risk} supplier document(s) in GSTR-2B for ITC you cannot claim yet.
              </p>
            )}
            <button
              onClick={() => setShowTable((v) => !v)}
              aria-expanded={showTable}
              className="self-start rounded-lg border border-line px-3 py-1.5 text-xs font-semibold text-pine-700 transition-colors hover:bg-mint-50"
            >
              {showTable ? 'Hide section break-down' : 'Show section break-down'}
            </button>
            {showTable && (
              <dl className="divide-y divide-line-soft rounded-lg border border-line">
                {GSTR3B_ROWS.map(([label, key]) => {
                  const line = g[key]
                  if (!line) return null
                  return (
                    <div key={label} className="flex items-center justify-between gap-3 px-3 py-2">
                      <dt className="text-xs text-ink">{label}</dt>
                      <dd className="font-mono text-right text-xs tabular-nums text-ink">
                        <span className="text-inksoft">{line.taxable_value ? rs(line.taxable_value) : '—'}</span>{' '}
                        <span className="font-semibold">₹{money(line.total ?? 0)}</span>
                      </dd>
                    </div>
                  )
                })}
              </dl>
            )}
            <button
              onClick={() => void download()}
              className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-800"
            >
              Download GSTR-3B JSON
            </button>
          </>
        ) : null}
      </div>
    </Panel>
  )
}

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

function GSTR2BCard({ period }: { period: string }) {
  const [batches, setBatches] = useState<GSTR2BBatch[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [openId, setOpenId] = useState<string | null>(null)
  const [docs, setDocs] = useState<GSTR2BDoc[]>([])
  const [docsError, setDocsError] = useState('')
  const [importing, setImporting] = useState(false)
  const [result, setResult] = useState<GSTR2BReconciliation | null>(null)
  const [importError, setImportError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  const loadBatches = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setBatches(await api.gstr2bBatches())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load GSTR-2B imports')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadBatches()
  }, [loadBatches])

  const toggleDocs = async (batch: GSTR2BBatch) => {
    if (openId === batch.id) {
      setOpenId(null)
      setDocs([])
      return
    }
    setOpenId(batch.id)
    setDocs([])
    setDocsError('')
    try {
      const res = await api.gstr2bBatch(batch.id)
      setDocs(res.docs)
    } catch (err) {
      setDocsError(err instanceof Error ? err.message : 'Could not load documents')
    }
  }

  const onFile = async (file: File) => {
    setImporting(true)
    setResult(null)
    setImportError('')
    try {
      const text = await file.text()
      const parsed = parseGSTR2BFile(text, file.name)
      const rec = await api.importGSTR2B({
        period,
        source: file.name,
        docs: parsed.docs,
      })
      setResult(rec)
      await loadBatches()
    } catch (err) {
      setImportError(err instanceof Error ? err.message : 'Could not import the file')
    } finally {
      setImporting(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <Panel
      title="GSTR-2B · Supplier documents"
      right={<span className="font-mono text-[10px] uppercase tracking-widest text-inksoft">reconciled against the purchase ledger</span>}
    >
      <div className="flex flex-col gap-4 p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-ink">Import the month's GSTR-2B</h3>
            <p className="mt-0.5 text-xs text-inksoft">
              Download GSTR-2B from the GST Portal and upload the JSON or CSV export. Each document is
              matched to its purchase invoice.
            </p>
          </div>
          <label className="cursor-pointer rounded-lg border border-line bg-white px-4 py-2 text-sm font-semibold text-pine-700 transition-colors hover:bg-mint-50">
            <input
              ref={fileRef}
              type="file"
              accept=".json,.csv,.tsv"
              className="sr-only"
              aria-label="Import GSTR-2B file"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void onFile(f)
              }}
            />
            {importing ? 'Importing…' : 'Upload GSTR-2B'}
          </label>
        </div>

        {importError && <CardError message={importError} />}

        {result && (
          <p className="rounded-lg bg-safe-bg px-3 py-2 text-xs text-safe-text">
            Imported {result.total_docs} document(s) for {periodLabel(period)} — {result.matched} matched, {result.unmatched} unmatched.
          </p>
        )}

        {loading ? (
          <p className="py-6 text-center text-sm text-inksoft">Loading imports…</p>
        ) : error ? (
          <CardError message={error} onRetry={() => void loadBatches()} />
        ) : batches.length === 0 ? (
          <div className="rounded-xl border border-dashed border-line bg-mint-50/40 p-6 text-center text-sm text-inksoft">
            No GSTR-2B imports yet. Upload the month's file above to start reconciling.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-line-soft text-left text-xs uppercase tracking-wider text-inksoft">
                  <th className="py-2 pr-2 font-bold">Period</th>
                  <th className="py-2 pr-2 font-bold">File</th>
                  <th className="py-2 pr-2 font-bold">Imported</th>
                  <th className="py-2 pr-2 text-right font-bold">Docs</th>
                  <th className="py-2 pr-2 text-right font-bold">Matched</th>
                  <th className="py-2 pl-2 text-right font-bold">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-soft">
                {batches.map((b) => (
                  <BatchRow key={b.id} batch={b} open={openId === b.id} docs={docs} docsError={docsError} onToggle={() => void toggleDocs(b)} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </Panel>
  )
}

function BatchRow({
  batch,
  open,
  docs,
  docsError,
  onToggle,
}: {
  batch: GSTR2BBatch
  open: boolean
  docs: GSTR2BDoc[]
  docsError: string
  onToggle: () => void
}) {
  return (
    <>
      <tr className={open ? 'bg-mint-50/60' : 'hover:bg-mint-50/40'}>
        <td className="py-2.5 pr-2 font-mono text-xs font-bold text-pine-700">{batch.period}</td>
        <td className="py-2.5 pr-2">
          <button
            onClick={onToggle}
            aria-expanded={open}
            className="flex items-center gap-1.5 text-xs font-semibold text-pine-700 transition-colors hover:text-pine-900"
          >
            <span className={`inline-block text-[10px] text-inksoft transition-transform ${open ? 'rotate-90' : ''}`}>▸</span>
            {batch.file_name}
          </button>
        </td>
        <td className="py-2.5 pr-2 font-mono text-xs text-ink">{batch.created_at.slice(0, 10)}</td>
        <td className="py-2.5 pr-2 text-right font-mono text-xs tabular-nums text-ink">{batch.doc_count}</td>
        <td className="py-2.5 pr-2 text-right font-mono text-xs tabular-nums text-ink">
          {batch.matched_count} / {batch.doc_count}
        </td>
        <td className="py-2.5 pl-2 text-right">
          <BatchStatusChip unmatched={batch.unmatched_count} />
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6} className="bg-mint-50/40 p-3">
            {docsError ? (
              <CardError message={docsError} />
            ) : docs.length === 0 ? (
              <p className="py-3 text-center text-xs text-inksoft">Loading documents…</p>
            ) : (
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-line-soft text-left text-xs uppercase tracking-wider text-inksoft">
                    <th className="py-2 pr-2 font-bold">Supplier GSTIN</th>
                    <th className="py-2 pr-2 font-bold">Doc</th>
                    <th className="py-2 pr-2 font-bold">Invoice</th>
                    <th className="py-2 pr-2 font-bold">Date</th>
                    <th className="py-2 pr-2 text-right font-bold">Taxable value</th>
                    <th className="py-2 pr-2 text-right font-bold">Tax</th>
                    <th className="py-2 pl-2 text-right font-bold">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line-soft">
                  {docs.map((d) => (
                    <tr key={d.id}>
                      <td className="py-2 pr-2 font-mono text-xs text-ink">{d.supplier_gstin || '—'}</td>
                      <td className="py-2 pr-2 font-mono text-xs text-inksoft">{d.doc_type}</td>
                      <td className="py-2 pr-2 text-xs font-semibold text-ink">{d.invoice_no}</td>
                      <td className="py-2 pr-2 font-mono text-xs text-inksoft">{d.invoice_date}</td>
                      <td className="py-2 pr-2 text-right font-mono text-xs tabular-nums text-ink">{rs(d.taxable_value)}</td>
                      <td className="py-2 pr-2 text-right font-mono text-xs tabular-nums text-ink">
                        {rs(d.igst_amount + d.cgst_amount + d.sgst_amount + d.cess_amount)}
                      </td>
                      <td className="py-2 pl-2 text-right">
                        <DocStatusChip status={d.match_status} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </td>
        </tr>
      )}
    </>
  )
}

export default function GSTReportsPage() {
  const [period, setPeriod] = useState(currentPeriod())
  const go = (delta: number) => setPeriod(shiftPeriod(period, delta))
  const current = currentPeriod()

  return (
    <div className="mx-auto flex w-full flex-col gap-6 px-4 py-8 lg:px-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-inksoft">Returns</p>
          <h1 className="mt-1 font-display text-2xl font-black uppercase tracking-wide text-pine-900">GST returns</h1>
        </div>
        <p className="text-xs text-inksoft">Monthly returns to the GST Portal · FY {fiscalYear(period)}</p>
      </div>

      <section aria-label={`Return period: ${periodLabel(period)}`} className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
        <div className="flex items-center justify-between gap-3 border-b border-line-soft bg-mint-50/40 px-4 py-2">
          <p className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-inksoft">Return period</p>
          <p className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-inksoft">period {periodGSTNCode(period)}</p>
        </div>
        <div className="flex items-center justify-between gap-3 px-4 py-4">
          <button
            onClick={() => go(-1)}
            aria-label="Previous month"
            className="rounded-lg border border-line px-3 py-1.5 font-mono text-sm font-bold text-pine-700 transition-colors hover:bg-mint-50"
          >
            ‹
          </button>
          <div className="flex flex-col items-center text-center">
            <span className="font-display text-2xl font-black uppercase tracking-wide text-pine-900">{periodLabel(period)}</span>
            <span className="mt-0.5 font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-inksoft">FY {fiscalYear(period)}</span>
          </div>
          <div className="flex items-center gap-2">
            {period !== current && (
              <button
                onClick={() => setPeriod(current)}
                aria-label="This month"
                className="rounded-lg border border-line px-3 py-1.5 text-xs font-semibold text-pine-700 transition-colors hover:bg-mint-50"
              >
                This month
              </button>
            )}
            <button
              onClick={() => go(1)}
              aria-label="Next month"
              className="rounded-lg border border-line px-3 py-1.5 font-mono text-sm font-bold text-pine-700 transition-colors hover:bg-mint-50"
            >
              ›
            </button>
          </div>
        </div>
        <div className="flex items-center justify-center gap-1.5 border-t border-line-soft bg-mint-50/30 px-4 py-2">
          {fiscalMonths(period).map((p) => (
            <button
              key={p.value}
              onClick={() => setPeriod(p.value)}
              aria-label={periodLabel(p.value)}
              title={periodLabel(p.value)}
              className="group flex flex-col items-center gap-1 px-1.5"
            >
              <span
                data-active={p.value === period}
                className={`h-5 w-1.5 rounded-full ${p.value === period ? 'bg-pine-700' : 'bg-line transition-colors group-hover:bg-mint-300'}`}
              />
              <span className="font-mono text-[9px] font-bold uppercase text-inksoft">{p.label}</span>
            </button>
          ))}
        </div>
      </section>

      <div className="grid items-start gap-6 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <GSTR1Card period={period} />
        </div>
        <GSTR3BCard period={period} />
      </div>

      <GSTR2BCard period={period} />
    </div>
  )
}