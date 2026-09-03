import { useEffect, useRef, useState } from 'react'
import { api } from '../lib/api'
import { money } from '../lib/format'
import type { Customer } from '../types'

export function customerMatches(c: Customer, q: string, matchGstin = false): boolean {
  const needle = q.trim().toLowerCase()
  if (!needle) return false
  return (
    c.name.toLowerCase().includes(needle) ||
    c.phone.toLowerCase().includes(needle) ||
    (matchGstin && !!c.gstin && c.gstin.toLowerCase().includes(needle))
  )
}

interface UseCustomerQueryOptions {
  customerType?: 'B2C' | 'B2B'
  fallbackPool?: Customer[]
  matchGstin?: boolean
  limit?: number
  delay?: number
}

export function useCustomerQuery({
  customerType,
  fallbackPool,
  matchGstin = false,
  limit = 10,
  delay = 300,
}: UseCustomerQueryOptions) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Customer[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [offline, setOffline] = useState(false)
  const timer = useRef<number | null>(null)
  const seq = useRef(0)

  const runQuery = (q: string) => {
    const token = ++seq.current
    if (!q.trim()) {
      setResults(null)
      setSearching(false)
      setOffline(false)
      return
    }
    setSearching(true)
    api
      .searchCustomers({ q: q.trim(), type: customerType, limit })
      .then((res) => {
        if (token !== seq.current) return
        setResults(res.customers)
        setOffline(false)
      })
      .catch(() => {
        if (token !== seq.current) return
        const pool = (fallbackPool ?? []).filter((c) => customerMatches(c, q, matchGstin)).slice(0, limit)
        setResults(pool)
        setOffline(true)
      })
      .finally(() => {
        if (token === seq.current) setSearching(false)
      })
  }

  useEffect(
    () => () => {
      seq.current += 1
      if (timer.current) clearTimeout(timer.current)
    },
    [],
  )

  const onChangeQuery = (val: string) => {
    setQuery(val)
    if (timer.current) clearTimeout(timer.current)
    timer.current = window.setTimeout(() => runQuery(val), delay)
  }

  const clearQuery = () => {
    seq.current += 1
    if (timer.current) clearTimeout(timer.current)
    setQuery('')
    setResults(null)
    setSearching(false)
    setOffline(false)
  }

  return { query, setQuery: onChangeQuery, clearQuery, results, searching, offline }
}

const baseInputClass = (accent: 'pine' | 'amber') =>
  accent === 'amber'
    ? 'w-full rounded-md border border-amber-300 bg-white pl-9 pr-3 py-1.5 text-sm outline-none focus:border-amber-500'
    : 'w-full rounded-md border border-line bg-white pl-9 pr-3 py-1.5 text-sm outline-none focus:border-pine-600'

function SearchIcon() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 20 20"
      className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-inksoft"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <circle cx="9" cy="9" r="6" />
      <path d="m14 14 4 4" strokeLinecap="round" />
    </svg>
  )
}

export interface CustomerSearchProps {
  value: Customer | null
  onChange: (c: Customer | null) => void
  customerType?: 'B2C' | 'B2B'
  fallbackPool?: Customer[]
  matchGstin?: boolean
  placeholder?: string
  accent?: 'pine' | 'amber'
  autoFocus?: boolean
  className?: string
}

export default function CustomerSearch({
  value,
  onChange,
  customerType,
  fallbackPool,
  matchGstin,
  placeholder = 'Search customer by name or phone…',
  accent = 'pine',
  autoFocus,
  className = '',
}: CustomerSearchProps) {
  const { query, setQuery, clearQuery, results, searching, offline } = useCustomerQuery({
    customerType,
    fallbackPool,
    matchGstin,
  })

  if (value) {
    return (
      <div
        className={
          'flex items-center gap-2 rounded-md border px-2.5 py-1.5 ' +
          (accent === 'amber' ? 'border-amber-300 bg-amber-100/70' : 'border-udhaar-line bg-udhaar-bg/70') +
          className
        }
      >
        <div className="min-w-0 flex-1 text-sm">
          <p className="truncate font-semibold">{value.name}</p>
          <p className="truncate font-mono text-[11px] text-inksoft">
            {value.phone}
            {value.gstin ? ` · ${value.gstin}` : ''}
            {value.state_code ? ` · ${value.state_code}` : ''}
          </p>
        </div>
        <button
          onClick={() => onChange(null)}
          className="shrink-0 text-[11px] font-bold uppercase tracking-wider text-inksoft underline-offset-2 transition-colors hover:text-ink hover:underline"
        >
          Change
        </button>
      </div>
    )
  }

  return (
    <div className={'relative ' + className}>
      <SearchIcon />
      <input
        autoFocus={autoFocus}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            clearQuery()
          }
        }}
        placeholder={placeholder}
        className={baseInputClass(accent)}
        role="combobox"
        aria-expanded={query.trim().length > 0}
        aria-autocomplete="list"
      />
      {query.trim() && (
        <div className="absolute left-0 right-0 top-full z-20 mt-1 overflow-hidden rounded-lg border border-line bg-white shadow-lg">
          {searching ? (
            <p className="px-3 py-2.5 text-xs text-inksoft">Searching…</p>
          ) : results === null ? (
            <p className="px-3 py-2.5 text-xs text-inksoft">Type to search.</p>
          ) : results.length === 0 ? (
            <p className="px-3 py-2.5 text-xs text-inksoft">No customers found — create a new one.</p>
          ) : (
            <ul className="max-h-56 divide-y divide-line-soft overflow-y-auto">
              {results.map((c) => (
                <li key={c.id}>
                  <button
                    type="button"
                    onMouseDown={(e) => {
                      e.preventDefault()
                      onChange(c)
                      clearQuery()
                    }}
                    className="w-full px-3 py-2 text-left transition-colors hover:bg-mint-50"
                  >
                    <p className="text-sm font-medium">{c.name}</p>
                    <p className="flex items-center gap-2 font-mono text-[11px] text-inksoft">
                      <span>{c.phone}</span>
                      {c.gstin && <span className="truncate">{c.gstin}</span>}
                      {c.current_balance > 0 && (
                        <span className="ml-auto rounded bg-udhaar-bg px-1 py-0.5 font-semibold text-udhaar-text">
                          owes ₹{money(c.current_balance)}
                        </span>
                      )}
                    </p>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {offline && (
            <p className="border-t border-dashed border-line px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-marigold-text">
              Server offline — showing saved cache
            </p>
          )}
        </div>
      )}
    </div>
  )
}

export interface CustomerQueryFieldProps {
  onResults: (results: Customer[] | null) => void
  customerType?: 'B2C' | 'B2B'
  fallbackPool?: Customer[]
  matchGstin?: boolean
  placeholder?: string
  limit?: number
}

export function CustomerQueryField({
  onResults,
  customerType,
  fallbackPool,
  matchGstin,
  placeholder = 'Search customers…',
  limit = 10,
}: CustomerQueryFieldProps) {
  const { query, setQuery, clearQuery, results, searching, offline } = useCustomerQuery({
    customerType,
    fallbackPool,
    matchGstin,
    limit,
  })

  useEffect(() => {
    onResults(results)
  }, [results, onResults])

  return (
    <div className="relative">
      <SearchIcon />
      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            clearQuery()
          }
        }}
        placeholder={placeholder}
        className={baseInputClass('pine')}
        role="searchbox"
      />
      {searching && (
        <span className="absolute right-2.5 top-1/2 -translate-y-1/2 animate-pulse text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Searching…
        </span>
      )}
      {offline && (
        <span className="ml-1 text-[10px] font-semibold uppercase tracking-wider text-marigold-text">
          offline results
        </span>
      )}
    </div>
  )
}