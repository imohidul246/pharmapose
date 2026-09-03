import { useCallback, useEffect, useState } from 'react'
import { api } from '../lib/api'
import { expiryClass, money, todayISO } from '../lib/format'
import Pagination, { usePaged } from '../components/Pagination'
import type {
  ExpiringBatch,
  LowStockItem,
  ProfitLossReport,
  PurchaseReport,
  SalesReport,
} from '../types'

type Range = { start: string; end: string }

export default function Reports() {
  const [range, setRange] = useState<Range>({ start: todayISO(-29), end: todayISO() })
  const [sales, setSales] = useState<SalesReport | null>(null)
  const [pl, setPl] = useState<ProfitLossReport | null>(null)
  const [purchases, setPurchases] = useState<PurchaseReport | null>(null)
  const [expiring, setExpiring] = useState<ExpiringBatch[]>([])
  const [lowStock, setLowStock] = useState<LowStockItem[]>([])
  const [windowMonths, setWindowMonths] = useState<3 | 6 | 12>(6)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async (r: Range, months: number) => {
    setBusy(true)
    setError('')
    try {
      const [s, p, pur, exp, low] = await Promise.all([
        api.salesReport(r.start, r.end),
        api.profitLoss(r.start, r.end),
        api.purchaseReport(r.start, r.end),
        api.expiry(months),
        api.lowStock(),
      ])
      setSales(s)
      setPl(p)
      setPurchases(pur)
      setExpiring(exp.batches)
      setLowStock(low.items)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void load(range, windowMonths)
  }, [load, windowMonths])

  const cash = sales?.breakdown.find((b) => b.payment_type === 'CASH')
  const credit = sales?.breakdown.find((b) => b.payment_type === 'CREDIT')
  const cashShare =
    sales && sales.net_sales > 0 ? ((cash?.total ?? 0) / sales.net_sales) * 100 : 0

  const plPage = usePaged(pl?.lines ?? [], 10)
  const expiryPage = usePaged(expiring, 10)
  const lowPage = usePaged(lowStock, 10)
  const pagerProps = (p: ReturnType<typeof usePaged>) => ({
    page: p.page,
    pageCount: p.pageCount,
    total: p.total,
    start: p.start,
    pageSize: 10,
    onPage: p.setPage,
  })

  return (
    <div className="space-y-6">
      <div className="no-print flex flex-wrap items-end gap-x-4 gap-y-3 rounded-xl border border-line bg-white p-4 shadow-sm">
        <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
          From
          <input
            type="date"
            value={range.start}
            onChange={(e) => setRange((r) => ({ ...r, start: e.target.value }))}
            className="mt-1 block rounded-lg border border-line px-2.5 py-1.5 font-mono text-sm text-ink focus:border-pine-600"
          />
        </label>
        <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
          To
          <input
            type="date"
            value={range.end}
            onChange={(e) => setRange((r) => ({ ...r, end: e.target.value }))}
            className="mt-1 block rounded-lg border border-line px-2.5 py-1.5 font-mono text-sm text-ink focus:border-pine-600"
          />
        </label>
        <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Expiry window
          <select
            value={windowMonths}
            onChange={(e) => setWindowMonths(Number(e.target.value) as 3 | 6 | 12)}
            className="mt-1 block rounded-lg border border-line px-2.5 py-1.5 text-sm text-ink focus:border-pine-600"
          >
            <option value={3}>Next 3 months</option>
            <option value={6}>Next 6 months</option>
            <option value={12}>Next 12 months</option>
          </select>
        </label>
        <button
          onClick={() => void load(range, windowMonths)}
          disabled={busy}
          className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
        >
          {busy ? 'Loading…' : 'Refresh'}
        </button>
        <button
          onClick={() => window.print()}
          className="rounded-lg border border-line px-4 py-2 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50"
        >
          Print
        </button>
        {error && (
          <span className="rounded-lg bg-brick-bg px-3 py-2 text-sm font-medium text-brick-text">
            {error}
          </span>
        )}
      </div>

      {/* Sales summary */}
      <section className="grid gap-4 md:grid-cols-3 print:block">
        <Card eyebrow="Net sales" note={`${sales?.breakdown.reduce((a, b) => a + b.invoices, 0) ?? 0} invoices · ${sales?.net_units ?? 0} units sold`}>
          ₹{money(sales?.net_sales ?? 0)}
        </Card>

        <Card eyebrow="Cash vs credit" plain>
          <div className="flex h-2.5 overflow-hidden rounded-full bg-line">
            <div className="bg-pine-600 transition-all" style={{ width: `${cashShare}%` }} />
            <div className="bg-udhaar" style={{ width: `${100 - cashShare}%` }} />
          </div>
          <dl className="mt-3 space-y-1.5 text-xs">
            <div className="flex items-center justify-between gap-3">
              <dt className="flex items-center gap-1.5">
                <span aria-hidden className="h-2 w-2 rounded-full bg-pine-600" />
                Cash · {cashShare.toFixed(1)}%
              </dt>
              <dd className="font-mono font-semibold tabular-nums">₹{money(cash?.total ?? 0)}</dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="flex items-center gap-1.5">
                <span aria-hidden className="h-2 w-2 rounded-full bg-udhaar" />
                Credit · {(100 - cashShare).toFixed(1)}%
              </dt>
              <dd className="font-mono font-semibold tabular-nums">₹{money(credit?.total ?? 0)}</dd>
            </div>
          </dl>
        </Card>

        <Card
          eyebrow={`Purchases · ${range.start} → ${range.end}`}
          note={`${purchases?.order_count ?? 0} orders · ${purchases?.item_count ?? 0} line items`}
        >
          ₹{money(purchases?.total_spend ?? 0)}
        </Card>
      </section>

      {/* P&L */}
      <section className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-line-soft bg-white px-4 py-3">
          <h3 className="font-display text-sm font-bold uppercase tracking-wide">
            Profit &amp; Loss — by product line
          </h3>
          <p className="text-sm tabular-nums">
            <span className="font-mono font-semibold">₹{money(pl?.total_profit ?? 0)}</span>
            <span
              className={
                'ml-2 rounded px-1.5 py-0.5 font-mono text-xs font-semibold ' +
                ((pl?.margin_pct ?? 0) >= 0 ? 'bg-safe-bg text-safe-text' : 'bg-brick-bg text-brick-text')
              }
            >
              {(pl?.margin_pct ?? 0).toFixed(1)}% margin
            </span>
          </p>
        </header>
        <Table
          cols={[
            'Medicine',
            { label: 'Units', align: 'right' },
            { label: 'Revenue', align: 'right' },
            { label: 'Cost (batch)', align: 'right' },
            { label: 'Profit', align: 'right' },
            { label: 'Margin', align: 'right' },
          ]}
        >
          {plPage.slice.map((l) => (
            <tr key={l.medicine_id} className="hover:bg-mint-50/60">
              <td className="max-w-[280px] truncate px-4 py-1.5 font-medium">{l.medicine_name}</td>
              <td className="px-2 py-1.5 text-right font-mono tabular-nums">{l.units_sold}</td>
              <td className="px-2 py-1.5 text-right font-mono tabular-nums">₹{money(l.revenue)}</td>
              <td className="px-2 py-1.5 text-right font-mono tabular-nums">₹{money(l.cost)}</td>
              <td
                className={
                  'px-2 py-1.5 text-right font-mono font-semibold tabular-nums ' +
                  (l.profit >= 0 ? 'text-safe-text' : 'text-brick-text')
                }
              >
                ₹{money(l.profit)}
              </td>
              <td className="px-4 py-1.5 text-right font-mono tabular-nums">{l.margin_pct.toFixed(1)}%</td>
            </tr>
          ))}
          {(pl?.lines ?? []).length === 0 && (
            <EmptyRow cols={6}>
              No sales in the selected window — widen the date range.
            </EmptyRow>
          )}
        </Table>
        <Pagination {...pagerProps(plPage)} />
      </section>

      {/* Risk matrix */}
      <section className="grid gap-5 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)] print:block">
        <Panel title={`Risk matrix — batches expiring within ${windowMonths} mo`}>
          <Table
            minWClass="min-w-[560px]"
            cols={[
              'Medicine',
              'Batch',
              'Expiry',
              { label: 'Qty', align: 'right' },
              { label: 'Value at risk', align: 'right' },
            ]}
          >
            {expiryPage.slice.map((b) => (
              <tr key={b.batch_id} className="hover:bg-mint-50/60">
                <td className="max-w-[200px] truncate px-4 py-1.5 font-medium">{b.medicine_name}</td>
                <td className="px-2 py-1.5 font-mono text-xs">{b.batch_number}</td>
                <td className="px-2 py-1.5">
                  <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(daysTo(b.expiry_date))}`}>
                    {b.expiry_date}{b.expired ? ' · EXPIRED' : ''}
                  </span>
                </td>
                <td className="px-2 py-1.5 text-right font-mono tabular-nums">{b.current_stock}</td>
                <td className="px-4 py-1.5 text-right font-mono tabular-nums">₹{money(b.stock_value)}</td>
              </tr>
            ))}
            {expiring.length === 0 && (
              <EmptyRow cols={5} tone="safe">
                Nothing expiring inside this window.
              </EmptyRow>
            )}
          </Table>
          <Pagination {...pagerProps(expiryPage)} />
        </Panel>

        <CreditLedger />

        <div className="lg:col-span-2 overflow-hidden rounded-xl border border-line bg-white shadow-sm">
          <header className="border-b border-line-soft px-4 py-3 font-display text-sm font-bold uppercase tracking-wide">
            Low stock — below reorder level
          </header>
          <ul className="divide-y divide-line-soft text-sm">
            {lowPage.slice.map((it) => (
              <li key={it.medicine_id} className="flex items-center justify-between gap-4 px-4 py-2 hover:bg-mint-50/60">
                <span className="truncate font-medium">{it.medicine_name}</span>
                <span className="shrink-0 text-xs text-inksoft">
                  <span className="font-mono font-bold text-brick-text">{it.total_stock}</span> on hand
                  {' · '}reorder at <span className="font-mono">{it.min_reorder_level}</span>{' '}
                  <span className="ml-1 rounded bg-marigold-bg px-1.5 py-0.5 font-mono font-semibold text-marigold-text">
                    short {it.shortfall}
                  </span>
                </span>
              </li>
            ))}
            {lowStock.length === 0 && (
              <li className="px-4 py-8 text-center text-sm font-medium text-safe-text">
                All tracked medicines above reorder level.
              </li>
            )}
          </ul>
          <Pagination {...pagerProps(lowPage)} />
        </div>
      </section>
    </div>
  )
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
      <header className="border-b border-line-soft px-4 py-3 font-display text-sm font-bold uppercase tracking-wide">
        {title}
      </header>
      {children}
    </div>
  )
}

function Table({
  cols,
  children,
  minWClass,
}: {
  cols: (string | { label: string; align?: 'right' })[]
  children: React.ReactNode
  minWClass?: string
}) {
  return (
    <table className={(minWClass ? minWClass + ' ' : '') + 'w-full text-sm'}>
      <thead>
        <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] uppercase tracking-wider text-inksoft">
          {cols.map((c, i) => {
            const col = typeof c === 'string' ? { label: c } : c
            return (
              <th
                key={col.label}
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
  )
}

function EmptyRow({
  cols,
  children,
  tone,
}: {
  cols: number
  children: React.ReactNode
  tone?: 'safe'
}) {
  return (
    <tr>
      <td colSpan={cols} className={'px-4 py-8 text-center text-sm ' + (tone === 'safe' ? 'font-medium text-safe-text' : 'text-inksoft')}>
        {children}
      </td>
    </tr>
  )
}

function Card({
  eyebrow,
  children,
  note,
  plain,
}: {
  eyebrow: string
  children: React.ReactNode
  note?: string
  plain?: boolean
}) {
  return (
    <div className="rounded-xl border border-line bg-white p-4 shadow-sm">
      <h3 className="text-[10px] font-bold uppercase tracking-[0.16em] text-inksoft">{eyebrow}</h3>
      {!plain && (
        <>
          <p className="mt-1.5 font-display text-[28px] leading-none font-black tracking-tight tabular-nums">
            {children}
          </p>
          {note && <p className="mt-2 text-xs text-inksoft">{note}</p>}
        </>
      )}
      {plain && children}
    </div>
  )
}

function CreditLedger() {
  const [customers, setCustomers] = useState<Awaited<ReturnType<typeof api.customers>>['customers']>([])
  useEffect(() => {
    void api
      .customers()
      .then((res) => setCustomers(res.customers))
      .catch(() => {})
  }, [])
  const ledgerPage = usePaged(customers, 10)

  return (
    <Panel title="Credit ledger matrix">
      <Table
        cols={[
          'Customer',
          { label: 'Balance', align: 'right' },
          { label: 'Limit', align: 'right' },
          'Utilization',
        ]}
      >
        {ledgerPage.slice.map((c) => {
          const pct = c.credit_limit > 0 ? Math.min((c.current_balance / c.credit_limit) * 100, 999) : c.current_balance > 0 ? 100 : 0
          const overdrawn = c.credit_limit > 0 && c.current_balance > c.credit_limit
          return (
            <tr key={c.id} className="hover:bg-mint-50/60">
              <td className="px-4 py-1.5">
                <p className="font-medium">{c.name}</p>
                <p className="font-mono text-xs text-inksoft">{c.phone}</p>
              </td>
              <td className="px-2 py-1.5 text-right font-mono font-semibold tabular-nums">
                ₹{money(c.current_balance)}
              </td>
              <td className="px-2 py-1.5 text-right font-mono tabular-nums text-inksoft">
                ₹{money(c.credit_limit)}
              </td>
              <td className="px-4 py-1.5">
                <div className="flex items-center gap-2">
                  <div className="h-2 flex-1 overflow-hidden rounded-full bg-line">
                    <div
                      className={
                        overdrawn ? 'h-full bg-brick' : pct >= 80 ? 'h-full bg-marigold-dot' : 'h-full bg-pine-600'
                      }
                      style={{ width: `${Math.min(pct, 100)}%` }}
                    />
                  </div>
                  {overdrawn && (
                    <span className="rounded bg-brick-bg px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-brick-text">
                      Over limit
                    </span>
                  )}
                </div>
              </td>
            </tr>
          )
        })}
        {customers.length === 0 && (
          <EmptyRow cols={4}>No credit customers yet — add them under Khata.</EmptyRow>
        )}
      </Table>
      <Pagination
        page={ledgerPage.page}
        pageCount={ledgerPage.pageCount}
        total={ledgerPage.total}
        start={ledgerPage.start}
        pageSize={10}
        onPage={ledgerPage.setPage}
      />
    </Panel>
  )
}

function daysTo(dateStr: string): number {
  return Math.round((new Date(dateStr + 'T00:00:00Z').getTime() - new Date(new Date().toISOString().slice(0, 10) + 'T00:00:00Z').getTime()) / 86400000)
}
