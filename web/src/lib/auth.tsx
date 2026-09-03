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

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthSession | null>(null)
  const [loading, setLoading] = useState(true)

  const reset = useCallback(() => setSession(null), [])

  useEffect(() => {
    let cancelled = false
    api.me()
      .then((s) => {
        if (!cancelled) setSession(s)
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
    setSession(full)
    return principal
  }, [])

  const register = useCallback(async (input: Parameters<typeof api.register>[0]) => {
    const full = await api.register(input)
    setSession(full)
    return full
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
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