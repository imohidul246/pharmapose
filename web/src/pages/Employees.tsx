import { useCallback, useEffect, useState } from 'react'
import { api } from '../lib/api'
import type { Membership, Store } from '../types'

export default function Employees() {
  const [members, setMembers] = useState<Membership[]>([])
  const [store, setStore] = useState<Store | null>(null)
  const [loading, setLoading] = useState(true)
  const [inviteOpen, setInviteOpen] = useState(false)
  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const [m, s] = await Promise.all([api.employees(), api.store()])
    setMembers(m.members)
    setStore(s.store)
    setLoading(false)
  }, [])

  useEffect(() => {
    void reload().catch(() => setLoading(false))
  }, [reload])

  const staff = members.filter((m) => m.role === 'EMPLOYEE')
  const activeStaff = staff.filter((m) => m.user_active)
  const seats = store?.max_employees ?? 0

  const invite = async () => {
    if (busy || !name.trim() || !phone.trim() || password.length < 8) return
    setBusy(true)
    setError('')
    try {
      await api.inviteEmployee(name.trim(), phone.trim(), password)
      setName('')
      setPhone('')
      setPassword('')
      setInviteOpen(false)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const deactivate = async (m: Membership) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api.deactivateEmployee(m.user_id)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (loading) {
    return <p className="py-16 text-center text-sm text-inksoft">Loading staff roster…</p>
  }

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="font-display text-lg font-bold tracking-tight">Counter staff</h2>
          <p className="text-xs text-inksoft">
            Logins are handed out by the owner. Staff bill sales; stock changes go through for approval.
          </p>
        </div>
        <button
          onClick={() => setInviteOpen((o) => !o)}
          className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-bold text-white transition-colors hover:bg-pine-600"
        >
          {inviteOpen ? 'Close' : '+ Invite a staff seat'}
        </button>
      </header>

      {error && (
        <p role="alert" className="rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
          {error}
        </p>
      )}

      {store && (
        <section className="rounded-xl border border-line bg-white p-4 shadow-sm">
          <div className="flex items-baseline justify-between">
            <h3 className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Staff seats — {store.name}
            </h3>
            <span className="font-mono text-xs font-semibold tabular-nums text-inksoft">
              {activeStaff.length} of {seats} filled
            </span>
          </div>
          <div
            className="blister-track mt-3 flex flex-wrap gap-1 rounded-lg border border-line/70 bg-porcelain p-2"
            role="img"
            aria-label={`${activeStaff.length} of ${seats} staff seats filled`}
          >
            {Array.from({ length: Math.max(seats, 1) }, (_, i) => (
              <span
                key={i}
                className={
                  'flex h-8 w-8 items-center justify-center rounded-md font-mono text-[11px] ' +
                  (i < activeStaff.length
                    ? 'bg-pine-700 font-bold text-mint-100 shadow-sm'
                    : 'bg-white text-inksoft/40 ring-1 ring-line')
                }
              >
                {i < activeStaff.length ? staff[i].user_phone?.slice(-2) : '·'}
              </span>
            ))}
          </div>
          {activeStaff.length >= seats && seats > 0 && (
            <p className="mt-2 rounded-lg bg-marigold-bg px-3 py-2 text-xs font-semibold text-marigold-text">
              All seats are filled. Raise the seat limit in Settings to invite more staff.
            </p>
          )}
        </section>
      )}

      {inviteOpen && (
        <section className="rounded-xl border border-pine-600/50 bg-mint-50 p-4 shadow-sm">
          <h3 className="font-display text-sm font-bold tracking-tight">Invite a staff member</h3>
          <p className="mt-0.5 text-xs text-inksoft">
            You set the starting password and hand it over yourself — this screen replaces a printed slip.
          </p>
          <div className="mt-3 grid gap-2 sm:grid-cols-3">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Full name"
              className="rounded-lg border border-line bg-white px-3 py-2 text-sm focus:border-pine-600"
            />
            <input
              inputMode="numeric"
              value={phone}
              onChange={(e) => /^\d{0,12}$/.test(e.target.value) && setPhone(e.target.value)}
              placeholder="Phone (login)"
              className="rounded-lg border border-line bg-white px-3 py-2 font-mono text-sm focus:border-pine-600"
            />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Starting password (8+ chars)"
              className="rounded-lg border border-line bg-white px-3 py-2 text-sm focus:border-pine-600"
            />
          </div>
          <div className="mt-3 flex items-center justify-end gap-2">
            {activeStaff.length >= seats && seats > 0 && (
              <span className="mr-auto text-xs font-semibold text-marigold-text">
                Seat limit reached — no more invites until raised.
              </span>
            )}
            <button
              onClick={() => void invite()}
              disabled={busy || !name.trim() || !phone.trim() || password.length < 8 || (seats > 0 && activeStaff.length >= seats)}
              className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
            >
              {busy ? 'Inviting…' : 'Create login'}
            </button>
          </div>
        </section>
      )}

      <section className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
        <ul className="divide-y divide-line-soft">
          {members.map((m) => {
            const active = m.user_active ?? m.is_active
            return (
              <li key={m.user_id} className="flex items-center gap-3 px-4 py-3">
                <span
                  className={
                    'flex h-9 w-9 shrink-0 items-center justify-center rounded-lg font-display font-black ' +
                    (m.role === 'STORE_OWNER' ? 'bg-marigold-bg text-marigold-text' : 'bg-mint-100 text-pine-700')
                  }
                >
                  {m.user_name?.charAt(0).toUpperCase() ?? '·'}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-semibold">{m.user_name}</p>
                  <p className="truncate font-mono text-[11px] text-inksoft">{m.user_phone}</p>
                </div>
                <span
                  className={
                    'rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ' +
                    (m.role === 'STORE_OWNER'
                      ? 'bg-marigold-bg text-marigold-text'
                      : active
                        ? 'bg-safe-bg text-safe-text'
                        : 'bg-brick-bg text-brick-text')
                  }
                >
                  {m.role === 'STORE_OWNER' ? 'Owner' : active ? 'Active' : 'Deactivated'}
                </span>
                {m.role === 'EMPLOYEE' && active && (
                  <button
                    onClick={() => void deactivate(m)}
                    className="rounded-md border border-line px-2 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-brick-bg hover:text-brick-text"
                  >
                    Deactivate
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}