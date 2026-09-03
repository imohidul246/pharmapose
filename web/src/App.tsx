import { useCallback, useEffect, useRef, useState } from 'react'
import { AuthProvider, useAuth } from './lib/auth'
import { syncLocalCache, type SyncResult } from './lib/db'
import POS from './pages/POS'
import Purchases from './pages/Purchases'
import Reconcile from './pages/Reconcile'
import Reports from './pages/Reports'
import Customers from './pages/Customers'
import Invoices from './pages/Invoices'
import Medicines from './pages/Medicines'
import Suppliers from './pages/Suppliers'
import GSTReportsPage from './pages/GSTReportsPage'
import Approvals from './pages/Approvals'
import Employees from './pages/Employees'
import StoreSettings from './pages/StoreSettings'
import Login from './pages/Login'
import AccountChip from './components/AccountChip'

type Tab =
  | 'pos'
  | 'purchases'
  | 'medicines'
  | 'invoices'
  | 'reconcile'
  | 'reports'
  | 'gst'
  | 'customers'
  | 'suppliers'
  | 'approvals'
  | 'employees'
  | 'settings'

const COMMON_TABS: { id: Tab; label: string }[] = [
  { id: 'pos', label: 'Billing' },
  { id: 'purchases', label: 'Purchases' },
  { id: 'medicines', label: 'Medicines' },
  { id: 'invoices', label: 'Invoices' },
  { id: 'reconcile', label: 'Stock Audit' },
  { id: 'reports', label: 'Reports' },
  { id: 'gst', label: 'GST Returns' },
  { id: 'customers', label: 'Khata' },
  { id: 'suppliers', label: 'Suppliers' },
]

const OWNER_TABS: { id: Tab; label: string }[] = [
  ...COMMON_TABS,
  { id: 'approvals', label: 'Approvals' },
  { id: 'employees', label: 'Employees' },
  { id: 'settings', label: 'Settings' },
]

export interface SyncState {
  status: 'idle' | 'syncing' | 'ok' | 'error'
  result?: SyncResult
  error?: string
}

export default function App() {
  return (
    <AuthProvider>
      <Gate />
    </AuthProvider>
  )
}

function Gate() {
  const { session, loading } = useAuth()

  if (loading) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-pine-950">
        <span className="flex h-14 w-14 animate-pulse items-center justify-center rounded-2xl bg-white/[0.06] font-display text-3xl font-black text-mint-100">
          ℞
        </span>
        <p className="text-xs font-semibold uppercase tracking-[0.22em] text-mint-300/60">
          Opening the counter…
        </p>
      </div>
    )
  }

  if (!session) return <Login />

  return <Workspace />
}

function Workspace() {
  const { session: maybeSession } = useAuth()
  const session = maybeSession!
  const [tab, setTab] = useState<Tab>('pos')
  const [sync, setSync] = useState<SyncState>({ status: 'idle' })
  const firstSync = useRef(false)

  const isOwner = session.principal.role === 'STORE_OWNER'
  const TABS = isOwner ? OWNER_TABS : COMMON_TABS
  const pendingTab = TABS.some((t) => t.id === tab) ? tab : TABS[0].id

  const doSync = useCallback(async () => {
    setSync((s) => ({ ...s, status: 'syncing' }))
    try {
      const result = await syncLocalCache()
      setSync({ status: 'ok', result })
    } catch (err) {
      if (session?.principal) {
        setSync({ status: 'error', error: err instanceof Error ? err.message : String(err) })
      }
    }
  }, [session])

  useEffect(() => {
    if (!session || firstSync.current) return
    firstSync.current = true
    void doSync()
  }, [session, doSync])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
      const idx = Number(e.key) - 1
      if (Number.isInteger(idx) && idx >= 0 && idx < TABS.length) {
        e.preventDefault()
        setTab(TABS[idx].id)
        return
      }
      if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
        e.preventDefault()
        setTab((cur) => {
          const i = TABS.findIndex((t) => t.id === cur)
          const next = e.key === 'ArrowRight' ? i + 1 : i - 1 + TABS.length
          return TABS[next % TABS.length].id
        })
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [TABS])

  const lampClass =
    sync.status === 'ok'
      ? 'bg-lamp-ok'
      : sync.status === 'syncing'
        ? 'animate-pulse bg-lamp-warn'
        : sync.status === 'error'
          ? 'bg-lamp-bad'
          : 'bg-white/25'

  return (
    <div className="min-h-screen">
      <header className="no-print sticky top-0 z-20 bg-pine-950 text-white shadow-lg shadow-pine-950/20">
        <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-x-5 gap-y-2 px-4 py-2.5 lg:px-6">
          {/* Brand */}
          <div className="flex items-center gap-2.5">
            <span
              aria-hidden
              className="flex h-8 w-8 items-center justify-center rounded-lg bg-pine-700 font-display text-lg font-black text-mint-100"
            >
              ℞
            </span>
            <div className="leading-none">
              <p className="font-display text-[17px] font-black uppercase tracking-tight">
                PharmaPOS
              </p>
              <p className="mt-0.5 text-[10px] font-semibold uppercase tracking-[0.22em] text-mint-300/70">
                Medical Store Billing
              </p>
            </div>
          </div>

          {/* Blister-strip navigation */}
          <nav
            aria-label="Sections"
            title={'Switch screens: Alt+1…' + TABS.length + ' · Alt+←/→ cycles'}
            className="order-3 -mx-1 w-full overflow-x-auto px-1 no-scrollbar md:order-none md:mx-0 md:w-auto md:flex-1 md:overflow-visible"
          >
            <div className="blister-track inline-flex items-center gap-1 rounded-xl bg-white/[0.06] p-[7px] ring-1 ring-white/10">
              {TABS.map((t, i) => (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  aria-current={pendingTab === t.id ? 'page' : undefined}
                  title={`${t.label} — Alt+${i + 1}`}
                  className={
                    'whitespace-nowrap rounded-lg px-3.5 py-1.5 text-[13px] font-semibold transition-colors duration-100 ' +
                    (pendingTab === t.id
                      ? 'bg-white text-pine-900 shadow-md shadow-black/30'
                      : 'text-mint-100/75 hover:bg-white/10 hover:text-white')
                  }
                >
                  {t.label}
                </button>
              ))}
            </div>
          </nav>

          {/* Sync lamp + account */}
          <div className="ml-auto flex items-center gap-2.5 text-xs">
            <span aria-live="polite" className="hidden items-center gap-2 sm:flex">
              <span className={`h-2 w-2 rounded-full ${lampClass}`} />
              {sync.status === 'idle' && <span className="text-white/45">Cache not synced</span>}
              {sync.status === 'syncing' && (
                <span className="text-lamp-warn">Syncing local cache…</span>
              )}
              {sync.status === 'ok' && (
                <span className="font-medium text-white/70">
                  Synced{' '}
                  <span className="font-mono text-[11px] text-lamp-ok">
                    {sync.result?.syncedAt.toLocaleTimeString([], {
                      hour: '2-digit',
                      minute: '2-digit',
                    })}
                  </span>{' '}
                  · {sync.result?.medicineCount} meds
                </span>
              )}
              {sync.status === 'error' && (
                <span className="max-w-56 truncate font-semibold text-lamp-bad" title={sync.error}>
                  Sync failed: {sync.error}
                </span>
              )}
            </span>
            <button
              onClick={() => void doSync()}
              disabled={sync.status === 'syncing'}
              className="rounded-lg border border-white/20 bg-white/5 px-3 py-1.5 text-xs font-semibold text-mint-100 transition-colors hover:bg-white/15 disabled:opacity-50"
            >
              Sync now
            </button>
            <div className="relative">
              <AccountChip />
            </div>
          </div>
        </div>
      </header>

      <main key={pendingTab} className="page-enter mx-auto max-w-[1400px] px-4 py-6 lg:px-6">
        {pendingTab === 'pos' && <POS cacheVersion={sync.result?.syncedAt.getTime() ?? 0} />}
        {pendingTab === 'purchases' && (
          <Purchases
            cacheVersion={sync.result?.syncedAt.getTime() ?? 0}
            onMutated={doSync}
            mode={isOwner ? 'record' : 'submit'}
          />
        )}
        {pendingTab === 'invoices' && <Invoices />}
        {pendingTab === 'medicines' && <Medicines />}
        {pendingTab === 'reconcile' && (
          <Reconcile
            cacheVersion={sync.result?.syncedAt.getTime() ?? 0}
            onMutated={doSync}
            mode={isOwner ? 'record' : 'submit'}
          />
        )}
        {pendingTab === 'reports' && <Reports />}
        {pendingTab === 'gst' && <GSTReportsPage />}
        {pendingTab === 'customers' && <Customers onMutated={doSync} />}
        {pendingTab === 'suppliers' && <Suppliers onMutated={doSync} />}
        {pendingTab === 'approvals' && isOwner && <Approvals />}
        {pendingTab === 'employees' && isOwner && <Employees />}
        {pendingTab === 'settings' && isOwner && <StoreSettings />}
      </main>
    </div>
  )
}