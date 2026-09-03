import { useCallback, useEffect, useState } from 'react'
import { api } from '../lib/api'
import { daysUntil, expiryClass, money } from '../lib/format'
import type {
  PurchaseRequest,
  PurchaseSnapshot,
  RequestStatus,
  StockAuditRequest,
  StockAuditRequestItem,
} from '../types'

function decodeSnapshot(b64?: string): PurchaseSnapshot | null {
  if (!b64) return null
  try {
    return JSON.parse(atob(b64)) as PurchaseSnapshot
  } catch {
    return null
  }
}

function timeAgo(iso: string): string {
  const dt = new Date(iso)
  if (Number.isNaN(dt.getTime())) return ''
  return dt.toLocaleString('en-IN', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export default function Approvals() {
  const [filter, setFilter] = useState<RequestStatus | 'ALL'>('PENDING')
  const [purchases, setPurchases] = useState<PurchaseRequest[]>([])
  const [audits, setAudits] = useState<StockAuditRequest[]>([])
  const [openPurchase, setOpenPurchase] = useState<string | null>(null)
  const [openAudit, setOpenAudit] = useState<string | null>(null)
  const [rejectTarget, setRejectTarget] = useState<
    { kind: 'purchase' | 'audit'; id: string; requester: string } | null
  >(null)
  const [rejectReason, setRejectReason] = useState('')
  const [stamp, setStamp] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    const [p, a] = await Promise.all([api.purchaseRequests(filter), api.stockAuditRequests(filter)])
    setPurchases(p.requests)
    setAudits(a.requests)
  }, [filter])

  useEffect(() => {
    void reload().catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [reload])

  const approve = async (kind: 'purchase' | 'audit', id: string) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (kind === 'purchase') {
        const res = await api.approvePurchaseRequest(id)
        setStamp(`Purchase ${res.purchase_order.invoice_no} approved and stocked`)
      } else {
        const res = await api.approveStockAuditRequest(id)
        setStamp(`Audit journal ${res.journal.id.slice(0, 8)}… approved`)
      }
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const confirmReject = async () => {
    if (!rejectTarget) return
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (rejectTarget.kind === 'purchase') {
        await api.rejectPurchaseRequest(rejectTarget.id, rejectReason.trim())
      } else {
        await api.rejectStockAuditRequest(rejectTarget.id, rejectReason.trim())
      }
      setRejectTarget(null)
      setRejectReason('')
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const pendingCount = purchases.filter((p) => p.status === 'PENDING').length +
    audits.filter((a) => a.status === 'PENDING').length

  return (
    <div className="mx-auto max-w-[1100px] space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="font-display text-lg font-bold tracking-tight">Approval counter</h2>
          <p className="text-xs text-inksoft">
            Staff submissions wait here for your sign-off. Approving a purchase stocks the batches;
            approving an audit posts the corrections.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'rounded-full px-2.5 py-1 font-mono text-xs font-semibold tabular-nums ' +
              (pendingCount > 0 ? 'bg-marigold-bg text-marigold-text' : 'bg-safe-bg text-safe-text')
            }
          >
            {pendingCount} pending
          </span>
          <div className="flex overflow-hidden rounded-lg border border-line bg-white text-sm">
            {(['PENDING', 'ALL'] as const).map((f) => (
              <button
                key={f}
                onClick={() => setFilter(f)}
                className={
                  'px-3 py-1.5 text-xs font-semibold transition-colors ' +
                  (filter === f ? 'bg-pine-700 text-white' : 'bg-white text-inksoft hover:bg-mint-50')
                }
              >
                {f === 'ALL' ? 'All' : 'Pending'}
              </button>
            ))}
          </div>
        </div>
      </header>

      {stamp && (
        <div className="flex items-center gap-4 rounded-xl border border-dashed border-pine-600/60 bg-white p-3.5 shadow-sm">
          <span aria-hidden className="stamp shrink-0 px-2.5 py-1 text-[11px]">
            Approved
          </span>
          <p className="min-w-0 flex-1 text-sm leading-snug">{stamp}</p>
          <button
            onClick={() => setStamp(null)}
            className="shrink-0 rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
          >
            Dismiss
          </button>
        </div>
      )}

      {error && (
        <p role="alert" className="rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
          {error}
        </p>
      )}

      <section className="space-y-3">
            <h3 className="font-display text-sm font-bold uppercase tracking-wide text-inksoft">
              Purchase requests
            </h3>
            {purchases.length === 0 ? (
              <Empty state={filter} label="No purchase requests — nothing awaiting your sign." />
            ) : (
              <ul className="divide-y divide-line-soft overflow-hidden rounded-xl border border-line bg-white shadow-sm">
                {purchases.map((req) => (
                  <PurchaseRow
                    key={req.id}
                    req={req}
                    open={openPurchase === req.id}
                    onToggle={() => setOpenPurchase(openPurchase === req.id ? null : req.id)}
                    onApprove={() => void approve('purchase', req.id)}
                    onReject={() => setRejectTarget({ kind: 'purchase', id: req.id, requester: req.requester_name ?? 'staff' })}
                    busy={busy}
                  />
                ))}
              </ul>
            )}
          </section>

          <section className="space-y-3">
            <h3 className="font-display text-sm font-bold uppercase tracking-wide text-inksoft">
              Stock audit requests
            </h3>
            {audits.length === 0 ? (
              <Empty state={filter} label="No stock audits on the counter." />
            ) : (
              <ul className="divide-y divide-line-soft overflow-hidden rounded-xl border border-line bg-white shadow-sm">
                {audits.map((req) => (
                  <AuditRow
                    key={req.id}
                    req={req}
                    open={openAudit === req.id}
                    onToggle={() => setOpenAudit(openAudit === req.id ? null : req.id)}
                    onApprove={() => void approve('audit', req.id)}
                    onReject={() => setRejectTarget({ kind: 'audit', id: req.id, requester: req.requester_name ?? 'staff' })}
                    busy={busy}
                  />
                ))}
              </ul>
            )}
          </section>

      {rejectTarget && (
        <div className="fixed inset-0 z-30 flex items-center justify-center bg-pine-950/55 p-4 backdrop-blur-[2px]">
          <div className="w-full max-w-md space-y-3 rounded-2xl bg-white p-5 shadow-2xl">
            <h4 className="font-display text-base font-bold tracking-tight">Reject submission</h4>
            <p className="text-xs text-inksoft">
              {rejectTarget.requester} will see the reason back on their counter. Stock is untouched either way.
            </p>
            <textarea
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              rows={3}
              placeholder="Why is this going back? e.g. batch qty doesn't match the bill"
              className="w-full resize-none rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => { setRejectTarget(null); setRejectReason('') }}
                className="rounded-lg border border-line px-3 py-2 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
              >
                Cancel
              </button>
              <button
                onClick={() => void confirmReject()}
                disabled={busy || !rejectReason.trim()}
                className="rounded-lg bg-brick px-4 py-2 text-xs font-bold text-white transition-colors hover:bg-brick-text disabled:bg-line disabled:text-inksoft"
              >
                {busy ? 'Rejecting…' : 'Reject with reason'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function StatusPill({ status }: { status: RequestStatus }) {
  const cls =
    status === 'PENDING'
      ? 'bg-marigold-bg text-marigold-text'
      : status === 'APPROVED'
        ? 'bg-safe-bg text-safe-text'
        : status === 'REJECTED'
          ? 'bg-brick-bg text-brick-text'
          : 'bg-line/70 text-inksoft'
  return (
    <span className={'rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ' + cls}>
      {status}
    </span>
  )
}

function PurchaseRow({
  req,
  open,
  onToggle,
  onApprove,
  onReject,
  busy,
}: {
  req: PurchaseRequest
  open: boolean
  onToggle: () => void
  onApprove: () => void
  onReject: () => void
  busy: boolean
}) {
  const snap = decodeSnapshot(req.purchase_snapshot)
  const itemCount = snap?.items.length ?? 0
  const total = snap?.items.reduce((acc, it) => {
    const gross = it.quantity * it.purchase_price
    const disc = it.discount_type === 'percent' ? (gross * it.discount_value) / 100 : it.discount_value
    return acc + gross - Math.min(Math.max(disc, 0), gross)
  }, 0) ?? 0
  const totalAfterPo = Math.max(0, total - (snap?.discount_total ?? 0))

  return (
    <li>
      <div
        className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors hover:bg-mint-50/40"
        onClick={onToggle}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        }}
      >
        <span className="min-w-0 flex-1">
          <p className="truncate font-semibold">
            {snap?.supplier_name ?? 'Purchase request'}
          </p>
          <p className="mt-0.5 truncate text-xs text-inksoft">
            {itemCount} {itemCount === 1 ? 'item' : 'items'} · {timeAgo(req.created_at)} · by{' '}
            {req.requester_name ?? 'staff'}
          </p>
        </span>
        <span className="font-mono text-sm font-semibold tabular-nums">
          ₹{money(totalAfterPo)}
        </span>
        <StatusPill status={req.status} />
        <svg
          viewBox="0 0 12 12"
          className={'h-3 w-3 text-inksoft/50 transition-transform ' + (open ? 'rotate-180' : '')}
          stroke="currentColor"
          strokeWidth="1.6"
          fill="none"
        >
          <path d="M3 4.5l3 3 3-3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>

      {open && req.status === 'PENDING' && (
        <div className="border-t border-dashed border-line bg-porcelain/60 p-4">
          {snap ? (
            <div className="space-y-2">
              <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-inksoft">
                {snap.invoice_no && (
                  <span>
                    Invoice <span className="font-mono font-semibold">{snap.invoice_no}</span>
                  </span>
                )}
                {snap.supplier_gstin && (
                  <span>
                    GSTIN <span className="font-mono font-semibold">{snap.supplier_gstin}</span>
                  </span>
                )}
                {snap.supplier_state && (
                  <span>
                    State <span className="font-mono font-semibold">{snap.supplier_state}</span>
                  </span>
                )}
              </div>
              <div className="overflow-x-auto rounded-lg border border-line bg-white">
                <table className="w-full min-w-[640px] text-sm">
                  <thead>
                    <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                      <th className="px-3 py-2">Medicine</th>
                      <th className="px-2 py-2">Batch</th>
                      <th className="px-2 py-2">Expiry</th>
                      <th className="px-2 py-2 text-right">Qty</th>
                      <th className="px-2 py-2 text-right">Buy ₹</th>
                      <th className="px-2 py-2 text-right">MRP ₹</th>
                      <th className="px-2 py-2 text-right">Disc</th>
                      <th className="px-3 py-2 text-right">Line</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line-soft">
                    {snap.items.map((it, i) => {
                      const gross = it.quantity * it.purchase_price
                      const disc =
                        it.discount_type === 'percent'
                          ? (gross * it.discount_value) / 100
                          : it.discount_value
                      const discAmt = Math.min(Math.max(disc, 0), gross)
                      return (
                        <tr key={i}>
                          <td className="max-w-[220px] px-3 py-2">
                            <p className="truncate font-medium">{it.medicine_name ?? 'Catalog item'}</p>
                            {it.hsn_code && (
                              <p className="truncate font-mono text-[11px] text-inksoft">HSN {it.hsn_code}</p>
                            )}
                          </td>
                          <td className="px-2 py-2 font-mono text-xs">{it.batch_number}</td>
                          <td className="px-2 py-2">
                            <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(daysUntil(it.expiry_date))}`}>
                              {it.expiry_date}
                            </span>
                          </td>
                          <td className="px-2 py-2 text-right font-mono tabular-nums">
                            {it.quantity}
                            {it.bonus_quantity > 0 && (
                              <span className="text-safe-text"> +{it.bonus_quantity} free</span>
                            )}
                          </td>
                          <td className="px-2 py-2 text-right font-mono tabular-nums">{money(it.purchase_price)}</td>
                          <td className="px-2 py-2 text-right font-mono tabular-nums">{money(it.sale_price)}</td>
                          <td className="px-2 py-2 text-right font-mono tabular-nums">
                            {it.discount_value > 0
                              ? it.discount_type === 'percent'
                                ? `${it.discount_value}%`
                                : `₹${money(it.discount_value)}`
                              : '—'}
                          </td>
                          <td className="px-3 py-2 text-right font-mono font-semibold tabular-nums">
                            ₹{money(gross - discAmt)}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
              {snap.discount_total > 0 && (
                <p className="px-1 text-xs text-inksoft">
                  PO discount <span className="font-mono font-semibold">−₹{money(snap.discount_total)}</span> ·{' '}
                  payable <span className="font-mono font-semibold">₹{money(totalAfterPo)}</span>
                </p>
              )}
              <div className="flex justify-end gap-2 pt-1">
                <button
                  onClick={onReject}
                  className="rounded-lg border border-line bg-white px-3 py-2 text-xs font-semibold text-inksoft transition-colors hover:bg-brick-bg hover:text-brick-text"
                >
                  Reject
                </button>
                <button
                  onClick={onApprove}
                  disabled={busy}
                  className="rounded-lg bg-pine-700 px-4 py-2 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
                >
                  {busy ? 'Approving…' : 'Approve & stock'}
                </button>
              </div>
            </div>
          ) : (
            <p className="text-xs text-inksoft">Snapshot unavailable.</p>
          )}
        </div>
      )}
    </li>
  )
}

function AuditRow({
  req,
  open,
  onToggle,
  onApprove,
  onReject,
  busy,
}: {
  req: StockAuditRequest
  open: boolean
  onToggle: () => void
  onApprove: () => void
  onReject: () => void
  busy: boolean
}) {
  const [items, setItems] = useState<StockAuditRequestItem[] | null>(null)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    void api
      .getStockAuditRequest(req.id)
      .then((res) => {
        if (!cancelled) setItems(res.items)
      })
      .catch(() => setItems([]))
    return () => {
      cancelled = true
    }
  }, [open, req.id])

  return (
    <li>
      <div
        className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors hover:bg-mint-50/40"
        onClick={onToggle}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        }}
      >
        <span className="min-w-0 flex-1">
          <p className="truncate font-semibold">
            Stock audit
            {req.notes && <span className="ml-1.5 font-normal text-inksoft">— {req.notes}</span>}
          </p>
          <p className="mt-0.5 truncate text-xs text-inksoft">
            {timeAgo(req.created_at)} · by {req.requester_name ?? 'staff'}
          </p>
        </span>
        <StatusPill status={req.status} />
        <svg
          viewBox="0 0 12 12"
          className={'h-3 w-3 text-inksoft/50 transition-transform ' + (open ? 'rotate-180' : '')}
          stroke="currentColor"
          strokeWidth="1.6"
          fill="none"
        >
          <path d="M3 4.5l3 3 3-3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>

      {open && items && items.length > 0 && req.status === 'PENDING' && (
        <div className="border-t border-dashed border-line bg-porcelain/60 p-4">
          <div className="overflow-x-auto rounded-lg border border-line bg-white">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                  <th className="px-3 py-2">Medicine</th>
                  <th className="px-2 py-2">Batch</th>
                  <th className="px-2 py-2 text-right">Counted</th>
                  <th className="px-2 py-2 text-right">System</th>
                  <th className="px-2 py-2 text-center">Drift</th>
                  <th className="px-3 py-2">Reason</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-soft">
                {items.map((it, i) => {
                  const variance = it.physical_quantity - it.system_quantity
                  return (
                    <tr key={i}>
                      <td className="max-w-[240px] px-3 py-2">
                        <p className="truncate font-medium">{it.medicine_name ?? 'Item'}</p>
                      </td>
                      <td className="px-2 py-2 font-mono text-xs">{it.batch_number}</td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{it.physical_quantity}</td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{it.system_quantity}</td>
                      <td className="px-2 py-2 text-center">
                        <span
                          className={
                            'inline-block rounded-full px-2.5 py-0.5 font-mono text-xs font-semibold tabular-nums ' +
                            (variance === 0
                              ? 'bg-safe-bg text-safe-text'
                              : variance < 0
                                ? 'bg-brick-bg text-brick-text'
                                : 'bg-udhaar-bg text-udhaar-text')
                          }
                        >
                          {variance === 0 ? 'Matched' : `${variance > 0 ? '+' : ''}${variance}`}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-xs text-inksoft">{it.reason}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              onClick={onReject}
              className="rounded-lg border border-line bg-white px-3 py-2 text-xs font-semibold text-inksoft transition-colors hover:bg-brick-bg hover:text-brick-text"
            >
              Reject
            </button>
            <button
              onClick={onApprove}
              disabled={busy}
              className="rounded-lg bg-pine-700 px-4 py-2 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
            >
              {busy ? 'Approving…' : 'Approve audit'}
            </button>
          </div>
        </div>
      )}
    </li>
  )
}

function Empty({ state, label }: { state: RequestStatus | 'ALL'; label: string }) {
  return (
    <div className="rounded-xl border border-dashed border-line bg-white px-4 py-10 text-center shadow-sm">
      <p className="text-sm font-medium text-inksoft">
        {state === 'PENDING' ? 'Nothing pending — the counter is clear.' : label}
      </p>
    </div>
  )
}