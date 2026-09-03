import { useCallback, useEffect, useState } from 'react'
import Modal from '../components/Modal'
import { api } from '../lib/api'
import type {
  PlatformStoreInfo,
  SubscriptionPayment,
  SubscriptionPlanType,
  SubscriptionStatus,
} from '../types'
import { SUBSCRIPTION_PLANS } from '../types'

function daysColor(days: number | null, status: string): string {
  if (status === 'SUSPENDED') return 'bg-brick-bg text-brick-text'
  if (days === null) return 'bg-white text-inksoft ring-1 ring-line'
  if (days < 0) return 'bg-brick-bg text-brick-text'
  if (days <= 7) return 'bg-marigold-bg text-marigold-text'
  return 'bg-safe-bg text-safe-text'
}

function formatDate(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleDateString('en-IN', { day: '2-digit', month: 'short', year: 'numeric' })
}

function daysLabel(days: number | null, status: string): string {
  if (status === 'SUSPENDED') return 'Suspended'
  if (days === null) return 'Grace'
  if (days < 0) return `Expired ${Math.abs(days)}d ago`
  if (days === 0) return 'Expires today'
  return `${days}d left`
}

export default function PlatformAdmin() {
  const [stores, setStores] = useState<PlatformStoreInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [renewTarget, setRenewTarget] = useState<PlatformStoreInfo | null>(null)
  const [historyTarget, setHistoryTarget] = useState<PlatformStoreInfo | null>(null)
  const [busyId, setBusyId] = useState('')

  const reload = useCallback(async () => {
    setError('')
    try {
      const res = await api.platformStores()
      setStores(res.stores)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const toggleStatus = async (s: PlatformStoreInfo) => {
    const next: SubscriptionStatus = s.subscription_status === 'ACTIVE' ? 'SUSPENDED' : 'ACTIVE'
    setBusyId(s.store_id)
    setError('')
    try {
      await api.platformSetStatus(s.store_id, next)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyId('')
    }
  }

  if (loading) {
    return <p className="py-16 text-center text-sm text-inksoft">Loading all stores…</p>
  }

  return (
    <div className="mx-auto max-w-6xl space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="font-display text-lg font-bold tracking-tight">Platform administration</h2>
          <p className="text-xs text-inksoft">
            Every tenant store, its cash-subscription window, and the offline payment ledger. Renewals take
            effect instantly.
          </p>
        </div>
        <button
          onClick={() => void reload()}
          className="rounded-lg border border-line bg-white px-4 py-2 text-sm font-semibold text-ink transition-colors hover:bg-mint-50"
        >
          Refresh
        </button>
      </header>

      {error && (
        <p role="alert" className="rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
          {error}
        </p>
      )}

      <section className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
        <table className="w-full min-w-[880px] text-left text-sm">
          <thead>
            <tr className="border-b border-line bg-cream/60 text-[10px] font-bold uppercase tracking-wider text-inksoft">
              <th className="px-4 py-3">Store</th>
              <th className="px-4 py-3">Owner</th>
              <th className="px-4 py-3">Expires</th>
              <th className="px-4 py-3">Remaining</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line-soft">
            {stores.map((s) => (
              <tr key={s.store_id} className="transition-colors hover:bg-mint-50/40">
                <td className="px-4 py-3">
                  <p className="font-semibold">{s.store_name}</p>
                  <p className="font-mono text-[11px] text-inksoft">{s.store_phone || '—'}</p>
                  <p className="max-w-56 truncate text-[11px] text-inksoft" title={s.store_address}>
                    {s.store_address || '—'}
                  </p>
                </td>
                <td className="px-4 py-3">
                  <p className="font-medium">{s.owner_name || '—'}</p>
                  <p className="font-mono text-[11px] text-inksoft">{s.owner_phone || '—'}</p>
                </td>
                <td className="whitespace-nowrap px-4 py-3 font-mono text-xs">
                  {formatDate(s.subscription_valid_until)}
                </td>
                <td className="px-4 py-3">
                  <span
                    className={
                      'inline-block rounded-full px-2.5 py-1 text-[11px] font-bold tabular-nums ' +
                      daysColor(s.days_remaining, s.subscription_status)
                    }
                  >
                    {daysLabel(s.days_remaining, s.subscription_status)}
                  </span>
                </td>
                <td className="px-4 py-3">
                  <button
                    role="switch"
                    aria-checked={s.subscription_status === 'ACTIVE'}
                    aria-label={`Toggle ${s.store_name} status`}
                    title={s.subscription_status === 'ACTIVE' ? 'Active — click to suspend' : 'Suspended — click to activate'}
                    onClick={() => void toggleStatus(s)}
                    disabled={busyId === s.store_id}
                    className={
                      'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors disabled:opacity-50 ' +
                      (s.subscription_status === 'ACTIVE' ? 'bg-safe-text/80' : 'bg-brick-text/80')
                    }
                  >
                    <span
                      className={
                        'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ' +
                        (s.subscription_status === 'ACTIVE' ? 'translate-x-6' : 'translate-x-1')
                      }
                    />
                  </button>
                  <span className="ml-2 text-xs font-bold uppercase tracking-wider text-inksoft">
                    {s.subscription_status}
                  </span>
                </td>
                <td className="whitespace-nowrap px-4 py-3 text-right">
                  <button
                    onClick={() => setHistoryTarget(s)}
                    className="mr-2 rounded-lg border border-line px-3 py-1.5 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50 hover:text-ink"
                  >
                    Ledger
                  </button>
                  <button
                    onClick={() => setRenewTarget(s)}
                    className="rounded-lg bg-pine-700 px-3 py-1.5 text-xs font-bold text-white transition-colors hover:bg-pine-600"
                  >
                    Record cash payment
                  </button>
                </td>
              </tr>
            ))}
            {stores.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-sm text-inksoft">
                  No stores registered yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      {renewTarget && (
        <RenewModal
          store={renewTarget}
          onClose={() => setRenewTarget(null)}
          onDone={() => {
            setRenewTarget(null)
            void reload()
          }}
        />
      )}

      {historyTarget && (
        <LedgerModal store={historyTarget} onClose={() => setHistoryTarget(null)} />
      )}
    </div>
  )
}

function RenewModal({
  store,
  onClose,
  onDone,
}: {
  store: PlatformStoreInfo
  onClose: () => void
  onDone: () => void
}) {
  const [plan, setPlan] = useState<SubscriptionPlanType>('1_MONTH')
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const selected = SUBSCRIPTION_PLANS.find((p) => p.plan_type === plan) ?? SUBSCRIPTION_PLANS[0]

  const confirm = async () => {
    setBusy(true)
    setError('')
    try {
      await api.platformRenew(store.store_id, {
        plan_type: selected.plan_type,
        amount: selected.amount,
        notes: notes.trim(),
      })
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={`Record cash payment — ${store.store_name}`} onClose={onClose}>
      <p className="text-xs leading-relaxed text-inksoft">
        Cash collected offline. Validity extends from the current expiry when the store is still active,
        otherwise from right now. The store re-activates instantly.
      </p>
      <div className="grid grid-cols-3 gap-2">
        {SUBSCRIPTION_PLANS.map((p) => (
          <button
            key={p.plan_type}
            onClick={() => setPlan(p.plan_type)}
            aria-pressed={plan === p.plan_type}
            className={
              'rounded-xl border px-3 py-2.5 text-left transition-colors ' +
              (plan === p.plan_type
                ? 'border-pine-700 bg-mint-50 ring-1 ring-pine-700'
                : 'border-line bg-white hover:bg-mint-50')
            }
          >
            <span className="block text-sm font-bold">{p.label}</span>
            <span className="block font-mono text-xs font-semibold text-pine-700">₹{p.amount.toLocaleString('en-IN')}</span>
            <span className="block text-[10px] text-inksoft">+{p.days} days</span>
          </button>
        ))}
      </div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
        Cash receipt reference / notes
        <input
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder="e.g. Receipt #412, collected 12 Sep"
          className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm font-normal normal-case tracking-normal focus:border-pine-600"
        />
      </label>
      {error && (
        <p role="alert" className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">
          {error}
        </p>
      )}
      <div className="flex items-center justify-between gap-2 pt-1">
        <p className="font-mono text-xs text-inksoft">
          {selected.label} · ₹{selected.amount.toLocaleString('en-IN')}
        </p>
        <div className="flex gap-2">
          <button
            onClick={onClose}
            className="rounded-lg border border-line px-3 py-2 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
          >
            Cancel
          </button>
          <button
            onClick={() => void confirm()}
            disabled={busy}
            className="rounded-lg bg-pine-700 px-4 py-2 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
          >
            {busy ? 'Recording…' : `Confirm ₹${selected.amount.toLocaleString('en-IN')}`}
          </button>
        </div>
      </div>
    </Modal>
  )
}

function LedgerModal({ store, onClose }: { store: PlatformStoreInfo; onClose: () => void }) {
  const [payments, setPayments] = useState<SubscriptionPayment[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api
      .platformPayments(store.store_id)
      .then((res) => {
        if (!cancelled) setPayments(res.payments)
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [store.store_id])

  return (
    <Modal title={`Payment ledger — ${store.store_name}`} onClose={onClose} wide>
      {loading ? (
        <p className="py-6 text-center text-sm text-inksoft">Loading ledger…</p>
      ) : error ? (
        <p role="alert" className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">
          {error}
        </p>
      ) : payments.length === 0 ? (
        <p className="py-6 text-center text-sm text-inksoft">No cash payments recorded yet.</p>
      ) : (
        <ul className="max-h-96 divide-y divide-line-soft overflow-y-auto">
          {payments.map((p) => (
            <li key={p.id} className="flex items-center gap-3 py-2.5">
              <span className="rounded-md bg-mint-100 px-2 py-1 font-mono text-[11px] font-bold text-pine-700">
                {p.plan_type}
              </span>
              <div className="min-w-0 flex-1">
                <p className="font-mono text-sm font-bold">₹{Number(p.amount).toLocaleString('en-IN')}</p>
                <p className="truncate text-[11px] text-inksoft">
                  {formatDate(p.valid_from)} → {formatDate(p.valid_until)}
                  {p.notes ? ` · ${p.notes}` : ''}
                </p>
              </div>
              <span className="font-mono text-[11px] text-inksoft">{formatDate(p.created_at)}</span>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}
