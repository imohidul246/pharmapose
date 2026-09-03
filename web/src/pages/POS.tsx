import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../lib/api'
import { loadCachedCustomers, loadCachedMedicines, upsertCachedCustomer, getCachedMedicineTax, syncLocalCache } from '../lib/db'
import { daysUntil, expiryClass, money } from '../lib/format'
import { searchMedicines, type SearchHit } from '../lib/search'
import CustomerForm from '../components/CustomerForm'
import CustomerSearch from '../components/CustomerSearch'
import TaxEditor from '../components/TaxEditor'
import type {
  CheckoutResponse,
  Customer,
  DiscountType,
  MedicineTaxConfig,
  MedicineWithBatches,
  PaymentType,
} from '../types'

interface CartLine {
  batchId: string
  medicineId: string
  medicineName: string
  batchNumber: string
  expiryDate: string
  unitPrice: number
  purchasePrice: number
  quantity: number
  maxStock: number
  discountType: DiscountType
  discountValue: number
  // B2B fields
  sellPrice: number | null  // null = use unitPrice (MRP), number = custom sell price
  bonusQuantity: number
}

function roundMoney(n: number): number {
  return Math.round((n + Number.EPSILON) * 100) / 100
}

function lineSellPrice(l: CartLine): number {
  return l.sellPrice ?? l.unitPrice
}

function lineGross(l: CartLine): number {
  return roundMoney(lineSellPrice(l) * l.quantity)
}

function lineDiscountAmount(l: CartLine): number {
  if (l.discountValue <= 0) return 0
  const gross = lineGross(l)
  const raw = l.discountType === 'percent' ? (gross * l.discountValue) / 100 : l.discountValue
  return roundMoney(Math.min(Math.max(raw, 0), gross))
}

type SaleType = 'RETAIL' | 'B2B'

export default function POS({ cacheVersion }: { cacheVersion: number }) {
  const [medicines, setMedicines] = useState<MedicineWithBatches[]>([])
  const [knownCustomers, setKnownCustomers] = useState<Customer[]>([])
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const [pickerFor, setPickerFor] = useState<MedicineWithBatches | null>(null)
  const [cart, setCart] = useState<CartLine[]>([])
  const [paymentType, setPaymentType] = useState<PaymentType>('CASH')
  const [customer, setCustomer] = useState<Customer | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [receipt, setReceipt] = useState<CheckoutResponse | null>(null)
  // B2B state
  const [saleType, setSaleType] = useState<SaleType>('RETAIL')
  const [b2bCustomer, setB2bCustomer] = useState<Customer | null>(null)
  const [buyerName, setBuyerName] = useState('')
  const [buyerGstin, setBuyerGstin] = useState('')
  const [buyerAddress, setBuyerAddress] = useState('')
  // "+ Create customer" modal for retail credit or B2B; auto-selects after create
  const [createFor, setCreateFor] = useState<'RETAIL' | 'B2B' | null>(null)
  // Tax config per cart line (loaded from offline cache) + which line is being edited
  const [taxByLine, setTaxByLine] = useState<Record<string, MedicineTaxConfig | null>>({})
  const [editLineId, setEditLineId] = useState<string | null>(null)

  const searchRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    searchRef.current?.focus()
  }, [])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      const [meds, custs] = await Promise.all([loadCachedMedicines(), loadCachedCustomers()])
      if (!cancelled) {
        setMedicines(meds)
        setKnownCustomers(custs)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [cacheVersion])

  const hits = useMemo(() => searchMedicines(medicines, query), [medicines, query])
  useEffect(() => setHighlight(0), [query])

  const addBatch = useCallback(
    (m: MedicineWithBatches, batchId: string) => {
      const batch =
        m.batches.find((b) => b.id === batchId && b.current_stock > 0) ??
        m.batches.filter((b) => b.current_stock > 0).sort((a, b) => a.expiry_date.localeCompare(b.expiry_date))[0]
      if (!batch) {
        setError(`${m.name}: no stock in any active batch`)
        return
      }
      setError('')
      setReceipt(null)
      setCart((prev) => {
        const existing = prev.find((l) => l.batchId === batch.id)
        if (existing) {
          return prev.map((l) =>
            l.batchId === batch.id
              ? { ...l, quantity: Math.min(l.quantity + 1, l.maxStock) }
              : l,
          )
        }
        void getCachedMedicineTax(m.id).then((cfg) => {
          setTaxByLine((prev) => ({ ...prev, [batch.id]: cfg }))
        })
        return [
          ...prev,
          {
            batchId: batch.id,
            medicineId: m.id,
            medicineName: m.name,
            batchNumber: batch.batch_number,
            expiryDate: batch.expiry_date,
            unitPrice: batch.sale_price,
            purchasePrice: batch.purchase_price,
            quantity: 1,
            maxStock: batch.current_stock,
            discountType: 'percent',
            discountValue: 0,
            sellPrice: null,
            bonusQuantity: 0,
          },
        ]
      })
    },
    [],
  )

  const onSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHighlight((h) => Math.min(h + 1, hits.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlight((h) => Math.max(h - 1, 0))
    } else if (e.key === 'Enter' && hits.length > 0) {
      e.preventDefault()
      const hit = hits[Math.min(highlight, hits.length - 1)]
      ;(e.target as HTMLInputElement).blur()
      setPickerFor(hit.medicine)
    } else if (e.key === 'Escape') {
      setQuery('')
    }
  }

  const changeQty = (batchId: string, delta: number) =>
    setCart((prev) =>
      prev.map((l) =>
        l.batchId === batchId
          ? { ...l, quantity: Math.max(1, Math.min(l.quantity + delta, l.maxStock)) }
          : l,
      ),
    )

  const removeLine = (batchId: string) =>
    setCart((prev) => prev.filter((l) => l.batchId !== batchId))

  const patchDiscount = (batchId: string, patch: Partial<Pick<CartLine, 'discountType' | 'discountValue'>>) =>
    setCart((prev) => prev.map((l) => (l.batchId === batchId ? { ...l, ...patch } : l)))

  const patchB2B = (batchId: string, patch: Partial<Pick<CartLine, 'sellPrice' | 'bonusQuantity'>>) =>
    setCart((prev) => prev.map((l) => (l.batchId === batchId ? { ...l, ...patch } : l)))

  const total = roundMoney(cart.reduce((acc, l) => acc + roundMoney(lineGross(l) - lineDiscountAmount(l)), 0))
  const totalDiscount = roundMoney(cart.reduce((acc, l) => acc + lineDiscountAmount(l), 0))

  // The customer attached to this sale: B2B uses its own selector (available
  // for both cash and credit), otherwise credit requires a customer.
  const isB2B = saleType === 'B2B'
  const activeCustomerId = (isB2B ? b2bCustomer?.id : customer?.id) || undefined
  const selectedCustomer = isB2B ? b2bCustomer : customer
  const projectedBalance = selectedCustomer
    ? selectedCustomer.current_balance + total
    : 0
  const creditBreached =
    paymentType === 'CREDIT' &&
    !!selectedCustomer &&
    projectedBalance > selectedCustomer.credit_limit

  // Auto-fill buyer details from the selected B2B customer profile. The seller
  // may still override the fields after autofill by editing them directly.
  useEffect(() => {
    if (!isB2B) return
    if (!b2bCustomer) return
    setBuyerName(b2bCustomer.name || '')
    if (b2bCustomer.gstin) setBuyerGstin(b2bCustomer.gstin)
    if (b2bCustomer.billing_address) setBuyerAddress(b2bCustomer.billing_address)
  }, [b2bCustomer, isB2B])

  // Clear the B2B-specific selection whenever the mode leaves B2B so state
  // never leaks into retail bills.
  useEffect(() => {
    if (isB2B) return
    setB2bCustomer(null)
  }, [isB2B])

  const addCreatedCustomer = async (c: Customer) => {
    setKnownCustomers((prev) => (prev.some((x) => x.id === c.id) ? prev : [...prev, c]))
    await upsertCachedCustomer(c)
    setError('')
  }

  const checkout = async () => {
    if (cart.length === 0 || busy) return
    if (paymentType === 'CREDIT' && !selectedCustomer) {
      setError('Customer is required for credit sales.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const res = await api.checkout({
        payment_type: paymentType,
        // B2B attaches to a selected customer on both cash & credit; otherwise
        // credit requires a customer.
        customer_id: activeCustomerId || undefined,
        store_id: import.meta.env.VITE_STORE_ID || undefined,
        place_of_supply: selectedCustomer?.state_code || undefined,
        sale_type: saleType,
        buyer_name: saleType === 'B2B' ? buyerName : undefined,
        buyer_gstin: saleType === 'B2B' ? buyerGstin : undefined,
        buyer_address: saleType === 'B2B' ? buyerAddress : undefined,
        items: cart.map((l) => ({
          batch_id: l.batchId,
          quantity: l.quantity,
          sell_price: saleType === 'B2B' && l.sellPrice !== null ? l.sellPrice : undefined,
          bonus_quantity: saleType === 'B2B' ? l.bonusQuantity : 0,
          discount:
            l.discountValue > 0
              ? { type: l.discountType, value: l.discountValue }
              : undefined,
        })),
      })
      void syncLocalCache().catch(() => {})
      setReceipt(res)
      setCart([])
      setPaymentType('CASH')
      setCustomer(null)
      setB2bCustomer(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
      searchRef.current?.focus()
    }
  }

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
      {/* Search column */}
      <section>
        <div className="relative">
          <svg
            aria-hidden
            viewBox="0 0 20 20"
            className="pointer-events-none absolute left-3.5 top-1/2 h-4.5 w-4.5 -translate-y-1/2 text-inksoft"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <circle cx="9" cy="9" r="6" />
            <path d="m14 14 4 4" strokeLinecap="round" />
          </svg>
          <input
            ref={searchRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onSearchKeyDown}
            placeholder="Search brand or salt…"
            className="h-12 w-full rounded-xl border border-line bg-white pl-10 pr-4 text-[15px] shadow-sm outline-none placeholder:text-inksoft/60 focus:border-pine-600"
            autoFocus
          />
        </div>

        <div className="mt-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-0.5 text-xs text-inksoft">
          <span>
            <span className="font-mono font-medium">{hits.length}</span>{' '}
            {query.trim()
              ? hits.length === 1
                ? 'match'
                : 'matches'
              : hits.length === 1
                ? 'medicine in catalog'
                : 'medicines in catalog'}
          </span>
          <span className="flex items-center gap-1.5">
            <kbd className="keycap">↑</kbd>
            <kbd className="keycap">↓</kbd> navigate
            <span className="text-line">·</span>
            <kbd className="keycap">⏎</kbd> add
            <span className="text-line">·</span>
            <kbd className="keycap">esc</kbd> clear
          </span>
        </div>

        <div className="mt-2 overflow-hidden rounded-xl border border-line bg-white shadow-sm">
          {hits.length === 0 && (
            <p className="px-4 py-10 text-center text-sm text-inksoft">
              {query
                ? `No medicines match "${query}". Try a salt name like paracetamol.`
                : 'Nothing cached yet — press Sync now, or add stock via the Purchases tab.'}
            </p>
          )}
          <ul className="divide-y divide-line-soft">
            {hits.map((hit, i) => (
              <SearchRow
                key={hit.medicine.id}
                hit={hit}
                highlighted={i === highlight}
                onPick={() => setPickerFor(hit.medicine)}
                onHover={() => setHighlight(i)}
              />
            ))}
          </ul>
        </div>

        {receipt && (
          <div className="mt-4 rounded-xl border border-dashed border-pine-600/60 bg-white p-3.5 shadow-sm">
            <div className="flex items-center gap-4">
              <span aria-hidden className="stamp shrink-0 px-2.5 py-1 text-[11px]">
                Recorded
              </span>
              <p className="min-w-0 flex-1 text-sm leading-snug">
                Invoice <span className="font-mono font-semibold">{receipt.invoice.invoice_no}</span>{' '}
                · {receipt.invoice.payment_type === 'CREDIT' ? 'Credit' : 'Cash'}
              </p>
              {receipt.invoice.sale_type === 'B2B' && (
                <button
                  onClick={async () => {
                    try {
                      const blob = await api.downloadB2BInvoicePDF(receipt.invoice.id)
                      const url = URL.createObjectURL(blob)
                      const a = window.document.createElement('a')
                      a.href = url
                      a.download = `B2B_${receipt.invoice.invoice_no}.pdf`
                      a.click()
                      setTimeout(() => URL.revokeObjectURL(url), 10_000)
                    } catch (err) {
                      setError(err instanceof Error ? err.message : String(err))
                    }
                  }}
                  className="shrink-0 rounded-md border border-pine-600 bg-pine-700 px-3 py-1 text-xs font-semibold text-white transition-colors hover:bg-pine-600"
                >
                  Download PDF
                </button>
              )}
              <button
                onClick={() => setReceipt(null)}
                className="shrink-0 rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
              >
                Dismiss
              </button>
            </div>
            {/* GST summary */}
            {receipt.invoice.supply_type && receipt.invoice.tax_total != null && receipt.invoice.tax_total > 0 && (
              <div className="mt-2 space-y-1 rounded-lg bg-mint-50 px-3 py-2 text-xs">
                <div className="flex justify-between text-inksoft">
                  <span className="font-semibold uppercase tracking-wider">Supply</span>
                  <span className="font-mono">
                    {receipt.invoice.supply_type === 'INTER_STATE' ? 'Inter-state (IGST)' : 'Intra-state (CGST+SGST)'}
                  </span>
                </div>
                {receipt.invoice.gross_amount != null && (
                  <div className="flex justify-between text-inksoft">
                    <span>Gross</span>
                    <span className="font-mono">₹{money(receipt.invoice.gross_amount)}</span>
                  </div>
                )}
                {receipt.invoice.discount_total > 0 && (
                  <div className="flex justify-between text-safe-text">
                    <span>Discount</span>
                    <span className="font-mono">−₹{money(receipt.invoice.discount_total)}</span>
                  </div>
                )}
                {receipt.invoice.taxable_amount != null && (
                  <div className="flex justify-between text-inksoft">
                    <span>Taxable value</span>
                    <span className="font-mono">₹{money(receipt.invoice.taxable_amount)}</span>
                  </div>
                )}
                {receipt.invoice.supply_type === 'INTER_STATE' ? (
                  receipt.invoice.igst_total != null && (
                    <div className="flex justify-between text-inksoft">
                      <span>IGST</span>
                      <span className="font-mono">₹{money(receipt.invoice.igst_total)}</span>
                    </div>
                  )
                ) : (
                  <>
                    {receipt.invoice.cgst_total != null && (
                      <div className="flex justify-between text-inksoft">
                        <span>CGST</span>
                        <span className="font-mono">₹{money(receipt.invoice.cgst_total)}</span>
                      </div>
                    )}
                    {receipt.invoice.sgst_total != null && (
                      <div className="flex justify-between text-inksoft">
                        <span>SGST</span>
                        <span className="font-mono">₹{money(receipt.invoice.sgst_total)}</span>
                      </div>
                    )}
                  </>
                )}
                <div className="flex justify-between border-t border-line pt-1 text-inksoft">
                  <span className="font-semibold">Tax total</span>
                  <span className="font-mono font-semibold">₹{money(receipt.invoice.tax_total)}</span>
                </div>
                <div className="flex justify-between pt-0.5">
                  <span className="font-display text-xs font-black uppercase tracking-wide">Grand total</span>
                  <span className="font-display text-base font-black tracking-tight tabular-nums">
                    ₹{money(receipt.invoice.grand_total ?? receipt.invoice.total_amount)}
                  </span>
                </div>
              </div>
            )}
            {!receipt.invoice.supply_type && (
              <div className="mt-2 flex items-center gap-2 text-sm">
                <span className="font-mono font-semibold">₹{money(receipt.invoice.total_amount)}</span>
                {receipt.invoice.discount_total > 0 && (
                  <span className="text-xs text-safe-text">discount ₹{money(receipt.invoice.discount_total)}</span>
                )}
              </div>
            )}
          </div>
        )}
        {error && (
          <div className="mt-4 rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
            {error}
          </div>
        )}
      </section>

      {/* Cart — the bill sheet */}
      <section className="overflow-hidden rounded-xl border border-line bg-white shadow-md shadow-pine-950/[0.04]">
        <header className="flex items-baseline justify-between px-4 pb-3 pt-3.5">
          <h2 className="font-display text-sm font-bold uppercase tracking-wide">Current bill</h2>
          <span className="font-mono text-xs text-inksoft">
            {cart.length} {cart.length === 1 ? 'line' : 'lines'}
          </span>
        </header>

        {/* Sale Type Toggle */}
        <div className="mx-4 mb-3">
          <div className="flex overflow-hidden rounded-lg border border-line">
            {(['RETAIL', 'B2B'] as SaleType[]).map((st) => (
              <button
                key={st}
                onClick={() => setSaleType(st)}
                className={
                  'flex-1 px-3 py-2 text-sm font-semibold transition-colors ' +
                  (saleType === st
                    ? st === 'B2B'
                      ? 'bg-amber-600 text-white'
                      : 'bg-pine-700 text-white'
                    : 'bg-white text-inksoft hover:bg-mint-50')
                }
              >
                {st === 'RETAIL' ? 'Retail' : 'B2B Wholesale'}
              </button>
            ))}
          </div>
        </div>

        {/* B2B Buyer Details */}
        {isB2B && (
          <div className="mx-4 mb-3 rounded-lg border border-amber-200 bg-amber-50 p-3">
            <p className="mb-2 text-xs font-bold uppercase tracking-wider text-amber-700">Buyer Details</p>

            {/* Search existing customer or create a new one inline */}
            <div className="space-y-2">
              <label className="block text-[10px] font-bold uppercase tracking-wider text-amber-700">
                Customer
              </label>
              <CustomerSearch
                value={b2bCustomer}
                onChange={(c) => {
                  setB2bCustomer(c)
                  if (c) {
                    setBuyerName(c.name || '')
                    setBuyerGstin(c.gstin || '')
                    setBuyerAddress(c.billing_address || '')
                  } else {
                    setBuyerName('')
                    setBuyerGstin('')
                    setBuyerAddress('')
                  }
                }}
                customerType="B2B"
                matchGstin
                fallbackPool={knownCustomers}
                placeholder="Search B2B customer (name · phone · GSTIN)…"
                accent="amber"
              />
              <button
                onClick={() => setCreateFor('B2B')}
                disabled={!!b2bCustomer}
                className="w-full rounded-md border border-dashed border-amber-400 bg-white px-2.5 py-1.5 text-xs font-semibold text-amber-700 transition-colors hover:bg-amber-100 disabled:opacity-40"
              >
                + New customer (not in base yet)
              </button>
            </div>

            {selectedCustomer && (
              <dl className="mt-2 grid grid-cols-3 gap-2 text-xs text-amber-800">
                <div>
                  <dt className="font-semibold uppercase tracking-wider text-[10px]">Outstanding</dt>
                  <dd className="font-mono tabular-nums">₹{money(selectedCustomer.current_balance)}</dd>
                </div>
                <div>
                  <dt className="font-semibold uppercase tracking-wider text-[10px]">Credit limit</dt>
                  <dd className="font-mono tabular-nums">₹{money(selectedCustomer.credit_limit)}</dd>
                </div>
                <div>
                  <dt className="font-semibold uppercase tracking-wider text-[10px]">After sale</dt>
                  <dd
                    className={
                      'font-mono tabular-nums ' + (creditBreached ? 'font-bold text-brick-text' : '')
                    }
                  >
                    ₹{money(projectedBalance)}
                  </dd>
                </div>
              </dl>
            )}

            <div className="mt-2 space-y-2 border-t border-amber-200 pt-2">
              <label className="block text-[10px] font-bold uppercase tracking-wider text-amber-700">
                Buyer details (editable)
              </label>
              <input
                value={buyerName}
                onChange={(e) => setBuyerName(e.target.value)}
                placeholder="Buyer / Business Name *"
                className="w-full rounded-md border border-amber-300 bg-white px-2.5 py-1.5 text-sm outline-none focus:border-amber-500"
              />
              <div className="grid grid-cols-2 gap-2">
                <input
                  value={buyerGstin}
                  onChange={(e) => setBuyerGstin(e.target.value)}
                  placeholder="GSTIN (optional)"
                  maxLength={15}
                  className="rounded-md border border-amber-300 bg-white px-2.5 py-1.5 text-sm outline-none focus:border-amber-500"
                />
                <input
                  value={buyerAddress}
                  onChange={(e) => setBuyerAddress(e.target.value)}
                  placeholder="Address (optional)"
                  className="rounded-md border border-amber-300 bg-white px-2.5 py-1.5 text-sm outline-none focus:border-amber-500"
                />
              </div>
            </div>
          </div>
        )}

        {cart.length === 0 ? (
          <div className="mx-4 mb-4 rounded-lg border border-dashed border-line px-4 py-12 text-center">
            <p className="text-sm font-medium text-inksoft">No items yet.</p>
            <p className="mt-1 text-xs text-inksoft/70">
              Search a brand or salt to start the bill.
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-dashed divide-line">
            {cart.map((l) => {
              const gross = lineGross(l)
              const disc = lineDiscountAmount(l)
              const net = roundMoney(gross - disc)
              const lineTax = taxByLine[l.batchId]
              const gstPct = lineTax?.tax_rate?.gst_rate ?? null
              return (
                <li key={l.batchId} className="px-4 py-3 text-sm">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-semibold">{l.medicineName}</p>
                      <p className="mt-0.5 truncate font-mono text-[11px] text-inksoft">
                        {lineTax && (
                          <span className="mr-1 inline-flex items-center gap-1 rounded bg-mint-100 px-1.5 py-0.5 font-semibold text-pine-800">
                            HSN {lineTax.hsn_code ? <span className="font-mono">{lineTax.hsn_code}</span> : '—'}
                            {gstPct != null && Number.isFinite(gstPct) && (
                              <span>· GST {gstPct}%</span>
                            )}
                          </span>
                        )}{' '}
                        Batch {l.batchNumber} ·{' '}
                        <span className={`rounded px-1 ${expiryClass(daysUntil(l.expiryDate))}`}>
                          exp {l.expiryDate}
                        </span>{' '}
                        · ₹{money(l.unitPrice)} · max {l.maxStock}
                      </p>
                    </div>
                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => changeQty(l.batchId, -1)}
                        aria-label="Decrease quantity"
                        className="h-7 w-7 rounded-md border border-line font-bold text-inksoft transition-colors hover:bg-mint-50 active:scale-95"
                      >
                        −
                      </button>
                      <span className="w-8 text-center font-mono font-semibold tabular-nums">
                        {l.quantity}
                      </span>
                      <button
                        onClick={() => changeQty(l.batchId, +1)}
                        disabled={l.quantity >= l.maxStock}
                        aria-label="Increase quantity"
                        className="h-7 w-7 rounded-md border border-line font-bold text-inksoft transition-colors hover:bg-mint-50 active:scale-95 disabled:opacity-40"
                      >
                        +
                      </button>
                    </div>
                    <span
                      className={
                        'w-20 text-right font-mono font-semibold tabular-nums ' +
                        (disc > 0 ? 'text-safe-text' : '')
                      }
                    >
                      ₹{money(net)}
                    </span>
                    <button
                      onClick={() => setEditLineId(l.batchId)}
                      title="Edit tax configuration"
                      className="rounded-md border border-pine-200 px-2 py-1 text-[11px] font-bold text-pine-700 transition-colors hover:bg-mint-50"
                    >
                      {lineTax || gstPct != null ? 'Edit tax' : 'Set tax'}
                    </button>
                    <button
                      onClick={() => removeLine(l.batchId)}
                      aria-label={`Remove ${l.medicineName}`}
                      className="rounded-md p-1 text-inksoft/60 transition-colors hover:bg-brick-bg hover:text-brick-text"
                    >
                      <svg viewBox="0 0 14 14" className="h-3.5 w-3.5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round">
                        <path d="M2 2l10 10M12 2L2 12" />
                      </svg>
                    </button>
                  </div>

                  {/* B2B: Sell Price + Bonus inputs */}
                  {isB2B && (
                    <div className="mt-1.5 flex items-center justify-end gap-2 text-xs">
                      <span className="text-[10px] font-semibold uppercase tracking-wider text-amber-600">MRP ₹{money(l.unitPrice)}</span>
                      <span className="text-inksoft">→</span>
                      <span className="text-[10px] font-semibold uppercase tracking-wider">Sell ₹</span>
                      <input
                        inputMode="decimal"
                        value={l.sellPrice === null ? '' : l.sellPrice}
                        onChange={(e) => {
                          const v = e.target.value
                          if (!/^\d*\.?\d{0,2}$/.test(v)) return
                          patchB2B(l.batchId, { sellPrice: v === '' ? null : Number(v) })
                        }}
                        placeholder={String(l.unitPrice)}
                        className="w-16 rounded-md border border-amber-200 bg-amber-50 px-1.5 py-1 text-right font-mono tabular-nums focus:border-amber-500"
                      />
                      <span className="text-[10px] font-semibold uppercase tracking-wider">Bonus</span>
                      <input
                        inputMode="numeric"
                        value={l.bonusQuantity === 0 ? '' : l.bonusQuantity}
                        onChange={(e) => {
                          const v = e.target.value
                          if (!/^\d*$/.test(v)) return
                          patchB2B(l.batchId, { bonusQuantity: Number(v) || 0 })
                        }}
                        placeholder="0"
                        className="w-10 rounded-md border border-amber-200 bg-amber-50 px-1.5 py-1 text-center font-mono tabular-nums focus:border-amber-500"
                      />
                    </div>
                  )}

                  {/* Discount row */}
                  <div className="mt-1.5 flex items-center justify-end gap-2 text-xs text-inksoft">
                    <span className="text-[10px] font-semibold uppercase tracking-wider">Disc</span>
                    <input
                      inputMode="decimal"
                      value={l.discountValue === 0 ? '' : l.discountValue}
                      onChange={(e) => {
                        const v = e.target.value
                        if (!/^\d*\.?\d{0,2}$/.test(v)) return
                        patchDiscount(l.batchId, { discountValue: Number(v) || 0 })
                      }}
                      placeholder="0"
                      className="w-16 rounded-md border border-line px-1.5 py-1 text-right font-mono tabular-nums focus:border-pine-600"
                    />
                    <div className="flex overflow-hidden rounded-md border border-line">
                      {(['percent', 'amount'] as DiscountType[]).map((t) => (
                        <button
                          key={t}
                          onClick={() => patchDiscount(l.batchId, { discountType: t })}
                          className={
                            'px-2 py-1 font-mono font-semibold transition-colors ' +
                            (l.discountType === t
                              ? 'bg-pine-700 text-white'
                              : 'bg-white hover:bg-mint-50')
                          }
                        >
                          {t === 'percent' ? '%' : '₹'}
                        </button>
                      ))}
                    </div>
                    {disc > 0 && (
                      <span className="rounded bg-safe-bg px-1.5 py-0.5 font-mono font-semibold tabular-nums text-safe-text">
                        −₹{money(disc)}
                      </span>
                    )}
                  </div>
                </li>
              )
            })}
          </ul>
        )}

        <footer className="space-y-3 border-t border-dashed border-line bg-mint-50/70 px-4 py-4">
          <div className="flex items-end justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-inksoft">
              Payable total
            </span>
            <span className="font-display text-2xl font-black tracking-tight tabular-nums">
              ₹{money(total)}
            </span>
          </div>
          {totalDiscount > 0 && (
            <p className="-mt-1 text-right font-mono text-xs font-medium text-safe-text">
              −₹{money(totalDiscount)} in discounts
            </p>
          )}

          <div className="grid grid-cols-2 gap-2">
            {(['CASH', 'CREDIT'] as PaymentType[]).map((pt) => (
              <button
                key={pt}
                onClick={() => setPaymentType(pt)}
                className={
                  'rounded-lg border px-3 py-2 text-sm font-semibold transition-colors ' +
                  (paymentType === pt
                    ? pt === 'CASH'
                      ? 'border-pine-700 bg-pine-700 text-white'
                      : 'border-udhaar bg-udhaar text-white'
                    : 'border-line bg-white text-inksoft hover:bg-mint-50')
                }
              >
                {pt === 'CASH' ? 'Cash' : 'Credit / Udhaar'}
              </button>
            ))}
          </div>

          {paymentType === 'CREDIT' && !isB2B && (
            <div className="rounded-lg border border-udhaar-line bg-udhaar-bg/60 p-3 text-sm">
              <p className="mb-2 text-[10px] font-bold uppercase tracking-wider text-udhaar-text">
                Credit customer
              </p>
              <CustomerSearch
                value={customer}
                onChange={setCustomer}
                customerType="B2C"
                fallbackPool={knownCustomers}
                placeholder="Search retail customer by name or phone…"
                accent="pine"
              />
              {!customer && (
                <button
                  onClick={() => setCreateFor('RETAIL')}
                  className="mt-2 w-full rounded-md border border-dashed border-udhaar-line bg-white px-2.5 py-1.5 text-xs font-semibold text-udhaar-text transition-colors hover:bg-udhaar-bg"
                >
                  + New customer
                </button>
              )}
              {customer && (
                <dl className="mt-2 grid grid-cols-3 gap-2 text-xs text-inksoft">
                  <div>
                    <dt className="font-semibold uppercase tracking-wider text-[10px]">Outstanding</dt>
                    <dd className="font-mono tabular-nums">₹{money(customer.current_balance)}</dd>
                  </div>
                  <div>
                    <dt className="font-semibold uppercase tracking-wider text-[10px]">After sale</dt>
                    <dd
                      className={
                        'font-mono tabular-nums ' + (creditBreached ? 'font-bold text-brick-text' : '')
                      }
                    >
                      ₹{money(projectedBalance)}
                    </dd>
                  </div>
                  <div>
                    <dt className="font-semibold uppercase tracking-wider text-[10px]">Limit</dt>
                    <dd className="font-mono tabular-nums">₹{money(customer.credit_limit)}</dd>
                  </div>
                </dl>
              )}
            </div>
          )}

          <button
            onClick={() => void checkout()}
            disabled={cart.length === 0 || busy || creditBreached || (paymentType === 'CREDIT' && !selectedCustomer) || (isB2B && !buyerName.trim())}
            className={
              'h-12 w-full rounded-xl font-display text-[15px] font-bold tracking-tight text-white shadow-sm transition-colors active:scale-[0.98] disabled:bg-line disabled:text-inksoft disabled:shadow-none ' +
              (isB2B
                ? 'bg-amber-600 hover:bg-amber-500 active:bg-amber-700'
                : 'bg-pine-700 hover:bg-pine-600 active:bg-pine-800')
            }
          >
            {busy ? 'Processing…' : isB2B ? `Complete B2B Sale — ₹${money(total)}` : `Complete Sale — ₹${money(total)}`}
          </button>
          {creditBreached && (
            <p className="text-center text-xs font-semibold text-brick-text">
              Credit limit exceeded — server will reject this sale.
            </p>
          )}
          {paymentType === 'CREDIT' && !isB2B && !customer && cart.length > 0 && (
            <p className="text-center text-xs font-semibold text-udhaar-text">
              Customer is required for credit sales — select or create one above.
            </p>
          )}
          {isB2B && !buyerName.trim() && cart.length > 0 && (
            <p className="text-center text-xs font-semibold text-amber-600">
              Enter buyer name to complete B2B sale.
            </p>
          )}
          {isB2B && paymentType === 'CREDIT' && !selectedCustomer && (
            <p className="text-center text-xs font-semibold text-udhaar-text">
              Credit B2B sale — select a customer above.
            </p>
          )}
        </footer>
      </section>

      {createFor && (
        <CustomerForm
          open
          onClose={() => setCreateFor(null)}
          defaultType={createFor === 'B2B' ? 'B2B' : 'B2C'}
          accent={createFor === 'B2B' ? 'amber' : 'pine'}
          title={createFor === 'B2B' ? 'New B2B customer' : 'New customer'}
          submitLabel={createFor === 'B2B' ? 'Create & use for this sale' : 'Create & select'}
          onCreated={async (c) => {
            await addCreatedCustomer(c)
            if (createFor === 'B2B') {
              setB2bCustomer(c)
              setBuyerName(c.name || '')
              setBuyerGstin(c.gstin || '')
              setBuyerAddress(c.billing_address || '')
            } else {
              setCustomer(c)
            }
            setCreateFor(null)
          }}
        />
      )}

      {pickerFor && (
        <BatchPickerModal
          medicine={pickerFor}
          onClose={() => setPickerFor(null)}
          onSelect={(batchId) => {
            addBatch(pickerFor, batchId)
            setPickerFor(null)
            setQuery('')
            searchRef.current?.focus()
          }}
        />
      )}

      {editLineId && (() => {
        const line = cart.find((l) => l.batchId === editLineId)
        if (!line) {
          setEditLineId(null)
          return null
        }
        return (
          <TaxEditor
            medicineId={line.medicineId}
            medicineName={line.medicineName}
            taxConfig={taxByLine[editLineId] ?? null}
            onClose={() => setEditLineId(null)}
            onSaved={(cfg) => {
              setTaxByLine((prev) => ({ ...prev, [editLineId]: cfg }))
              setEditLineId(null)
            }}
          />
        )
      })()}
    </div>
  )
}

function SearchRow({
  hit,
  highlighted,
  onPick,
  onHover,
}: {
  hit: SearchHit
  highlighted: boolean
  onPick: () => void
  onHover: () => void
}) {
  const m = hit.medicine
  const stock = m.batches.reduce((a, b) => a + b.current_stock, 0)
  const best = m.batches
    .filter((b) => b.current_stock > 0)
    .sort((a, b) => a.expiry_date.localeCompare(b.expiry_date))[0]
  return (
    <li
      onMouseEnter={onHover}
      onClick={onPick}
      className={
        'flex cursor-pointer items-center justify-between gap-3 px-4 py-2.5 transition-colors ' +
        (highlighted ? 'bg-mint-50 shadow-[inset_3px_0_0_var(--color-pine-600)]' : '')
      }
    >
      <div className="min-w-0">
        <p className={'truncate text-sm ' + (highlighted ? 'font-semibold' : 'font-medium')}>
          {m.name}
        </p>
        <p className="truncate text-xs text-inksoft">{m.salt_composition}</p>
      </div>
      <div className="shrink-0 text-right text-xs">
        {stock > 0 ? (
          <p className="font-mono font-medium text-ink">{stock} in stock</p>
        ) : (
          <p className="inline-block rounded bg-brick-bg px-1.5 py-0.5 font-semibold text-brick-text">
            out of stock
          </p>
        )}
        {best && (
          <p className="font-mono text-inksoft">
            from ₹{money(best.sale_price)}
          </p>
        )}
      </div>
    </li>
  )
}

function BatchPickerModal({
  medicine,
  onClose,
  onSelect,
}: {
  medicine: MedicineWithBatches
  onClose: () => void
  onSelect: (batchId: string) => void
}) {
  const [highlight, setHighlight] = useState(0)
  const [peekCost, setPeekCost] = useState(false)
  const active = useMemo(
    () =>
      medicine.batches
        .filter((b) => b.current_stock > 0)
        .sort((a, b) => a.expiry_date.localeCompare(b.expiry_date)),
    [medicine],
  )
  const rowRefs = useRef<(HTMLTableRowElement | null)[]>([])
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    panelRef.current?.focus()
  }, [])

  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
      return
    }
    if (active.length === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHighlight((h) => Math.min(h + 1, active.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlight((h) => Math.max(h - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      onSelect(active[Math.min(highlight, active.length - 1)].id)
    } else if (/^[1-9]$/.test(e.key)) {
      const idx = Number(e.key) - 1
      if (idx < active.length) {
        e.preventDefault()
        onSelect(active[idx].id)
      }
    }
  }

  useEffect(() => {
    rowRefs.current[highlight]?.scrollIntoView({ block: 'nearest' })
  }, [highlight])

  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-pine-950/55 p-4 backdrop-blur-[2px]"
      onClick={onClose}
    >
      <div
        ref={panelRef}
        tabIndex={-1}
        onKeyDown={onKeyDown}
        className="w-full max-w-2xl rounded-2xl bg-white p-5 shadow-2xl outline-none"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="font-display text-base font-bold tracking-tight">{medicine.name}</h3>
        <p className="text-xs text-inksoft">
          {medicine.salt_composition} · {medicine.manufacturer} — batches ranked by nearest expiry
        </p>
        <p className="no-print mb-4 mt-2 flex flex-wrap items-center gap-1.5 text-xs text-inksoft">
          <kbd className="keycap">↑</kbd>
          <kbd className="keycap">↓</kbd> navigate
          <span className="text-line">·</span>
          <kbd className="keycap">⏎</kbd> add highlighted
          <span className="text-line">·</span>
          <kbd className="keycap">1–9</kbd> quick add
          <span className="text-line">·</span>
          <kbd className="keycap">esc</kbd> close
        </p>

        <button
          onMouseDown={(e) => {
            e.preventDefault()
            setPeekCost(true)
          }}
          onMouseUp={() => setPeekCost(false)}
          onMouseLeave={() => setPeekCost(false)}
          onTouchStart={(e) => {
            e.preventDefault()
            setPeekCost(true)
          }}
          onTouchEnd={() => setPeekCost(false)}
          className={
            'mb-3 w-full rounded-lg border px-3 py-2 text-xs font-bold uppercase tracking-wider transition-colors select-none ' +
            (peekCost
              ? 'border-marigold bg-marigold-bg text-marigold-text'
              : 'border-line bg-white text-inksoft hover:bg-mint-50')
          }
        >
          {peekCost ? '● Cost visible — release to hide' : '○ Hold to see purchase price (PP)'}
        </button>

        {active.length === 0 ? (
          <p className="py-6 text-center text-sm font-medium text-brick-text">
            No active batches with stock.
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-[11px] uppercase tracking-wider text-inksoft">
                <th className="w-8 py-2" />
                <th className="py-2">Batch</th>
                <th className="py-2">Expiry</th>
                <th className="py-2 text-right">Stock</th>
                {peekCost && <th className="py-2 text-right">PP ₹</th>}
                <th className="py-2 text-right">Rate</th>
                <th />
              </tr>
            </thead>
            <tbody className="divide-y divide-line-soft">
              {active.map((b, i) => {
                const d = daysUntil(b.expiry_date)
                const highlightedRow = i === highlight
                return (
                  <tr
                    key={b.id}
                    ref={(el) => {
                      rowRefs.current[i] = el
                    }}
                    onMouseEnter={() => setHighlight(i)}
                    onClick={() => onSelect(b.id)}
                    className={'cursor-pointer transition-colors ' + (highlightedRow ? 'bg-mint-50' : '')}
                  >
                    <td className="py-2 text-center">
                      {i < 9 && <kbd className="keycap">{i + 1}</kbd>}
                    </td>
                    <td className="py-2 font-mono text-xs">{b.batch_number}</td>
                    <td className="py-2">
                      <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(d)}`}>
                        {b.expiry_date}
                        {d <= 90 && d >= 0 && ` (${d}d)`}
                      </span>
                    </td>
                    <td className="py-2 text-right font-mono tabular-nums">{b.current_stock}</td>
                    {peekCost && (
                      <td className="py-2 text-right font-mono text-xs tabular-nums text-brick-text font-semibold">
                        ₹{money(b.purchase_price)}
                      </td>
                    )}
                    <td className="py-2 text-right font-mono font-semibold tabular-nums">
                      ₹{money(b.sale_price)}
                    </td>
                    <td className="py-2 pl-3 text-right">
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          onSelect(b.id)
                        }}
                        className={
                          'rounded-md px-3 py-1 text-xs font-semibold transition-colors ' +
                          (highlightedRow
                            ? 'bg-pine-700 text-white'
                            : 'border border-line text-inksoft hover:bg-mint-50')
                        }
                      >
                        Add
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
