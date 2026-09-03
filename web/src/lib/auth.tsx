import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { api, UnauthorizedError } from './api'
import type { AuthSession, Principal } from '../types'

interface AuthState {
  session: AuthSession | null
  loading: boolean
  login: (phone: string, password: string) => Promise<Principal>
  register: (input: Parameters<typeof api.register>[0]) => Promise<AuthSession>
  logout: () => Promise<void>
  reset: () => void
}

const AuthContext = createContext<AuthState | null>(null)

const ACTIVE_STORE_KEY = 'activeStoreId'

// authorizedStoreIds returns every store id the principal may act in. The
// current API issues single-store principals (principal.store_id); the
// user.stores list is read defensively so this stays correct if/when the
// backend ships multi-store memberships.
function authorizedStoreIds(session: AuthSession): string[] {
  const ids: string[] = []
  if (session.principal?.store_id) ids.push(session.principal.store_id)
  const stores = (session.user as unknown as { stores?: Array<{ id: string }> })?.stores
  if (Array.isArray(stores)) {
    for (const s of stores) {
      if (s?.id && !ids.includes(s.id)) ids.push(s.id)
    }
  }
  return ids
}

// syncActiveStore validates the terminal's cached activeStoreId against the
// freshly hydrated session. A stale id (different user, revoked membership,
// fresh login) is reset to the first authorized store so the terminal can
// never desynchronize into another tenant's scope.
export function syncActiveStore(session: AuthSession): string | null {
  try {
    const ids = authorizedStoreIds(session)
    if (ids.length === 0) return null
    const cached = localStorage.getItem(ACTIVE_STORE_KEY)
    if (cached && ids.includes(cached)) return cached
    localStorage.setItem(ACTIVE_STORE_KEY, ids[0])
    return ids[0]
  } catch {
    return session.principal?.store_id ?? null
  }
}

export function clearActiveStore(): void {
  try {
    localStorage.removeItem(ACTIVE_STORE_KEY)
  } catch {
    // Storage unavailable (private mode): nothing to clear.
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthSession | null>(null)
  const [loading, setLoading] = useState(true)

  const reset = useCallback(() => setSession(null), [])

  useEffect(() => {
    let cancelled = false
    api.me()
      .then((s) => {
        if (!cancelled) {
          syncActiveStore(s)
          setSession(s)
        }
      })
      .catch((err) => {
        if (!cancelled && !(err instanceof UnauthorizedError)) {
          setSession(null)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const onUnauthorized = () => setSession(null)
    window.addEventListener('pms:unauthorized', onUnauthorized)
    return () => window.removeEventListener('pms:unauthorized', onUnauthorized)
  }, [])

  const login = useCallback(async (phone: string, password: string) => {
    const { principal } = await api.login(phone, password)
    const full = await api.me()
    syncActiveStore(full)
    setSession(full)
    return principal
  }, [])

  const register = useCallback(async (input: Parameters<typeof api.register>[0]) => {
    const full = await api.register(input)
    syncActiveStore(full)
    setSession(full)
    return full
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      clearActiveStore()
      setSession(null)
    }
  }, [])

  return (
    <AuthContext.Provider value={{ session, loading, login, register, logout, reset }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

export function isOwner(p: Principal): boolean {
  return p.role === 'STORE_OWNER'
}