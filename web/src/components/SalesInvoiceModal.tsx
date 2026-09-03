import { useEffect, useRef, useState } from 'react'
import { api } from '../lib/api'
import { money } from '../lib/format'
import type { SalesInvoiceDetail } from '../types'

// ---- Shared sales-invoice detail dialog -----------------------------------
// Used by both the Invoices page (fetched by internal id) and the Khata
// ledger notes (fetched by the printed invoice number via `load`). Every
// fetch state is explicit and visible: a skeleton while loading, a retryable
// error box on failure, an explicit note when an invoice carries no line
// items — so the dialog can never collapse into a blank overlay.

export type LoadState = 'loading' | 'ready' | 'error'

export function fmtDate(iso: string): string {
  return new Date(iso).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

export function DetailModal({
  title,
  onClose,
  status,
  error,
  onRetry,
  children,
}: {
  title: React.ReactNode
  onClose: () => void
  status: LoadState
  error: string
  onRetry: () => void
  children?: React.ReactNode
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-30 flex items-center justify-center bg-pine-950/55 p-4 backdrop-blur-[2px]"
      onClick={onClose}
    >
      <div
        tabIndex={-1}
        className="max-h-[88vh] w-full max-w-2xl space-y-3 overflow-y-auto rounded-2xl bg-white p-5 shadow-2xl outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-dashed border-line pb-3">
          <h3 className="font-display text-base font-bold tracking-tight">{title}</h3>
          <button
            onClick={onClose}
            aria-label="Close dialog"
            className="rounded-md p-1 text-inksoft/70 transition-colors hover:bg-mint-50 hover:text-ink"
          >
            <svg viewBox="0 0 14 14" className="h-3.5 w-3.5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round">
              <path d="M2 2l10 10M12 2L2 12" />
            </svg>
          </button>
        </div>

        {status === 'loading' && <ModalSkeleton />}

        {status === 'error' && (
          <div className="space-y-3">
            <p className="rounded-lg bg-brick-bg px-3 py-2 text-sm font-medium text-brick-text">
              Could not load this invoice: {error}
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={onClose}
                className="rounded-lg border border-line px-3.5 py-2 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50"
              >
                Close
              </button>
              <button
                onClick={onRetry}
                className="rounded-lg bg-pine-700 px-3.5 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-600"
              >
                Retry
              </button>
            </div>
          </div>
        )}

        {status === 'ready' && children}
      </div>
    </div>
  )
}

export function ModalSkeleton() {
  return (
    <div className="animate-pulse space-y-3" aria-label="Loading invoice details">
      <div className="grid grid-cols-3 gap-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="space-y-1.5 rounded-lg bg-mint-50 px-3.5 py-3">
            <div className="h-2 w-12 rounded bg-line" />
            <div className="h-3.5 w-20 rounded bg-line" />
          </div>
        ))}
      </div>
      <div className="overflow-hidden rounded-lg border border-line">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="flex items-center gap-4 border-b border-line-soft px-3 py-2.5 last:border-0">
            <div className="h-3 flex-1 rounded bg-line" />
            <div className="h-3 w-10 rounded bg-line" />
            <div className="h-3 w-14 rounded bg-line" />
          </div>
        ))}
      </div>
      <p className="text-center text-xs text-inksoft">Loading invoice…</p>
    </div>
  )
}

// Fetches either by the internal id (Invoices page) or by a `load` callback
// (Khata ledger, which only knows the printed invoice number). `load` is read
// from a ref so callers may pass an inline closure without refetch loops.
export function SalesInvoiceModal({
  id,
  load,
  onClose,
}: {
  id?: string
  load?: () => Promise<SalesInvoiceDetail>
  onClose: () => void
}) {
  const loadRef = useRef<(() => Promise<SalesInvoiceDetail>) | null>(null)
  loadRef.current = load ?? null

  const [detail, setDetail] = useState<SalesInvoiceDetail | null>(null)
  const [status, setStatus] = useState<LoadState>('loading')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    setStatus('loading')
    setDetail(null)
    setError('')
    const fetchDetail = () =>
      id != null
        ? api.salesInvoice(id)
        : (loadRef.current?.() ?? Promise.reject(new Error('No invoice reference provided.')))
    void fetchDetail()
      .then((d) => {
        if (cancelled) return
        setDetail(d)
        setStatus('ready')
      })
      .catch((err) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : String(err))
        setStatus('error')
      })
    return () => {
      cancelled = true
    }
  }, [id, attempt])

  const inv = detail?.invoice ?? null
  const items = detail?.items ?? []
  const gross = items.reduce((a, it) => a + it.quantity * it.unit_sale_price, 0)

  return (
    <DetailModal
      onClose={onClose}
      status={status}
      error={error}
      onRetry={() => setAttempt((n) => n + 1)}
      title={
        inv ? (
          <>
            Sales invoice <span className="font-mono">{inv.invoice_no}</span>
          </>
        ) : (
          'Sales invoice'
        )
      }
    >
      {inv && (
        <>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-lg bg-mint-50 px-3.5 py-3 text-sm sm:grid-cols-3">
            <Meta label="Date" value={fmtDate(inv.created_at)} mono />
            <Meta
              label="Payment"
              value={inv.payment_type === 'CREDIT' ? 'Credit (udhaar)' : 'Cash'}
              accent={inv.payment_type === 'CREDIT'}
            />
            <Meta label="Customer" value={(detail?.customer_name ?? '').trim() || 'Walk-in'} />
            {inv.supply_type && (
              <Meta label="Supply" value={inv.supply_type === 'INTER_STATE' ? 'Inter-state (IGST)' : 'Intra-state (CGST+SGST)'} />
            )}
          </dl>

          {inv.sale_type === 'B2B' && (
            <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm">
              <div className="mb-1.5 flex items-center gap-2">
                <span className="rounded bg-amber-600 px-2 py-0.5 text-[11px] font-bold text-white">B2B</span>
                <span className="text-xs font-semibold uppercase tracking-wider text-amber-700">Wholesale Invoice</span>
              </div>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1">
                {inv.buyer_name && <Meta label="Buyer" value={inv.buyer_name} />}
                {inv.buyer_gstin && <Meta label="GSTIN" value={inv.buyer_gstin} mono />}
                {inv.buyer_address && <Meta label="Address" value={inv.buyer_address} />}
              </dl>
              <button
                onClick={async () => {
                  try {
                    const blob = await api.downloadB2BInvoicePDF(inv.id)
                    const url = URL.createObjectURL(blob)
                    const a = window.document.createElement('a')
                    a.href = url
                    a.download = `B2B_${inv.invoice_no}.pdf`
                    a.click()
                    setTimeout(() => URL.revokeObjectURL(url), 10_000)
                  } catch (err) {
                    alert(err instanceof Error ? err.message : String(err))
                  }
                }}
                className="mt-2 rounded-md border border-amber-600 bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white transition-colors hover:bg-amber-500"
              >
                Download B2B Invoice PDF
              </button>
            </div>
          )}

          {items.length === 0 ? (
            <p className="rounded-lg border border-dashed border-line px-4 py-8 text-center text-sm text-inksoft">
              No line items recorded on this invoice.
            </p>
          ) : (
            <div className="overflow-hidden rounded-lg border border-line">
              <table className="w-full text-sm">
                <thead className="bg-mint-50 shadow-[0_1px_0_var(--color-line)]">
                  <tr className="text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                    <th className="px-3 py-2">Medicine</th>
                    <th className="px-2 py-2">Batch</th>
                    <th className="px-2 py-2">HSN</th>
                    <th className="px-2 py-2 text-right">Qty</th>
                    {inv?.sale_type === 'B2B' && <th className="px-2 py-2 text-right">Bonus</th>}
                    <th className="px-2 py-2 text-right">{inv?.sale_type === 'B2B' ? 'Sell ₹' : 'Rate'}</th>
                    {inv?.sale_type === 'B2B' && <th className="px-2 py-2 text-right">MRP ₹</th>}
                    <th className="px-2 py-2 text-right">Tax</th>
                    <th className="px-2 py-2 text-right">Disc.</th>
                    <th className="px-3 py-2 text-right">Amount</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line-soft">
                  {items.map((it) => (
                    <tr key={it.id}>
                      <td className="max-w-[220px] truncate px-3 py-2 font-medium" title={it.medicine_name}>
                        {it.medicine_name}
                      </td>
                      <td className="whitespace-nowrap px-2 py-2 font-mono text-xs text-inksoft">{it.batch_number}</td>
                      <td className="whitespace-nowrap px-2 py-2 font-mono text-xs text-inksoft">{it.hsn_code ?? '—'}</td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{it.quantity}</td>
                      {inv?.sale_type === 'B2B' && (
                        <td className="px-2 py-2 text-right font-mono tabular-nums text-safe-text">
                          {it.bonus_quantity > 0 ? `+${it.bonus_quantity}` : '—'}
                        </td>
                      )}
                      <td className="px-2 py-2 text-right font-mono tabular-nums">₹{money(it.unit_sale_price)}</td>
                      {inv?.sale_type === 'B2B' && (
                        <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                          {it.mrp != null && it.mrp > 0 ? `₹${money(it.mrp)}` : '—'}
                        </td>
                      )}
                      <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                        {it.gst_rate != null && it.gst_rate > 0 ? `${it.gst_rate}%` : '—'}
                      </td>
                      <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                        {it.discount_amount > 0 ? `-₹${money(it.discount_amount)}` : '—'}
                      </td>
                      <td className="px-3 py-2 text-right font-mono font-semibold tabular-nums">₹{money(it.subtotal)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="space-y-1.5 rounded-xl border border-line bg-mint-50/70 px-4 py-3 text-sm">
            <TotalRow label="Gross amount" value={`₹${money(inv.gross_amount ?? gross)}`} muted />
            {inv.discount_total > 0 && (
              <TotalRow label="Discount" value={`−₹${money(inv.discount_total)}`} accent="text-brick-text" />
            )}
            {inv.taxable_amount != null && (
              <TotalRow label="Taxable value" value={`₹${money(inv.taxable_amount)}`} muted />
            )}
            {inv.supply_type === 'INTER_STATE' && inv.igst_total != null && inv.igst_total > 0 && (
              <TotalRow label="IGST" value={`₹${money(inv.igst_total)}`} muted />
            )}
            {inv.supply_type !== 'INTER_STATE' && (
              <>
                {inv.cgst_total != null && inv.cgst_total > 0 && (
                  <TotalRow label="CGST" value={`₹${money(inv.cgst_total)}`} muted />
                )}
                {inv.sgst_total != null && inv.sgst_total > 0 && (
                  <TotalRow label="SGST" value={`₹${money(inv.sgst_total)}`} muted />
                )}
              </>
            )}
            {inv.tax_total != null && inv.tax_total > 0 && (
              <TotalRow label="Tax total" value={`₹${money(inv.tax_total)}`} muted />
            )}
            <div className="flex items-end justify-between pt-1">
              <span className="text-xs font-semibold uppercase tracking-wider text-inksoft">
                {inv.grand_total != null ? 'Grand total' : 'Net payable'}
              </span>
              <span className="font-display text-2xl font-black tracking-tight tabular-nums">
                ₹{money(inv.grand_total ?? inv.total_amount)}
              </span>
            </div>
          </div>
        </>
      )}
    </DetailModal>
  )
}

export function Meta({
  label,
  value,
  mono,
  accent,
}: {
  label: string
  value: string
  mono?: boolean
  accent?: boolean
}) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] font-bold uppercase tracking-wider text-inksoft">{label}</dt>
      <dd
        className={
          'truncate font-medium ' +
          (mono ? 'font-mono text-[13px] tabular-nums ' : '') +
          (accent ? 'text-udhaar-text' : '')
        }
        title={value}
      >
        {value}
      </dd>
    </div>
  )
}

export function TotalRow({
  label,
  value,
  muted,
  accent,
}: {
  label: string
  value: string
  muted?: boolean
  accent?: string
}) {
  return (
    <div className={'flex items-center justify-between ' + (muted ? 'text-inksoft' : '')}>
      <span className="text-xs font-semibold uppercase tracking-wider">{label}</span>
      <span className={'font-mono font-semibold tabular-nums ' + (accent ?? '')}>{value}</span>
    </div>
  )
}