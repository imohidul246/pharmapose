import { useCallback, useEffect, useState } from 'react'
import { api } from '../lib/api'
import { loadCachedHSNCodes, loadCachedMedicines, upsertCachedHSNWithRate, upsertCachedMedicineTax } from '../lib/db'
import { daysUntil, expiryClass, money } from '../lib/format'
import { searchMedicines, type SearchHit } from '../lib/search'
import Pagination, { usePaged } from '../components/Pagination'
import type { HSNWithRate, MedicineDetail, MedicineTaxConfig, MedicineWithBatches } from '../types'

const UQC_LABELS: Record<string, string> = {
  NOS: 'Numbers', TAB: 'Tablets', BTL: 'Bottles', BOX: 'Boxes',
  KGS: 'Kilograms', GMS: 'Grams', LTR: 'Litres',
}

const PAGE_SIZE = 30

export default function Medicines() {
  const [allMedicines, setAllMedicines] = useState<MedicineWithBatches[]>([])
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<MedicineDetail | null>(null)
  const [detailBusy, setDetailBusy] = useState(false)
  const [detailError, setDetailError] = useState('')
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    void loadCachedMedicines().then((meds) => {
      setAllMedicines(meds)
      setLoaded(true)
    })
  }, [])

  const hits: SearchHit[] = searchMedicines(allMedicines, query, 9999)
  const paged = usePaged(hits, PAGE_SIZE)

  const loadDetail = useCallback(async (id: string) => {
    setDetailBusy(true)
    setDetailError('')
    setSelected(null)
    try {
      const detail = await api.medicineDetail(id)
      setSelected(detail)
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      setDetailBusy(false)
    }
  }, [])

  const selectMedicine = (med: MedicineWithBatches) => {
    void loadDetail(med.id)
  }

  return (
    <div className="space-y-4">
      {/* Search bar */}
      <section className="rounded-xl border border-line bg-white px-4 py-3 shadow-sm">
        <div className="flex items-end gap-3">
          <label className="min-w-[260px] flex-1 text-[10px] font-bold uppercase tracking-wider text-inksoft">
            Search medicines
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Name, salt, manufacturer, packing…"
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            />
          </label>
          <button
            onClick={() => setQuery('')}
            disabled={!query}
            className="h-[38px] rounded-lg border border-line px-4 py-2 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50 disabled:opacity-40"
          >
            Clear
          </button>
          <p className="ml-auto text-xs text-inksoft">
            <span className="font-mono font-semibold">{hits.length}</span>{' '}
            {hits.length === 1 ? 'medicine' : 'medicines'}
            {query && (
              <>
                {' '}matching <span className="font-mono font-semibold">"{query}"</span>
              </>
            )}
          </p>
        </div>
      </section>

      {/* Content: list + detail */}
      <div
        className={
          selected || detailBusy
            ? 'grid gap-5 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]'
            : 'grid gap-5 lg:grid-cols-1'
        }
      >
        {/* Medicine list */}
        <div className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
          <header className="flex items-baseline justify-between border-b border-line-soft px-4 pb-2.5 pt-3">
            <h2 className="font-display text-sm font-bold uppercase tracking-wide">
              Medicine catalogue
            </h2>
            <p className="text-xs text-inksoft">
              Click a row to view full details
            </p>
          </header>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-sm">
              <thead>
                <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                  <th className="px-4 py-2">Medicine</th>
                  <th className="px-3 py-2">Salt / Composition</th>
                  <th className="px-3 py-2">Manufacturer</th>
                  <th className="px-3 py-2">Packing</th>
                  <th className="px-3 py-2 text-right">Stock</th>
                  <th className="px-4 py-2 text-right">MRP</th>
                  <th className="px-4 py-2 text-right">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-soft">
                {!loaded ? (
                  <tr>
                    <td colSpan={7} className="px-4 py-10 text-center text-sm text-inksoft">
                      Loading medicines…
                    </td>
                  </tr>
                ) : paged.slice.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="px-4 py-10 text-center text-sm text-inksoft">
                      {query ? `No medicines match "${query}".` : 'No medicines in inventory.'}
                    </td>
                  </tr>
                ) : (
                  paged.slice.map(({ medicine: m }) => {
                    const totalStock = m.batches.reduce((acc, b) => acc + b.current_stock, 0)
                    const mrp = m.batches.length > 0
                      ? Math.max(...m.batches.map((b) => b.sale_price))
                      : 0

                    return (
                      <tr
                        key={m.id}
                        onClick={() => selectMedicine(m)}
                        className={
                          'cursor-pointer transition-colors ' +
                          (selected?.id === m.id
                            ? 'bg-pine-700/[0.06]'
                            : 'hover:bg-mint-50/60')
                        }
                      >
                        <td className="px-4 py-2.5">
                          <p className="font-medium leading-tight">{m.name}</p>
                          <p className="font-mono text-[11px] text-inksoft/70">
                            {m.batches.length} {m.batches.length === 1 ? 'batch' : 'batches'}
                          </p>
                        </td>
                        <td className="max-w-[180px] truncate px-3 py-2.5 text-xs text-inksoft" title={m.salt_composition}>
                          {m.salt_composition || <span className="italic text-inksoft/50">—</span>}
                        </td>
                        <td className="max-w-[150px] truncate px-3 py-2.5 text-xs text-inksoft" title={m.manufacturer}>
                          {m.manufacturer || <span className="italic text-inksoft/50">—</span>}
                        </td>
                        <td className="px-3 py-2.5 font-mono text-xs text-inksoft">
                          {m.packing || <span className="italic text-inksoft/50">—</span>}
                        </td>
                        <td className="px-3 py-2.5 text-right">
                          <span
                            className={
                              'font-mono text-xs font-semibold tabular-nums ' +
                              (totalStock === 0
                                ? 'text-brick-text'
                                : m.min_reorder_level > 0 && totalStock <= m.min_reorder_level
                                  ? 'text-marigold-text'
                                  : 'text-safe-text')
                            }
                          >
                            {totalStock}
                          </span>
                          {m.min_reorder_level > 0 && totalStock <= m.min_reorder_level && (
                            <span className="ml-1 text-[9px] text-marigold-text" title={`Reorder at ${m.min_reorder_level}`}>
                              ⚠
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-2.5 text-right font-mono text-xs tabular-nums text-inksoft">
                          {mrp > 0 ? `₹${money(mrp)}` : '—'}
                        </td>
                        <td className="px-4 py-2.5 text-right">
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              selectMedicine(m)
                            }}
                            className="rounded-md bg-pine-700 px-2.5 py-1 text-xs font-semibold text-white transition-colors hover:bg-pine-600"
                          >
                            View
                          </button>
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
          <Pagination
            page={paged.page}
            pageCount={paged.pageCount}
            total={paged.total}
            start={paged.start}
            pageSize={PAGE_SIZE}
            pageNumbers
            onPage={paged.setPage}
          />
        </div>

        {/* Detail panel */}
        {(selected || detailBusy || detailError) && (
          <div className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
            {detailBusy && !selected && (
              <div className="animate-pulse p-5">
                <div className="space-y-3">
                  <div className="h-4 w-40 rounded bg-line" />
                  <div className="h-3 w-28 rounded bg-line" />
                  <div className="grid grid-cols-2 gap-3 pt-2">
                    {[0, 1, 2, 3].map((i) => (
                      <div key={i} className="space-y-1.5 rounded-lg bg-mint-50 px-3.5 py-3">
                        <div className="h-2 w-12 rounded bg-line" />
                        <div className="h-3.5 w-16 rounded bg-line" />
                      </div>
                    ))}
                  </div>
                  <p className="pt-2 text-center text-xs text-inksoft">Loading medicine details…</p>
                </div>
              </div>
            )}
            {detailError && (
              <div className="p-5">
                <p className="rounded-lg bg-brick-bg px-3 py-2 text-sm font-medium text-brick-text">
                  {detailError}
                </p>
                <div className="mt-3 flex justify-end gap-2">
                  <button
                    onClick={() => {
                      setDetailError('')
                      if (selected) void loadDetail(selected.id)
                    }}
                    className="rounded-lg bg-pine-700 px-3.5 py-2 text-sm font-semibold text-white transition-colors hover:bg-pine-600"
                  >
                    Retry
                  </button>
                  <button
                    onClick={() => {
                      setSelected(null)
                      setDetailError('')
                    }}
                    className="rounded-lg border border-line px-3.5 py-2 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50"
                  >
                    Close
                  </button>
                </div>
              </div>
            )}
            {selected && <DetailPanel detail={selected} />}
          </div>
        )}
      </div>
    </div>
  )
}

// ---- Detail panel ----

function DetailPanel({ detail: d }: { detail: MedicineDetail }) {
  const activeBatches = d.batches.filter((b) => !b.expired)
  const expiredBatches = d.batches.filter((b) => b.expired)

  return (
    <div className="max-h-[80vh] overflow-y-auto p-5">
      {/* Header */}
      <div className="border-b border-dashed border-line pb-3">
        <h3 className="font-display text-lg font-black tracking-tight">{d.name}</h3>
        <p className="mt-0.5 text-sm text-inksoft">
          {d.salt_composition && <>{d.salt_composition} · </>}
          {d.manufacturer}
        </p>
      </div>

      {/* Meta grid */}
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-lg bg-mint-50 px-3.5 py-3 sm:grid-cols-3">
        <Meta label="Packing" value={d.packing || '—'} />
        <Meta label="UQC" value={d.uqc ? `${d.uqc} - ${UQC_LABELS[d.uqc] || d.uqc}` : 'NOS'} />
        <Meta label="Min reorder" value={d.min_reorder_level > 0 ? String(d.min_reorder_level) : '—'} mono />
        <Meta label="Total stock" value={`${d.total_stock} units`} mono />
        <Meta label="Sales" value={`${d.sales_stats.units_sold} units in ${d.sales_stats.invoices} invoices`} />
        <Meta label="Revenue" value={`₹${money(d.sales_stats.total_revenue)}`} mono />
        <Meta label="Purchases" value={`${d.purchase_stats.units_purchased} units in ${d.purchase_stats.orders} orders`} />
        <Meta label="Spend" value={`₹${money(d.purchase_stats.total_spend)}`} mono />
        <Meta
          label="Created"
          value={new Date(d.created_at).toLocaleDateString([], { dateStyle: 'medium' })}
        />
      </dl>

      {/* Tax configuration */}
      <TaxConfigSection medicineId={d.id} taxConfig={d.tax_config} />

      {/* Batches */}
      {d.batches.length > 0 ? (
        <section className="mt-4">
          <h4 className="mb-2 text-[11px] font-bold uppercase tracking-wider text-inksoft">
            All batches ({activeBatches.length} active, {expiredBatches.length} expired)
          </h4>
          <div className="overflow-hidden rounded-lg border border-line">
            <div className="max-h-[30vh] overflow-y-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-mint-50 shadow-[0_1px_0_var(--color-line)]">
                  <tr className="text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                    <th className="px-3 py-2">Batch #</th>
                    <th className="px-2 py-2">Expiry</th>
                    <th className="px-2 py-2 text-right">Stock</th>
                    <th className="px-2 py-2 text-right">Buy ₹</th>
                    <th className="px-2 py-2 text-right">MRP ₹</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line-soft">
                  {d.batches.map((b) => (
                    <tr key={b.id} className={b.expired ? 'bg-brick-bg/30' : ''}>
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-inksoft">
                        {b.batch_number}
                      </td>
                      <td className="whitespace-nowrap px-2 py-1.5">
                        <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${expiryClass(daysUntil(b.expiry_date))}`}>
                          {b.expiry_date}
                        </span>
                        {b.expired && (
                          <span className="ml-1 text-[10px] font-semibold text-brick-text">expired</span>
                        )}
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono text-xs tabular-nums">
                        <span className={b.current_stock === 0 ? 'text-inksoft/50' : ''}>
                          {b.current_stock}
                        </span>
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono text-xs tabular-nums text-inksoft">
                        ₹{money(b.purchase_price)}
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono text-xs tabular-nums">
                        ₹{money(b.sale_price)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      ) : (
        <p className="mt-4 rounded-lg border border-dashed border-line px-4 py-6 text-center text-sm text-inksoft">
          No batches recorded for this medicine.
        </p>
      )}

      {/* Recent sales */}
      {d.recent_sales.length > 0 && (
        <section className="mt-4">
          <h4 className="mb-2 text-[11px] font-bold uppercase tracking-wider text-inksoft">
            Recent sales (last {d.recent_sales.length})
          </h4>
          <div className="overflow-hidden rounded-lg border border-line">
            <div className="max-h-[25vh] overflow-y-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-mint-50 shadow-[0_1px_0_var(--color-line)]">
                  <tr className="text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                    <th className="px-3 py-2">Invoice</th>
                    <th className="px-2 py-2">Date</th>
                    <th className="px-2 py-2">Customer</th>
                    <th className="px-2 py-2 text-right">Qty</th>
                    <th className="px-2 py-2 text-right">Rate</th>
                    <th className="px-3 py-2 text-right">Amount</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line-soft">
                  {d.recent_sales.map((s, i) => (
                    <tr key={`${s.invoice_id}-${i}`}>
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs font-semibold text-pine-700">
                        #{s.invoice_no}
                      </td>
                      <td className="whitespace-nowrap px-2 py-1.5 font-mono text-xs text-inksoft">
                        {s.created_at}
                      </td>
                      <td className="max-w-[140px] truncate px-2 py-1.5 text-xs" title={s.customer_name}>
                        {s.customer_name || <span className="text-inksoft/60">Walk-in</span>}
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono tabular-nums">{s.quantity}</td>
                      <td className="px-2 py-1.5 text-right font-mono text-xs tabular-nums text-inksoft">
                        ₹{money(s.unit_sale_price)}
                      </td>
                      <td className="px-3 py-1.5 text-right font-mono font-semibold tabular-nums">
                        ₹{money(s.subtotal)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      )}

      {/* Recent purchases */}
      {d.recent_purchases.length > 0 && (
        <section className="mt-4">
          <h4 className="mb-2 text-[11px] font-bold uppercase tracking-wider text-inksoft">
            Recent purchases (last {d.recent_purchases.length})
          </h4>
          <div className="overflow-hidden rounded-lg border border-line">
            <div className="max-h-[25vh] overflow-y-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-mint-50 shadow-[0_1px_0_var(--color-line)]">
                  <tr className="text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                    <th className="px-3 py-2">Invoice</th>
                    <th className="px-2 py-2">Date</th>
                    <th className="px-2 py-2">Supplier</th>
                    <th className="px-2 py-2 text-right">Qty</th>
                    <th className="px-2 py-2 text-right">Bonus</th>
                    <th className="px-3 py-2 text-right">Rate ₹</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line-soft">
                  {d.recent_purchases.map((p, i) => (
                    <tr key={`${p.purchase_id}-${i}`}>
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs font-semibold text-marigold-text">
                        {p.invoice_no}
                      </td>
                      <td className="whitespace-nowrap px-2 py-1.5 font-mono text-xs text-inksoft">
                        {p.created_at}
                      </td>
                      <td className="max-w-[140px] truncate px-2 py-1.5 text-xs" title={p.supplier_name}>
                        {p.supplier_name}
                      </td>
                      <td className="px-2 py-1.5 text-right font-mono tabular-nums">{p.quantity}</td>
                      <td className="px-2 py-1.5 text-right font-mono tabular-nums text-safe-text">
                        {p.bonus_quantity > 0 ? `+${p.bonus_quantity}` : '—'}
                      </td>
                      <td className="px-3 py-1.5 text-right font-mono font-semibold tabular-nums">
                        ₹{money(p.purchase_price)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      )}
    </div>
  )
}

// ---- Tax configuration section ----

function TaxConfigSection({ medicineId, taxConfig }: { medicineId: string; taxConfig?: MedicineTaxConfig | null }) {
  const [editing, setEditing] = useState(false)
  const [hsnCodes, setHsnCodes] = useState<HSNWithRate[]>([])
  const [selectedHSNId, setSelectedHSNId] = useState(taxConfig?.hsn_code_id || '')
  const [gstRate, setGstRate] = useState(String(taxConfig?.tax_rate?.gst_rate ?? ''))
  const [cgstRate, setCgstRate] = useState(String(taxConfig?.tax_rate?.cgst_rate ?? ''))
  const [sgstRate, setSgstRate] = useState(String(taxConfig?.tax_rate?.sgst_rate ?? ''))
  const [igstRate, setIgstRate] = useState(String(taxConfig?.tax_rate?.igst_rate ?? ''))
  const [cessRate, setCessRate] = useState(String(taxConfig?.tax_rate?.cess_rate ?? ''))
  const [priceIncl, setPriceIncl] = useState(taxConfig?.price_includes_tax ?? true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [savedMsg, setSavedMsg] = useState('')
  const [latestTaxConfig, setLatestTaxConfig] = useState<MedicineTaxConfig | null | undefined>(taxConfig)
  const [creating, setCreating] = useState(false)
  const [newCode, setNewCode] = useState('')
  const [newDesc, setNewDesc] = useState('')

  const startEdit = async () => {
    setEditing(true)
    setError('')
    setSavedMsg('')
    try {
      // Prefer the offline cache (store-scoped, populated on login). The cache
      // carries the rate metadata (gst/cgst etc.) that the live list does not,
      // so we initialise the dropdown with it.
      const cached = await loadCachedHSNCodes()
      setHsnCodes(cached)
      // Refresh the HSN list from the network in the background so codes added
      // by another session surface. The live list carries only code +
      // description, so we merge each code with its cached rates to keep the
      // auto-fill working when an HSN is chosen.
      api.listHSNCodes()
        .then(({ hsn_codes }) => {
          const byId = new Map<string, HSNWithRate>(cached.map((c) => [c.id, c]))
          const zero = { gst_rate: 0, cgst_rate: 0, sgst_rate: 0, igst_rate: 0, cess_rate: 0 }
          setHsnCodes(hsn_codes.map((h) => byId.get(h.id) ?? { ...h, ...zero }))
        })
        .catch(() => { /* stay on cache */ })
    } catch {
      /* ignore */
    }
  }

  const chooseHSN = (id: string) => {
    setSelectedHSNId(id)
    const hit = hsnCodes.find((h) => h.id === id) as HSNWithRate | undefined
    if (!hit) return
    if (Number.isFinite(hit.gst_rate)) setGstRate(String(hit.gst_rate))
    if (Number.isFinite(hit.cgst_rate) && hit.cgst_rate > 0) setCgstRate(String(hit.cgst_rate))
    if (Number.isFinite(hit.sgst_rate) && hit.sgst_rate > 0) setSgstRate(String(hit.sgst_rate))
    if (Number.isFinite(hit.igst_rate) && hit.igst_rate > 0) setIgstRate(String(hit.igst_rate))
    if (Number.isFinite(hit.cess_rate) && hit.cess_rate > 0) setCessRate(String(hit.cess_rate))
  }

  const createHSN = async () => {
    const code = newCode.trim()
    if (!code) {
      setError('HSN code is required to create a new entry.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const gst = parseFloat(gstRate) || 0
      const cgst = parseFloat(cgstRate) || gst / 2
      const sgst = parseFloat(sgstRate) || gst / 2
      const igst = parseFloat(igstRate) || gst
      const cess = parseFloat(cessRate) || 0
      const hsn = await api.createHSNCode(code, newDesc.trim())
      await api.upsertTaxRate(hsn.id, {
        gst_rate: gst,
        cgst_rate: cgst,
        sgst_rate: sgst,
        igst_rate: igst,
        cess_rate: cess,
      })
      const withRate: HSNWithRate = {
        id: hsn.id,
        code: hsn.code,
        description: hsn.description,
        created_at: hsn.created_at,
        gst_rate: gst,
        cgst_rate: cgst,
        sgst_rate: sgst,
        igst_rate: igst,
        cess_rate: cess,
      }
      await upsertCachedHSNWithRate(withRate)
      setHsnCodes((prev) => [...prev, withRate])
      setSelectedHSNId(hsn.id)
      if (Number.isFinite(gst)) setGstRate(String(gst))
      if (Number.isFinite(cgst) && cgst > 0) setCgstRate(String(cgst))
      if (Number.isFinite(sgst) && sgst > 0) setSgstRate(String(sgst))
      if (Number.isFinite(igst) && igst > 0) setIgstRate(String(igst))
      if (Number.isFinite(cess) && cess > 0) setCessRate(String(cess))
      setCreating(false)
      setNewCode('')
      setNewDesc('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    setError('')
    try {
      // 1. Find or create HSN code
      let hsnId = selectedHSNId
      if (!hsnId) {
        // Use the first available HSN or create from the GST rate
        setError('Please select an HSN code.')
        setBusy(false)
        return
      }

      // 2. Upsert tax rate for this HSN
      const gst = parseFloat(gstRate) || 0
      const cgst = parseFloat(cgstRate) || gst / 2
      const sgst = parseFloat(sgstRate) || gst / 2
      const igst = parseFloat(igstRate) || gst
      const cess = parseFloat(cessRate) || 0
      const taxRate = await api.upsertTaxRate(hsnId, {
        gst_rate: gst,
        cgst_rate: cgst,
        sgst_rate: sgst,
        igst_rate: igst,
        cess_rate: cess,
      })

      // 3. Assign to medicine
      const cfg = await api.upsertMedicineTaxConfig(medicineId, {
        hsn_code_id: hsnId,
        tax_rate_id: taxRate.id,
        price_includes_tax: priceIncl,
      })

      // Keep the store-scoped offline caches in sync so the HSN dropdown and
      // auto-fill reflect this edit immediately without a full re-sync.
      const hsnHit = hsnCodes.find((h) => h.id === hsnId) as HSNWithRate | undefined
      if (hsnHit) {
        await upsertCachedHSNWithRate({
          ...hsnHit,
          gst_rate: gst,
          cgst_rate: cgst,
          sgst_rate: sgst,
          igst_rate: igst,
          cess_rate: cess,
        })
      }
      if (cfg) await upsertCachedMedicineTax(cfg)

      // Refresh the read-only display locally so it shows the reassigned values
      // immediately, even before the parent re-fetches the medicine detail.
      const enriched: MedicineTaxConfig = {
        ...cfg,
        hsn_code: hsnHit?.code ?? latestTaxConfig?.hsn_code ?? '',
        tax_rate: {
          id: taxRate.id,
          hsn_code_id: hsnId,
          gst_rate: gst,
          cgst_rate: cgst,
          sgst_rate: sgst,
          igst_rate: igst,
          cess_rate: cess,
          effective_from: cfg.effective_from,
          created_at: cfg.created_at,
        },
      }
      setLatestTaxConfig(enriched)

      setEditing(false)
      setSavedMsg(`Saved — HSN ${enriched.hsn_code || hsnId}, GST ${gst}%`)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (!editing) {
    const view = latestTaxConfig ?? taxConfig
    return (
      <section className="mt-4">
        <div className="flex items-center justify-between">
          <h4 className="text-[11px] font-bold uppercase tracking-wider text-inksoft">Tax configuration</h4>
          <button
            onClick={startEdit}
            className="rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
          >
            {view ? 'Edit' : 'Assign HSN & tax'}
          </button>
        </div>
        {savedMsg && (
          <p role="status" className="mt-2 rounded-lg border border-pine-600/30 bg-mint-50 px-3 py-2 text-xs font-semibold text-pine-700">
            {savedMsg}
          </p>
        )}
        {view ? (
          <div className="mt-2 flex flex-wrap gap-3 rounded-lg bg-mint-50 px-3 py-2 text-xs">
            <span>HSN: <span className="font-mono font-semibold">{view.hsn_code}</span></span>
            {view.tax_rate && (
              <>
                <span>GST: <span className="font-mono font-semibold">{view.tax_rate.gst_rate}%</span></span>
                <span>CGST: <span className="font-mono font-semibold">{view.tax_rate.cgst_rate}%</span></span>
                <span>SGST: <span className="font-mono font-semibold">{view.tax_rate.sgst_rate}%</span></span>
                {view.tax_rate.igst_rate > 0 && (
                  <span>IGST: <span className="font-mono font-semibold">{view.tax_rate.igst_rate}%</span></span>
                )}
              </>
            )}
            <span>Price includes tax: <span className="font-mono font-semibold">{view.price_includes_tax ? 'Yes' : 'No'}</span></span>
          </div>
        ) : (
          <p className="mt-2 text-xs text-inksoft">No tax configuration assigned. Tax will not be computed at checkout.</p>
        )}
      </section>
    )
  }

  return (
    <section className="mt-4">
      <h4 className="text-[11px] font-bold uppercase tracking-wider text-inksoft">
        {taxConfig ? 'Edit tax configuration' : 'Assign tax configuration'}
      </h4>
      <div className="mt-2 space-y-2 rounded-lg border border-pine-600/30 bg-mint-50 px-3 py-3">
        <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
          HSN code
          <select
            value={selectedHSNId}
            onChange={(e) => chooseHSN(e.target.value)}
            className="mt-1 w-full rounded-lg border border-line px-2.5 py-2 text-sm focus:border-pine-600"
          >
            <option value="">— Select HSN —</option>
            {hsnCodes.map((h) => (
              <option key={h.id} value={h.id}>
                {h.code} — {h.description || 'No description'}
              </option>
            ))}
          </select>
        </label>

        {!creating ? (
          <button
            onClick={() => setCreating(true)}
            className="rounded-md border border-dashed border-pine-400 px-2.5 py-1 text-xs font-semibold text-pine-700 transition-colors hover:bg-mint-50"
          >
            + Create new HSN
          </button>
        ) : (
          <div className="space-y-2 rounded-lg border border-line bg-white p-3">
            <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
              New HSN code
              <input
                value={newCode}
                onChange={(e) => setNewCode(e.target.value)}
                placeholder="e.g. 3004"
                maxLength={12}
                className="mt-1 w-full rounded-lg border border-line px-2.5 py-2 font-mono text-sm focus:border-pine-600"
              />
            </label>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Description
              <input
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
                placeholder="Optional — what does this HSN cover?"
                className="mt-1 w-full rounded-lg border border-line px-2.5 py-2 text-sm focus:border-pine-600"
              />
            </label>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                GST %
                <input
                  inputMode="decimal"
                  value={gstRate}
                  onChange={(e) => setGstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
                />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                CGST %
                <input
                  inputMode="decimal"
                  value={cgstRate}
                  onChange={(e) => setCgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
                />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                SGST %
                <input
                  inputMode="decimal"
                  value={sgstRate}
                  onChange={(e) => setSgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
                />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                IGST %
                <input
                  inputMode="decimal"
                  value={igstRate}
                  onChange={(e) => setIgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
                />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                Cess %
                <input
                  inputMode="decimal"
                  value={cessRate}
                  onChange={(e) => setCessRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
                />
              </label>
            </div>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => { setCreating(false); setError('') }}
                className="rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-white"
              >
                Back
              </button>
              <button
                onClick={createHSN}
                disabled={busy || !newCode.trim()}
                className="rounded-md bg-pine-700 px-3 py-1 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
              >
                {busy ? 'Creating…' : 'Create & select'}
              </button>
            </div>
          </div>
        )}

        {!creating && (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            GST %
            <input
              inputMode="decimal"
              value={gstRate}
              onChange={(e) => setGstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            CGST %
            <input
              inputMode="decimal"
              value={cgstRate}
              onChange={(e) => setCgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            SGST %
            <input
              inputMode="decimal"
              value={sgstRate}
              onChange={(e) => setSgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            IGST %
            <input
              inputMode="decimal"
              value={igstRate}
              onChange={(e) => setIgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            Cess %
            <input
              inputMode="decimal"
              value={cessRate}
              onChange={(e) => setCessRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600"
            />
          </label>
        </div>
        )}

        <label className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-wider text-inksoft">
          <input
            type="checkbox"
            checked={priceIncl}
            onChange={(e) => setPriceIncl(e.target.checked)}
            className="rounded border-line"
          />
          MRP includes tax (price-inclusive)
        </label>

        {error && (
          <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
        )}

        <div className="flex justify-end gap-2">
          <button
            onClick={() => { setEditing(false); setSavedMsg('') }}
            className="rounded-lg border border-line px-3 py-1.5 text-xs font-semibold text-inksoft transition-colors hover:bg-white"
          >
            Cancel
          </button>
          <button
            onClick={save}
            disabled={busy || !selectedHSNId}
            className="rounded-lg bg-pine-700 px-3.5 py-1.5 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
          >
            {busy ? 'Saving…' : 'Save tax config'}
          </button>
        </div>
      </div>
    </section>
  )
}

function Meta({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] font-bold uppercase tracking-wider text-inksoft">{label}</dt>
      <dd
        className={'truncate font-medium ' + (mono ? 'font-mono text-[13px] tabular-nums ' : '')}
        title={value}
      >
        {value}
      </dd>
    </div>
  )
}
