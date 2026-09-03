import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { money } from '../lib/format'
import Pagination, { usePaged } from '../components/Pagination'
import Modal from '../components/Modal'
import { SalesInvoiceModal } from '../components/SalesInvoiceModal'
import { CustomerQueryField } from '../components/CustomerSearch'
import type { Customer, LedgerEntry } from '../types'

interface Paged<T> {
  page: number
  pageCount: number
  setPage: (n: number) => void
  slice: T[]
  total: number
  start: number
}

export default function Customers({ onMutated }: { onMutated: () => Promise<void> }) {
  const [customers, setCustomers] = useState<Customer[]>([])
  const [b2cFilter, setB2cFilter] = useState<Customer[] | null>(null)
  const [b2bFilter, setB2bFilter] = useState<Customer[] | null>(null)
  const [error, setError] = useState('')
  const [payFor, setPayFor] = useState<Customer | null>(null)
  const [ledgerFor, setLedgerFor] = useState<Customer | null>(null)

  const isB2B = (c: Customer) => (c.customer_type ?? 'B2C') === 'B2B'

  const b2cFull = useMemo(() => customers.filter((c) => !isB2B(c)), [customers])
  const b2bFull = useMemo(() => customers.filter((c) => isB2B(c)), [customers])
  const b2cList = b2cFilter ?? b2cFull
  const b2bList = b2bFilter ?? b2bFull

  const b2cPage = usePaged(b2cList, 10)
  const b2bPage = usePaged(b2bList, 10)

  const load = async () => {
    try {
      const res = await api.customers()
      setCustomers(res.customers)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const refreshAll = async () => {
    await Promise.all([load(), onMutated()])
  }

  const bumpLimit = async (c: Customer, newLimit: number) => {
    if (newLimit < 0 || newLimit === c.credit_limit) return
    try {
      await api.updateCustomer(c.id, {
        name: c.name, phone: c.phone, credit_limit: newLimit,
        gstin: c.gstin || undefined, customer_type: c.customer_type || undefined, state_code: c.state_code || undefined,
      })
      await refreshAll()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const variants = {
    udhaar: {
      title: 'B2C credit customers',
      hint: 'Retail customers on credit. Issues udhaar and is tracked in Khata.',
      searchPlaceholder: 'Search B2C customers by name or phone…',
      searchBar: 'border-udhaar-line',
    },
    amber: {
      title: 'B2B credit customers',
      hint: 'Business customers — GSTIN, credit limits and invoices.',
      searchPlaceholder: 'Search B2B customers by name, phone or GSTIN…',
      searchBar: 'border-amber-300',
    },
  } as const

  const tables = [
    {
      key: 'B2C' as const,
      page: b2cPage,
      list: b2cFull,
      filtered: b2cFilter !== null,
      onResults: (r: Customer[] | null) => setB2cFilter(r),
      customerType: 'B2C' as const,
      style: variants.udhaar,
      emptyHint: b2cFilter === null
        ? 'No B2C credit customers yet — create one from the Billing page.'
        : 'No B2C customers match your search. Create one from the Billing page.',
    },
    {
      key: 'B2B' as const,
      page: b2bPage,
      list: b2bFull,
      filtered: b2bFilter !== null,
      onResults: (r: Customer[] | null) => setB2bFilter(r),
      customerType: 'B2B' as const,
      style: variants.amber,
      emptyHint: b2bFilter === null
        ? 'No B2B credit customers yet — create one from the Billing page.'
        : 'No B2B customers match your search. Create one from the Billing page.',
    },
  ]

  return (
    <div className="space-y-8">
      {error && (
        <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
      )}

      {tables.map((tbl) => (
        <section key={tbl.key} className="space-y-3">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <div>
              <h3 className="font-display text-sm font-bold uppercase tracking-wide">{tbl.style.title}</h3>
              <p className="text-xs text-inksoft">{tbl.style.hint}</p>
            </div>
            <span className="font-mono text-xs text-inksoft">{tbl.list.length} total</span>
          </div>

          <CustomerQueryField
            customerType={tbl.customerType}
            matchGstin={tbl.customerType === 'B2B'}
            onResults={tbl.onResults}
            placeholder={tbl.style.searchPlaceholder}
            limit={20}
          />

          <div className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
            <Table
              page={tbl.page}
              filtered={tbl.filtered}
              emptyHint={tbl.emptyHint}
              onPay={setPayFor}
              onLedger={setLedgerFor}
              onLimit={bumpLimit}
            />
          </div>
        </section>
      ))}

      {payFor && (
        <PayModal
          customer={payFor}
          onClose={() => setPayFor(null)}
          onPaid={async () => {
            await refreshAll()
            setPayFor(null)
          }}
        />
      )}
      {ledgerFor && (
        <LedgerModal
          customer={ledgerFor}
          onClose={async () => {
            setLedgerFor(null)
            await load()
          }}
        />
      )}
    </div>
  )
}

function Table({
  page,
  filtered,
  emptyHint,
  onPay,
  onLedger,
  onLimit,
}: {
  page: Paged<Customer>
  filtered: boolean
  emptyHint: string
  onPay: (c: Customer) => void
  onLedger: (c: Customer) => void
  onLimit: (c: Customer, v: number) => void
}) {
  return (
    <>
      <table className="w-full min-w-[620px] text-sm">
        <thead>
          <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
            <th className="px-4 py-2.5">Customer</th>
            <th className="px-4 py-2.5">GST</th>
            <th className="px-2 py-2.5 text-right">Balance</th>
            <th className="px-2 py-2.5 text-right">Credit limit</th>
            <th className="px-4 py-2.5 text-center">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line-soft">
          {page.slice.map((c) => (
            <tr key={c.id} className="hover:bg-mint-50/50">
              <td className="px-4 py-2">
                <p className="font-medium">{c.name}</p>
                <p className="font-mono text-xs text-inksoft">{c.phone}</p>
              </td>
              <td className="px-4 py-2 text-xs text-inksoft">
                {c.gstin ? (
                  <span className="font-mono">{c.gstin}</span>
                ) : (
                  <span className="italic text-inksoft/50">—</span>
                )}
                {c.customer_type && (
                  <span className="ml-1 rounded bg-mint-50 px-1 py-0.5 text-[10px] font-semibold">{c.customer_type}</span>
                )}
              </td>
              <td
                className={
                  'px-2 py-2 text-right font-mono font-semibold tabular-nums ' +
                  (c.current_balance > 0 ? 'text-udhaar-text' : 'text-inksoft/60')
                }
              >
                ₹{money(c.current_balance)}
              </td>
              <td className="px-2 py-2 text-right">
                <LimitEditor limit={c.credit_limit} onCommit={(v) => onLimit(c, v)} />
              </td>
              <td className="px-4 py-2">
                <div className="flex justify-center gap-1.5">
                  <button
                    onClick={() => onPay(c)}
                    disabled={c.current_balance <= 0}
                    title={c.current_balance <= 0 ? 'Nothing outstanding' : 'Collect payment'}
                    className="rounded-md bg-pine-700 px-2.5 py-1 text-xs font-semibold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
                  >
                    Collect
                  </button>
                  <button
                    onClick={() => onLedger(c)}
                    className="rounded-md border border-udhaar-line px-2.5 py-1 text-xs font-semibold text-udhaar-text transition-colors hover:bg-udhaar-bg"
                  >
                    Ledger
                  </button>
                </div>
              </td>
            </tr>
          ))}
          {page.total === 0 && (
            <tr>
              <td colSpan={5} className="px-4 py-10 text-center text-sm text-inksoft">
                {filtered ? 'No customers match your search — try a different name, phone or GSTIN.' : emptyHint}
              </td>
            </tr>
          )}
        </tbody>
      </table>
      <Pagination
        page={page.page}
        pageCount={page.pageCount}
        total={page.total}
        start={page.start}
        pageSize={10}
        onPage={page.setPage}
      />
    </>
  )
}

function PayModal({
  customer,
  onClose,
  onPaid,
}: {
  customer: Customer
  onClose: () => void
  onPaid: () => Promise<void>
}) {
  const [amount, setAmount] = useState(String(customer.current_balance))
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const parsed = Number(amount) || 0

  const submit = async () => {
    if (busy || parsed <= 0) return
    setBusy(true)
    setError('')
    try {
      await api.recordPayment(customer.id, Math.round(parsed * 100) / 100, notes.trim())
      await onPaid()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setBusy(false)
    }
  }

  return (
    <Modal title={`Collect payment — ${customer.name}`} onClose={onClose}>
      <p className="rounded-lg bg-udhaar-bg px-3 py-2 text-sm text-udhaar-deep">
        Outstanding balance:{' '}
        <strong className="font-mono tabular-nums">₹{money(customer.current_balance)}</strong>
      </p>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
        Amount received (₹)
        <input
          autoFocus
          inputMode="decimal"
          value={amount}
          onChange={(e) => /^\d*\.?\d{0,2}$/.test(e.target.value) && setAmount(e.target.value)}
          className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-lg tabular-nums focus:border-pine-600"
        />
      </label>
      <div className="flex flex-wrap gap-2 text-xs">
        <button
          onClick={() => setAmount(String(customer.current_balance))}
          className="rounded-md border border-line bg-white px-2.5 py-1.5 font-mono font-medium text-ink shadow-[0_1px_0_var(--color-line)] transition-colors hover:bg-mint-50"
        >
          Full ₹{money(customer.current_balance)}
        </button>
        <button
          onClick={() => setAmount(String(customer.current_balance / 2))}
          className="rounded-md border border-line bg-white px-2.5 py-1.5 font-mono font-medium text-ink shadow-[0_1px_0_var(--color-line)] transition-colors hover:bg-mint-50"
        >
          Half ₹{money(customer.current_balance / 2)}
        </button>
        <button
          onClick={() => setAmount('')}
          className="rounded-md px-2.5 py-1.5 text-inksoft/70 transition-colors hover:bg-mint-50"
        >
          Clear
        </button>
      </div>
      <input
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        placeholder="Reference / notes (optional) — e.g. UPI ref, receipt no."
        className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
      />
      {parsed > customer.current_balance + 0.004 && (
        <p className="text-xs font-semibold text-brick-text">
          Amount exceeds the outstanding balance — the server will reject it.
        </p>
      )}
      {error && (
        <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
      )}
      <button
        onClick={() => void submit()}
        disabled={busy || parsed <= 0 || parsed > customer.current_balance + 0.004}
        className="w-full rounded-lg bg-pine-700 px-4 py-2.5 text-sm font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
      >
        {busy
          ? 'Recording…'
          : `Record payment of ₹${money(parsed)}${parsed >= customer.current_balance - 0.004 && parsed > 0 ? ' (settles in full)' : ''}`}
      </button>
    </Modal>
  )
}

function invoiceNoFromNote(notes: string): string | null {
  const m = /^Invoice\s*#?\s*(\S+)\s*$/.exec(notes.trim())
  return m ? m[1] : null
}

function LedgerModal({ customer, onClose }: { customer: Customer; onClose: () => void }) {
  const [entries, setEntries] = useState<LedgerEntry[] | null>(null)
  const [error, setError] = useState('')
  const [invoiceNo, setInvoiceNo] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void api
      .ledger(customer.id)
      .then((res) => !cancelled && setEntries(res.entries))
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : String(err)))
    return () => {
      cancelled = true
    }
  }, [customer.id])

  return (
    <>
      <Modal title={`Credit ledger — ${customer.name}`} onClose={() => void onClose()} wide>
      <p className="text-sm text-inksoft">
        Phone <span className="font-mono font-medium">{customer.phone}</span> · Current balance{' '}
        <strong className="font-mono tabular-nums text-udhaar-text">₹{money(customer.current_balance)}</strong>{' '}
        · Limit ₹{money(customer.credit_limit)}
      </p>
      {error && (
        <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
      )}
      {entries === null ? (
        <p className="py-8 text-center text-sm text-inksoft">Loading history…</p>
      ) : entries.length === 0 ? (
        <p className="py-8 text-center text-sm text-inksoft">
          No credit activity yet for this customer.
        </p>
      ) : (
        <div className="max-h-[55vh] overflow-y-auto rounded-lg border border-line">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-mint-50 shadow-[0_1px_0_var(--color-line)]">
              <tr className="text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                <th className="px-3 py-2">Date</th>
                <th className="px-2 py-2">Type</th>
                <th className="px-2 py-2 text-right">Amount</th>
                <th className="px-2 py-2 text-right">Balance</th>
                <th className="px-3 py-2">Notes</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-soft">
              {entries.map((e) => {
                const invoiceNo = e.entry_type === 'CREDIT_SALE' ? invoiceNoFromNote(e.notes) : null
                return (
                  <tr key={e.id}>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-inksoft">
                      {new Date(e.created_at).toLocaleString([], {
                        dateStyle: 'medium',
                        timeStyle: 'short',
                      })}
                    </td>
                    <td className="px-2 py-1.5">
                      <span
                        className={
                          'rounded-full px-2 py-0.5 text-[11px] font-semibold ' +
                          (e.entry_type === 'PAYMENT'
                            ? 'bg-safe-bg text-safe-text'
                            : e.entry_type === 'CREDIT_SALE'
                              ? 'bg-udhaar-bg text-udhaar-text'
                              : 'bg-marigold-bg text-marigold-text')
                        }
                      >
                        {e.entry_type === 'CREDIT_SALE' ? 'Sale' : e.entry_type === 'PAYMENT' ? 'Payment' : 'Adjustment'}
                      </span>
                    </td>
                    <td
                      className={
                        'px-2 py-1.5 text-right font-mono font-semibold tabular-nums ' +
                        (e.amount >= 0 ? 'text-udhaar-text' : 'text-safe-text')
                      }
                    >
                      {e.amount > 0 ? '+' : ''}
                      {money(e.amount)}
                    </td>
                    <td className="px-2 py-1.5 text-right font-mono tabular-nums">₹{money(e.balance_after)}</td>
                    <td className="max-w-[220px] truncate px-3 py-1.5 text-xs text-inksoft" title={e.notes}>
                      {invoiceNo ? (
                        <>
                          Invoice{' '}
                          <button
                            onClick={() => setInvoiceNo(invoiceNo)}
                            className="rounded font-mono font-semibold text-pine-700 underline-offset-2 hover:underline"
                            title="View invoice details"
                          >
                            {invoiceNo}
                          </button>
                        </>
                      ) : (
                        e.notes
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <p className="text-xs text-inksoft/70">
        Newest first. Balance column shows the running outstanding after each entry.
      </p>
      </Modal>
      {invoiceNo && (
        <SalesInvoiceModal
          load={() => api.salesInvoiceByNo(customer.id, invoiceNo)}
          onClose={() => setInvoiceNo(null)}
        />
      )}
    </>
  )
}

function LimitEditor({ limit, onCommit }: { limit: number; onCommit: (v: number) => void }) {
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState(String(limit))

  useEffect(() => setValue(String(limit)), [limit])

  if (!editing) {
    return (
      <button
        onClick={() => setEditing(true)}
        className="rounded font-mono tabular-nums underline-offset-2 hover:underline"
        title="Click to edit"
      >
        ₹{money(limit)}
      </button>
    )
  }
  return (
    <input
      autoFocus
      inputMode="decimal"
      value={value}
      onChange={(e) => /^\d*\.?\d*$/.test(e.target.value) && setValue(e.target.value)}
      onBlur={() => {
        setEditing(false)
        onCommit(Number(value) || 0)
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
        if (e.key === 'Escape') {
          setValue(String(limit))
          setEditing(false)
        }
      }}
      className="w-24 rounded-md border border-pine-300 px-2 py-1 text-right font-mono tabular-nums focus:border-pine-600"
    />
  )
}