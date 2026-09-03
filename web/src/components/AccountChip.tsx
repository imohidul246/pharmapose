import { useState } from 'react'
import Modal from './Modal'
import { api } from '../lib/api'
import { useAuth, isOwner } from '../lib/auth'

export default function AccountChip() {
  const { session, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const p = session?.principal
  if (!p) return null

  const badge = isOwner(p)
    ? 'bg-marigold-bg text-marigold-text'
    : 'bg-safe-bg text-safe-text'

  return (
    <>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex items-center gap-2 rounded-lg border border-white/15 bg-white/[0.07] py-1 pl-1 pr-2 transition-colors hover:bg-white/15"
      >
        <span className="flex h-6 w-6 items-center justify-center rounded-md bg-pine-600 font-display text-[11px] font-black text-white">
          {p.name.trim().charAt(0).toUpperCase()}
        </span>
        <span className="hidden text-left text-xs sm:block">
          <span className="block max-w-28 truncate font-semibold leading-tight text-white">
            {p.name}
          </span>
          <span className={`block w-fit rounded px-1 text-[9px] font-bold uppercase tracking-wider leading-4 ${badge}`}>
            {isOwner(p) ? 'Owner' : 'Staff'}
          </span>
        </span>
        <svg
          viewBox="0 0 12 12"
          className={'h-3 w-3 text-white/50 transition-transform ' + (open ? 'rotate-180' : '')}
          stroke="currentColor"
          strokeWidth="1.6"
          fill="none"
        >
          <path d="M3 4.5l3 3 3-3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setOpen(false)} aria-hidden />
          <div className="absolute right-0 top-full z-30 mt-2 w-56 rounded-xl border border-line bg-white p-1.5 text-sm shadow-2xl shadow-pine-950/30">
            <p className="px-2.5 py-1.5 text-[10px] font-bold uppercase tracking-wider text-inksoft">
              {p.name} · {p.store_id.slice(0, 8)}
            </p>
            <button
              onClick={() => {
                setShowPassword(true)
                setOpen(false)
              }}
              className="w-full rounded-lg px-2.5 py-2 text-left font-medium text-ink transition-colors hover:bg-mint-50"
            >
              Change password
            </button>
            <button
              onClick={() => void logout()}
              className="w-full rounded-lg px-2.5 py-2 text-left font-medium text-brick-text transition-colors hover:bg-brick-bg"
            >
              Sign out
            </button>
          </div>
        </>
      )}

      {showPassword && (
        <PasswordModal onClose={() => setShowPassword(false)} />
      )}
    </>
  )
}

function PasswordModal({ onClose }: { onClose: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  const save = async () => {
    if (busy || next.length < 8) return
    setBusy(true)
    setError('')
    try {
      await api.changePassword(current, next)
      setDone(true)
      setCurrent('')
      setNext('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={done ? 'Password changed' : 'Change password'} onClose={onClose}>
      {done ? (
        <p className="text-sm text-inksoft">
          Your password is updated — every other session was signed out. The counter stays open.
        </p>
      ) : (
        <div className="space-y-3">
          <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
            Current password
            <input
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            />
          </label>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
            New password
            <input
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              placeholder="8 characters or more"
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            />
          </label>
          {error && (
            <p role="alert" className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-1">
            <button
              onClick={onClose}
              className="rounded-lg border border-line px-3 py-2 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
            >
              Cancel
            </button>
            <button
              onClick={() => void save()}
              disabled={busy || !current || next.length < 8}
              className="rounded-lg bg-pine-700 px-4 py-2 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
            >
              {busy ? 'Changing…' : 'Change password'}
            </button>
          </div>
        </div>
      )}
    </Modal>
  )
}