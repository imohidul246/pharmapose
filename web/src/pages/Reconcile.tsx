import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { loadCachedMedicines } from '../lib/db'
import { daysUntil, expiryClass } from '../lib/format'
import type { ReconcileResultItem, StockAuditRequest } from '../types'

interface Row {
  batchId: string
  medicineId: string
  medicineName: string
  salt: string
  batchNumber: string
  expiryDate: string
  systemStock: number
  physicalInput: string
  reason: string
}

export default function Reconcile({
  cacheVersion,
  onMutated,
  mode = 'record',
}: {
  cacheVersion: number
  onMutated: () => Promise<void>
  mode?: 'record' | 'submit'
}) {
  const { session } = useAuth()
  const isSubmit = mode === 'submit'
  const selfId = session?.principal?.id
  const [rows, setRows] = useState<Row[]>([])
  const [notes, setNotes] = useState('')
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<{ id: string; items: ReconcileResultItem[] } | null>(null)
  const [myAudits, setMyAudits] = useState<StockAuditRequest[]>([])

  const loadMyAudits = useCallback(async () => {
    if (!isSubmit || !selfId) return
    try {
      const all = await api.stockAuditRequests()
      setMyAudits(all.requests.filter((r) => r.requested_by === selfId))
    } catch {
      /* non-fatal */
    }
  }, [isSubmit, selfId])

  useEffect(() => {
    void loadMyAudits()
  }, [loadMyAudits])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      const medicines = await loadCachedMedicines()
      if (cancelled) return
      setRows(
        medicines.flatMap((m) =>
          m.batches.map((b) => ({
            batchId: b.id,
            medicineId: m.id,
            medicineName: m.name,
            salt: m.salt_composition,
            batchNumber: b.batch_number,
            expiryDate: b.expiry_date,
            systemStock: b.current_stock,
            physicalInput: '',
            reason: '',
          })),
        ),
      )
    })()
    return () => {
      cancelled = true
    }
  }, [cacheVersion])

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter(
      (r) =>
        r.medicineName.toLowerCase().includes(q) ||
        r.salt.toLowerCase().includes(q) ||
        r.batchNumber.toLowerCase().includes(q),
    )
  }, [rows, filter])

  const adjustedCount = rows.filter((r) => r.physicalInput !== '').length

  const setInput = (batchId: string, value: string) => {
    if (value !== '' && (!/^\d*$/.test(value) || Number(value) < 0)) return
    setRows((prev) =>
      prev.map((r) => (r.batchId === batchId ? { ...r, physicalInput: value } : r)),
    )
  }
  const setReason = (batchId: string, value: string) =>
    setRows((prev) =>
      prev.map((r) => (r.batchId === batchId ? { ...r, reason: value } : r)),
    )

  const submit = async () => {
    const pending = rows.filter((r) => r.physicalInput !== '')
    const missingReason = pending.find((r) => r.reason.trim() === '')
    if (missingReason) {
      setError(`A reason is required for ${missingReason.medicineName} (${missingReason.batchNumber}).`)
      return
    }
    if (pending.length === 0 || busy) return

    setBusy(true)
    setError('')
    try {
      if (isSubmit) {
        const items = pending.map((r) => ({
          medicine_id: r.medicineId,
          batch_id: r.batchId,
          physical_quantity: Number(r.physicalInput),
          reason: r.reason.trim(),
        }))
        await api.createStockAuditRequest(notes, items)
        setResult(null)
        setRows((prev) => prev.map((r) => ({ ...r, physicalInput: '', reason: '' })))
        setNotes('')
        await loadMyAudits()
      } else {
        const items = pending.map((r) => ({
          batch_id: r.batchId,
          physical_count: Number(r.physicalInput),
          reason: r.reason.trim(),
        }))
        const res = await api.reconcile(items, notes)
        setResult({
          id: res.journal.id,
          items: res.items as unknown as ReconcileResultItem[],
        })
        setRows((prev) => prev.map((r) => ({ ...r, physicalInput: '', reason: '' })))
        setNotes('')
        await onMutated()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const cancelAudit = async (id: string) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api.cancelStockAuditRequest(id)
      await loadMyAudits()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="no-print flex flex-wrap items-center gap-x-4 gap-y-3">
        <div>
          <h2 className="font-display text-lg font-bold tracking-tight">
            Physical Stock Reconciliation
          </h2>
          <p className="text-xs text-inksoft">
            {isSubmit
              ? 'Enter what you actually counted on paper — your owner signs it off before any stock moves.'
              : 'Enter what you actually counted; the ledger records every correction with a reason.'}
          </p>
        </div>
        <span
          className={
            'rounded-full px-2.5 py-1 text-xs font-semibold tabular-nums ' +
            (adjustedCount > 0 ? 'bg-marigold-bg text-marigold-text' : 'bg-line/60 text-inksoft')
          }
        >
          {adjustedCount} row{adjustedCount === 1 ? '' : 's'} staged
        </span>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by name / salt / batch…"
          className="ml-auto w-full max-w-64 rounded-lg border border-line bg-white px-3 py-1.5 text-sm shadow-sm focus:border-pine-600 sm:w-64"
        />
        <button
          onClick={() => void submit()}
          disabled={busy || adjustedCount === 0}
          className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
        >
          {busy
            ? isSubmit
              ? 'Submitting audit…'
              : 'Posting audit…'
            : isSubmit
              ? `Submit audit for approval (${adjustedCount})`
              : `Submit Audit (${adjustedCount})`}
        </button>
      </div>

      {error && (
        <p className="rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
          {error}
        </p>
      )}

      {result && !isSubmit && (
        <div className="flex items-start gap-3 rounded-xl border border-dashed border-pine-600/60 bg-white p-4 text-sm shadow-sm">
          <span aria-hidden className="stamp shrink-0 px-2.5 py-1 text-[11px]">
            Posted
          </span>
          <div className="min-w-0 flex-1">
            <p className="font-semibold">Journal {result.id.slice(0, 8)}… posted.</p>
            <ul className="mt-1.5 space-y-1 text-inksoft">
              {result.items.map((it) => (
                <li key={it.id} className="font-mono text-xs">
                  {it.medicine_name} [{it.batch_number}]: {it.system_stock} →{' '}
                  {it.physical_stock} ({fmtVariance(it.variance_quantity)}), cost impact ₹
                  {it.cost_impact.toFixed(2)}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      {isSubmit && myAudits.length > 0 && (
        <section className="no-print">
          <h3 className="mb-2 font-display text-sm font-bold uppercase tracking-wide text-inksoft">
            Your submitted audits
          </h3>
          <ul className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
            {myAudits.map((r) => (
              <li
                key={r.id}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line-soft px-4 py-2.5 text-sm last:border-b-0"
              >
                <span className="min-w-0 flex-1">
                  <span className="font-semibold">
                    {r.status === 'PENDING' ? 'Stock audit' : `Audit ${r.status.toLowerCase()}`}
                  </span>
                  <span className="ml-2 text-xs text-inksoft">
                    {new Date(r.created_at).toLocaleString('en-IN', {
                      day: 'numeric',
                      month: 'short',
                      hour: '2-digit',
                      minute: '2-digit',
                    })}
                  </span>
                  {r.notes && <span className="ml-2 text-xs text-inksoft/70">— {r.notes}</span>}
                  {r.status === 'REJECTED' && r.rejection_reason && (
                    <span className="ml-2 rounded bg-brick-bg px-1.5 py-0.5 text-[11px] font-semibold text-brick-text">
                      {r.rejection_reason}
                    </span>
                  )}
                  {r.status === 'APPROVED' && r.journal_id && (
                    <span className="ml-2 rounded bg-safe-bg px-1.5 py-0.5 text-[11px] font-semibold text-safe-text">
                      posted
                    </span>
                  )}
                </span>
                <span
                  className={
                    'rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ' +
                    (r.status === 'PENDING'
                      ? 'bg-marigold-bg text-marigold-text'
                      : r.status === 'APPROVED'
                        ? 'bg-safe-bg text-safe-text'
                        : r.status === 'REJECTED'
                          ? 'bg-brick-bg text-brick-text'
                          : 'bg-line/70 text-inksoft')
                  }
                >
                  {r.status}
                </span>
                {r.status === 'PENDING' && (
                  <button
                    onClick={() => void cancelAudit(r.id)}
                    className="rounded-md border border-line px-2 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-brick-bg hover:text-brick-text"
                  >
                    Cancel
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <div className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
        <table className="w-full min-w-[820px] text-sm">
          <thead>
            <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
              <th className="px-4 py-2.5">Medicine</th>
              <th className="px-2 py-2.5">Batch</th>
              <th className="px-2 py-2.5">Expiry</th>
              <th className="px-2 py-2.5 text-right">System</th>
              <th className="px-2 py-2.5 text-center">Physical count</th>
              <th className="px-2 py-2.5 text-center">Drift</th>
              <th className="px-4 py-2.5">Reason</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line-soft">
            {visible.map((r) => {
              const entered = r.physicalInput !== ''
              const variance = entered ? Number(r.physicalInput) - r.systemStock : null
              return (
                <tr key={r.batchId} className="hover:bg-mint-50/50">
                  <td className="max-w-[240px] px-4 py-2">
                    <p className="truncate font-medium">{r.medicineName}</p>
                    <p className="truncate text-xs text-inksoft">{r.salt}</p>
                  </td>
                  <td className="px-2 py-2 font-mono text-xs">{r.batchNumber}</td>
                  <td className="px-2 py-2">
                    <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(daysUntil(r.expiryDate))}`}>
                      {r.expiryDate}
                    </span>
                  </td>
                  <td className="px-2 py-2 text-right font-mono tabular-nums">{r.systemStock}</td>
                  <td className="px-2 py-2 text-center">
                    <input
                      inputMode="numeric"
                      value={r.physicalInput}
                      onChange={(e) => setInput(r.batchId, e.target.value)}
                      placeholder="—"
                      aria-label={`Physical count for ${r.medicineName} batch ${r.batchNumber}`}
                      className="w-20 rounded-md border border-line px-2 py-1 text-right font-mono tabular-nums focus:border-pine-600"
                    />
                  </td>
                  <td className="px-2 py-2 text-center">
                    {variance === null ? (
                      <span className="text-xs text-line">·</span>
                    ) : (
                      <span
                        className={
                          'inline-block rounded-full px-2.5 py-0.5 font-mono text-xs font-semibold tabular-nums ' +
                          driftClass(variance)
                        }
                      >
                        {fmtVariance(variance)}
                        {variance === 0 ? '' : variance < 0 ? ' Shortage' : ' Surplus'}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2">
                    <input
                      value={r.reason}
                      onChange={(e) => setReason(r.batchId, e.target.value)}
                      disabled={!entered}
                      placeholder={entered ? 'why is stock off?' : ''}
                      aria-label={`Reason for ${r.medicineName} batch ${r.batchNumber}`}
                      className="w-full rounded-md border border-line-soft bg-porcelain px-2 py-1 text-xs focus:border-pine-600 focus:bg-white disabled:opacity-40"
                    />
                  </td>
                </tr>
              )
            })}
            {visible.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-10 text-center text-sm text-inksoft">
                  No batches in the local cache — run “Sync now” first.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <input
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        placeholder="Audit notes (optional) — e.g. “March month-end count”"
        className="no-print w-full rounded-lg border border-line bg-white px-3 py-2 text-sm shadow-sm focus:border-pine-600"
      />
    </div>
  )
}

function driftClass(variance: number): string {
  if (variance === 0) return 'bg-safe-bg text-safe-text'
  if (variance < 0) return 'bg-brick-bg text-brick-text'
  return 'bg-udhaar-bg text-udhaar-text'
}

function fmtVariance(variance: number): string {
  if (variance === 0) return 'Matched'
  return variance > 0 ? `+${variance}` : `${variance}`
}
