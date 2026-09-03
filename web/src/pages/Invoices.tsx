import { useCallback, useEffect, useState } from 'react'
import { api } from '../lib/api'
import { daysUntil, expiryClass, money, todayISO } from '../lib/format'
import Pagination, { usePaged } from '../components/Pagination'
import {
  DetailModal,
  Meta,
  SalesInvoiceModal,
  fmtDate,
  type LoadState,
} from '../components/SalesInvoiceModal'
import type {
  PurchaseInvoiceDetail,
  PurchaseInvoiceRow,
  SalesInvoiceRow,
} from '../types'

type Range = { start: string; end: string }

const QUICK_RANGES: { label: string; days: number }[] = [
  { label: 'Today', days: 0 },
  { label: '7 days', days: -6 },
  { label: '30 days', days: -29 },
  { label: '90 days', days: -89 },
]

const PAGE_SIZE = 8

export default function Invoices() {
  const [range, setRange] = useState<Range>({ start: todayISO(-29), end: todayISO() })
  const [query, setQuery] = useState('')
  const [applied, setApplied] = useState<{ range: Range; q: string }>({
    range: { start: todayISO(-29), end: todayISO() },
    q: '',
  })

  const [sales, setSales] = useState<SalesInvoiceRow[]>([])
  const [purchases, setPurchases] = useState<PurchaseInvoiceRow[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const [salesDetailFor, setSalesDetailFor] = useState<string | null>(null)
  const [purchaseDetailFor, setPurchaseDetailFor] = useState<string | null>(null)

  const salesPage = usePaged(sales, PAGE_SIZE)
  const purchasePage = usePaged(purchases, PAGE_SIZE)

  const search = useCallback(async (r: Range, q: string) => {
    setBusy(true)
    setError('')
    try {
      const [s, p] = await Promise.all([
        api.salesInvoices(r.start, r.end, q),
        api.purchaseInvoices(r.start, r.end, q),
      ])
      setSales(s.invoices)
      setPurchases(p.invoices)
      setApplied({ range: r, q })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void search({ start: todayISO(-29), end: todayISO() }, '')
  }, [search])

  const applyQuickRange = (days: number) => {
    const r = { start: todayISO(days), end: todayISO() }
    setRange(r)
    void search(r, query.trim())
  }

  const submitSearch = () => void search(range, query.trim())

  const totalSales = sales.reduce((a, s) => a + (s.grand_total ?? s.total_amount), 0)
  const totalPurchases = purchases.reduce((a, p) => a + (p.grand_total ?? p.total_amount), 0)

  return (
    <div className="space-y-6">
      {/* Search bar */}
      <section className="rounded-xl border border-line bg-white p-4 shadow-sm">
        <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
          <label className="min-w-[180px] flex-1 text-[10px] font-bold uppercase tracking-wider text-inksoft sm:max-w-xs">
            Invoice no.
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submitSearch()}
              placeholder="e.g. 1042 or PINV-2026…"
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            From
            <input
              type="date"
              value={range.start}
              max={range.end}
              onChange={(e) => setRange((r) => ({ ...r, start: e.target.value }))}
              onKeyDown={(e) => e.key === 'Enter' && submitSearch()}
              className="mt-1 block rounded-lg border border-line px-2.5 py-2 font-mono text-sm text-ink focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            To
            <input
              type="date"
              value={range.end}
              min={range.start}
              onChange={(e) =>
                setRange((r) => ({
                  ...r,
                  start: r.start > e.target.value ? e.target.value : r.start,
                  end: e.target.value,
                }))
              }
              onKeyDown={(e) => e.key === 'Enter' && submitSearch()}
              className="mt-1 block rounded-lg border border-line px-2.5 py-2 font-mono text-sm text-ink focus:border-pine-600"
            />
          </label>
          <button
            onClick={submitSearch}
            disabled={busy}
            className="h-[38px] rounded-lg bg-pine-700 px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
          >
            {busy ? 'Searching…' : 'Search'}
          </button>
          <button
            onClick={() => {
              setQuery('')
              const r = { start: todayISO(-29), end: todayISO() }
              setRange(r)
              void search(r, '')
            }}
            disabled={busy || (!query && applied.q === '')}
            className="h-[38px] rounded-lg border border-line px-4 py-2 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50 disabled:opacity-40"
          >
            Clear
          </button>
          <div className="ml-auto flex flex-wrap items-center gap-1.5">
            {QUICK_RANGES.map((q) => (
              <button
                key={q.label}
                onClick={() => applyQuickRange(q.days)}
                disabled={busy}
                title={`Last ${q.label.toLowerCase()} (${todayISO(q.days)} → today)`}
                className={
                  'rounded-full px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ' +
                  (range.start === todayISO(q.days) && range.end === todayISO()
                    ? 'bg-pine-700 text-white'
                    : 'border border-line bg-white text-inksoft hover:bg-mint-50')
                }
              >
                {q.label}
              </button>
            ))}
          </div>
        </div>
        {(error || !busy) && (
          <p className="mt-2 text-xs text-inksoft">
            {error ? (
              <span className="font-medium text-brick-text">{error}</span>
            ) : (
              <>
                Showing invoices dated{' '}
                <span className="font-mono font-semibold">{applied.range.start}</span> →{' '}
                <span className="font-mono font-semibold">{applied.range.end}</span>
                {applied.q && (
                  <>
                    {' '}
                    matching no. <span className="font-mono font-semibold">“{applied.q}”</span>
                  </>
                )}
              </>
            )}
          </p>
        )}
      </section>

      {/* Purchase invoices */}
      <InvoiceSection
        tone="purchase"
        title="Purchase invoices"
        subtitle="Stock received from suppliers"
        count={purchases.length}
        total={totalPurchases}
        pagination={
          <Pagination
            page={purchasePage.page}
            pageCount={purchasePage.pageCount}
            total={purchasePage.total}
            start={purchasePage.start}
            pageSize={PAGE_SIZE}
            onPage={purchasePage.setPage}
          />
        }
      >
        {purchases.length === 0 ? (
          <SectionEmpty cols={6}>No purchase invoices found in this window.</SectionEmpty>
        ) : (
          purchasePage.slice.map((p) => (
            <tr key={p.id} className="hover:bg-mint-50/60">
              <td className="px-4 py-2">
                <button
                  onClick={() => setPurchaseDetailFor(p.id)}
                  className="rounded font-mono text-sm font-semibold text-pine-700 underline-offset-2 hover:underline"
                  title="View invoice details"
                >
                  {p.invoice_no}
                </button>
              </td>
              <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-inksoft">
                {fmtDate(p.created_at)}
              </td>
              <td className="max-w-[240px] truncate px-3 py-2 font-medium" title={p.supplier_name}>
                {p.supplier_name}
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                {p.item_count} · {p.units_purchased} u
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                {p.discount_total > 0 ? `-₹${money(p.discount_total)}` : '—'}
              </td>
              <td className="px-4 py-2 text-right font-mono font-semibold tabular-nums">
                ₹{money(p.grand_total ?? p.total_amount)}
              </td>
              <td className="px-4 py-2 text-right">
                <button
                  onClick={() => setPurchaseDetailFor(p.id)}
                  className="rounded-md bg-pine-700 px-2.5 py-1 text-xs font-semibold text-white transition-colors hover:bg-pine-600"
                >
                  View
                </button>
              </td>
            </tr>
          ))
        )}
      </InvoiceSection>

      {/* Sales invoices */}
      <InvoiceSection
        tone="sales"
        title="Sales invoices"
        subtitle="Bills raised at the counter"
        count={sales.length}
        total={totalSales}
        pagination={
          <Pagination
            page={salesPage.page}
            pageCount={salesPage.pageCount}
            total={salesPage.total}
            start={salesPage.start}
            pageSize={PAGE_SIZE}
            onPage={salesPage.setPage}
          />
        }
      >
        {sales.length === 0 ? (
          <SectionEmpty cols={8}>No sales invoices found in this window.</SectionEmpty>
        ) : (
          salesPage.slice.map((s) => (
            <tr key={s.id} className="hover:bg-mint-50/60">
              <td className="px-4 py-2">
                <button
                  onClick={() => setSalesDetailFor(s.id)}
                  className="rounded font-mono text-sm font-semibold text-pine-700 underline-offset-2 hover:underline"
                  title="View invoice details"
                >
                  {s.invoice_no}
                </button>
                {s.sale_type === 'B2B' && (
                  <span className="ml-1.5 inline-block rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-bold text-amber-700">B2B</span>
                )}
              </td>
              <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-inksoft">
                {fmtDate(s.created_at)}
              </td>
              <td className="max-w-[200px] truncate px-3 py-2 font-medium" title={s.customer_name || undefined}>
                {s.customer_name || <span className="text-inksoft">Walk-in</span>}
              </td>
              <td className="px-2 py-2">
                <span
                  className={
                    'rounded-full px-2 py-0.5 text-[11px] font-semibold ' +
                    (s.payment_type === 'CREDIT' ? 'bg-udhaar-bg text-udhaar-text' : 'bg-safe-bg text-safe-text')
                  }
                >
                  {s.payment_type === 'CREDIT' ? 'Credit' : 'Cash'}
                </span>
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                {s.item_count} · {s.units_sold} u
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                {s.discount_total > 0 ? `-₹${money(s.discount_total)}` : '—'}
              </td>
              <td className="px-4 py-2 text-right font-mono font-semibold tabular-nums">
                ₹{money(s.grand_total ?? s.total_amount)}
              </td>
              <td className="px-4 py-2 text-right">
                <button
                  onClick={() => setSalesDetailFor(s.id)}
                  className="rounded-md bg-pine-700 px-2.5 py-1 text-xs font-semibold text-white transition-colors hover:bg-pine-600"
                >
                  View
                </button>
              </td>
            </tr>
          ))
        )}
      </InvoiceSection>

      {purchaseDetailFor && (
        <PurchaseInvoiceModal id={purchaseDetailFor} onClose={() => setPurchaseDetailFor(null)} />
      )}
      {salesDetailFor && (
        <SalesInvoiceModal id={salesDetailFor} onClose={() => setSalesDetailFor(null)} />
      )}
    </div>
  )
}

function InvoiceSection({
  tone,
  title,
  subtitle,
  count,
  total,
  pagination,
  children,
}: {
  tone: 'purchase' | 'sales'
  title: string
  subtitle: string
  count: number
  total: number
  pagination: React.ReactNode
  children: React.ReactNode
}) {
  const cols: (string | { label: string; align?: 'right' })[] =
    tone === 'purchase'
      ? ['Invoice no.', 'Date', 'Supplier', { label: 'Items', align: 'right' }, { label: 'Disc.', align: 'right' }, { label: 'Total', align: 'right' }, '']
      : ['Invoice no.', 'Date', 'Customer', 'Payment', { label: 'Items', align: 'right' }, { label: 'Discount', align: 'right' }, { label: 'Total', align: 'right' }, '']
  return (
    <section className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
      <header className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-line-soft px-4 pb-3 pt-3.5">
        <div>
          <h2 className="flex items-center gap-2 font-display text-sm font-bold uppercase tracking-wide">
            <span aria-hidden className={'inline-block h-2.5 w-2.5 rounded-full ' + (tone === 'purchase' ? 'bg-marigold-dot' : 'bg-pine-600')} />
            {title}
          </h2>
          <p className="mt-0.5 text-xs text-inksoft">{subtitle}</p>
        </div>
        <p className="text-sm tabular-nums">
          <span className="font-mono font-semibold">{count}</span>{' '}
          <span className="text-xs text-inksoft">{count === 1 ? 'invoice' : 'invoices'}</span>
          {count > 0 && (
            <>
              {' · '}
              <span className="font-display text-base font-black tracking-tight">₹{money(total)}</span>
            </>
          )}
        </p>
      </header>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] text-sm">
          <thead>
            <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] uppercase tracking-wider text-inksoft">
              {cols.map((c, i) => {
                const col = typeof c === 'string' ? { label: c } : c
                return (
                  <th
                    key={i}
                    className={
                      'py-2 font-bold ' +
                      (i === 0 || i === cols.length - 1 ? 'px-4 ' : 'px-2 ') +
                      (col.align === 'right' ? 'text-right' : '')
                    }
                  >
                    {col.label}
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-line-soft">{children}</tbody>
        </table>
      </div>
      {pagination}
    </section>
  )
}

function SectionEmpty({ cols, children }: { cols: number; children: React.ReactNode }) {
  return (
    <tr>
      <td colSpan={cols} className="px-4 py-10 text-center text-sm text-inksoft">
        {children}
      </td>
    </tr>
  )
}

// ---- Purchase detail modal ------------------------------------------------
// Reuses the shared DetailModal/ModalSkeleton/Meta/TotalRow machinery from
// components/SalesInvoiceModal.tsx. Every fetch state is explicit and visible:
// a skeleton while loading, a retryable error box on failure, an explicit
// note when an invoice carries no line items.

function PurchaseInvoiceModal({ id, onClose }: { id: string; onClose: () => void }) {
  const [detail, setDetail] = useState<PurchaseInvoiceDetail | null>(null)
  const [status, setStatus] = useState<LoadState>('loading')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    setStatus('loading')
    setDetail(null)
    setError('')
    void api
      .purchaseInvoice(id)
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

  return (
    <DetailModal
      onClose={onClose}
      status={status}
      error={error}
      onRetry={() => setAttempt((n) => n + 1)}
      title={
        inv ? (
          <>
            Purchase invoice <span className="font-mono">{inv.invoice_no}</span>
          </>
        ) : (
          'Purchase invoice'
        )
      }
    >
      {inv && (
        <>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-lg bg-marigold-bg/60 px-3.5 py-3 text-sm sm:grid-cols-3">
            <Meta label="Date" value={fmtDate(inv.created_at)} mono />
            <Meta label="Supplier" value={inv.supplier_name} />
            <Meta label="Items" value={`${items.length}`} mono />
            {inv.supplier_gstin && (
              <Meta label="GSTIN" value={inv.supplier_gstin} mono />
            )}
            {inv.supply_type && (
              <Meta label="Supply" value={inv.supply_type === 'INTER_STATE' ? 'Inter-state (IGST)' : 'Intra-state (CGST+SGST)'} />
            )}
          </dl>

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
                    <th className="px-2 py-2">Expiry</th>
                    <th className="px-2 py-2 text-right">Qty</th>
                    <th className="px-2 py-2 text-right">Bonus</th>
                    <th className="px-2 py-2 text-right">Buy ₹</th>
                    <th className="px-2 py-2 text-right">MRP ₹</th>
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
                      <td className="whitespace-nowrap px-2 py-2">
                        <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(daysUntil(it.expiry_date))}`}>
                          {it.expiry_date}
                        </span>
                      </td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{it.quantity}</td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums text-safe-text">
                        {it.bonus_quantity > 0 ? `+${it.bonus_quantity}` : '—'}
                      </td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{money(it.purchase_price)}</td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums text-inksoft">{money(it.sale_price)}</td>
                      <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                        {it.gst_rate != null && it.gst_rate > 0 ? `${it.gst_rate}%` : '—'}
                      </td>
                      <td className="px-2 py-2 text-right font-mono text-xs tabular-nums text-inksoft">
                        {it.discount_amount > 0 ? `-₹${money(it.discount_amount)}` : '—'}
                      </td>
                      <td className="px-3 py-2 text-right font-mono font-semibold tabular-nums">
                        ₹{money(it.line_total ?? (it.quantity * it.purchase_price - it.discount_amount))}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="space-y-1.5 rounded-xl border border-line bg-marigold-bg/60 px-4 py-3 text-sm">
            {inv.discount_total > 0 && (
              <div className="flex items-center justify-between text-inksoft">
                <span className="text-xs font-semibold uppercase tracking-wider">PO Discount</span>
                <span className="font-mono font-semibold text-brick-text tabular-nums">−₹{money(inv.discount_total)}</span>
              </div>
            )}
            {inv.taxable_amount != null && (
              <div className="flex items-center justify-between text-inksoft">
                <span className="text-xs font-semibold uppercase tracking-wider">Taxable value</span>
                <span className="font-mono font-semibold tabular-nums">₹{money(inv.taxable_amount)}</span>
              </div>
            )}
            {inv.supply_type === 'INTER_STATE' && inv.igst_total != null && inv.igst_total > 0 && (
              <div className="flex items-center justify-between text-inksoft">
                <span className="text-xs font-semibold uppercase tracking-wider">IGST</span>
                <span className="font-mono font-semibold tabular-nums">₹{money(inv.igst_total)}</span>
              </div>
            )}
            {inv.supply_type !== 'INTER_STATE' && (
              <>
                {inv.cgst_total != null && inv.cgst_total > 0 && (
                  <div className="flex items-center justify-between text-inksoft">
                    <span className="text-xs font-semibold uppercase tracking-wider">CGST</span>
                    <span className="font-mono font-semibold tabular-nums">₹{money(inv.cgst_total)}</span>
                  </div>
                )}
                {inv.sgst_total != null && inv.sgst_total > 0 && (
                  <div className="flex items-center justify-between text-inksoft">
                    <span className="text-xs font-semibold uppercase tracking-wider">SGST</span>
                    <span className="font-mono font-semibold tabular-nums">₹{money(inv.sgst_total)}</span>
                  </div>
                )}
              </>
            )}
            {inv.tax_total != null && inv.tax_total > 0 && (
              <div className="flex items-center justify-between border-t border-line pt-1 text-inksoft">
                <span className="text-xs font-semibold uppercase tracking-wider">Tax total</span>
                <span className="font-mono font-semibold tabular-nums">₹{money(inv.tax_total)}</span>
              </div>
            )}
            <div className="flex items-end justify-between">
              <span className="text-xs font-semibold uppercase tracking-wider text-inksoft">
                {inv.grand_total != null ? 'Grand total' : 'Purchase total'}
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