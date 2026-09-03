import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { loadCachedHSNCodes, loadCachedMedicineTaxConfigs, loadCachedMedicines, upsertCachedHSNWithRate } from '../lib/db'
import { daysUntil, expiryClass, money, todayISO } from '../lib/format'
import { searchMedicines } from '../lib/search'
import TaxEditor from '../components/TaxEditor'
import type {
  DiscountType,
  HSNWithRate,
  MedicineTaxConfig,
  MedicineWithBatches,
  PurchaseLineInput,
  PurchaseOrderInfo,
  PurchaseRequest,
  Supplier,
} from '../types'

interface StagedLine {
  key: string
  kind: 'existing' | 'new'
  medicineId: string
  name: string
  salt: string
  manufacturer: string
  packing: string
  minReorder: number
  hsnCode: string
  priceIncludesTax: boolean
  batchNumber: string
  expiryDate: string
  quantity: number
  bonusQty: number
  purchasePrice: number
  salePrice: number
  discountType: DiscountType
  discountValue: number
}

interface ItemDraft {
  batchNumber: string
  expiryDate: string
  quantity: string
  bonusQty: string
  purchasePrice: string
  salePrice: string
  discountType: DiscountType
  discountValue: string
  hsnCode: string
  priceIncludesTax: boolean
}

const emptyDraft = (): ItemDraft => ({
  batchNumber: '',
  expiryDate: todayISO(365),
  quantity: '',
  bonusQty: '',
  purchasePrice: '',
  salePrice: '',
  discountType: 'percent',
  discountValue: '',
  hsnCode: '',
  priceIncludesTax: true,
})

export default function Purchases({
  cacheVersion,
  onMutated,
  mode = 'record',
}: {
  cacheVersion: number
  onMutated: () => Promise<void>
  mode?: 'record' | 'submit'
}) {
  const { session } = useAuth()
  const isSubmit = mode === 'submit'
  const [medicines, setMedicines] = useState<MedicineWithBatches[]>([])
  const [medTaxCache, setMedTaxCache] = useState<Record<string, MedicineTaxConfig>>({})
  const [hsnCodes, setHsnCodes] = useState<HSNWithRate[]>([])
  const [editTaxLineKey, setEditTaxLineKey] = useState<string | null>(null)
  const [creatingHsn, setCreatingHsn] = useState(false)
  const [newHsnCode, setNewHsnCode] = useState('')
  const [newHsnDesc, setNewHsnDesc] = useState('')
  const [newHsnGst, setNewHsnGst] = useState('')
  const [newHsnCgst, setNewHsnCgst] = useState('')
  const [newHsnSgst, setNewHsnSgst] = useState('')
  const [newHsnIgst, setNewHsnIgst] = useState('')
  const [newHsnCess, setNewHsnCess] = useState('')
  const [modeToggle, setModeToggle] = useState<'existing' | 'new'>('existing')
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const [picked, setPicked] = useState<MedicineWithBatches | null>(null)
  const [newMed, setNewMed] = useState({
    name: '',
    salt: '',
    manufacturer: '',
    packing: '',
    minReorder: '0',
  })
  const [draft, setDraft] = useState<ItemDraft>(emptyDraft())
  const [lines, setLines] = useState<StagedLine[]>([])
  const [supplier, setSupplier] = useState('')
  const [invoiceNo, setInvoiceNo] = useState('')
  const [poDiscount, setPoDiscount] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [lineError, setLineError] = useState('')
  const [result, setResult] = useState<PurchaseOrderInfo | null>(null)
  const [submitted, setSubmitted] = useState<PurchaseRequest | null>(null)
  const [myRequests, setMyRequests] = useState<PurchaseRequest[]>([])
  const [suppliers, setSuppliers] = useState<Supplier[]>([])
  const [selectedSupplierId, setSelectedSupplierId] = useState('')
  const batchRef = useRef<HTMLInputElement>(null)

  const selfId = session?.principal?.id

  const loadMyRequests = useCallback(async () => {
    if (!isSubmit || !selfId) return
    try {
      const all = await api.purchaseRequests()
      setMyRequests(all.requests.filter((r) => r.requested_by === selfId))
    } catch {
      /* non-fatal */
    }
  }, [isSubmit, selfId])

  useEffect(() => {
    void loadMyRequests()
  }, [loadMyRequests])

  useEffect(() => {
    let cancelled = false
    void loadCachedMedicines().then((meds) => {
      if (!cancelled) setMedicines(meds)
    })
    return () => {
      cancelled = true
    }
  }, [cacheVersion])

  useEffect(() => {
    let cancelled = false
    void loadCachedMedicineTaxConfigs().then((cfgs) => {
      if (cancelled) return
      setMedTaxCache(Object.fromEntries(cfgs.map((c) => [c.medicine_id, c])))
    })
    return () => {
      cancelled = true
    }
  }, [cacheVersion])

  useEffect(() => {
    let cancelled = false
    void loadCachedHSNCodes().then(async (hsns) => {
      if (hsns.length > 0) {
        if (!cancelled) setHsnCodes(hsns)
        return
      }
      try {
        const { hsn_codes } = await api.listHSNCodes()
        if (!cancelled) {
          setHsnCodes(hsn_codes.map((h) => ({
            id: h.id,
            code: h.code,
            description: h.description,
            created_at: h.created_at,
            gst_rate: 0,
            cgst_rate: 0,
            sgst_rate: 0,
            igst_rate: 0,
            cess_rate: 0,
          })))
        }
      } catch {
        /* stay empty */
      }
    })
    return () => {
      cancelled = true
    }
  }, [cacheVersion])

  useEffect(() => {
    void api.suppliers().then((s) => setSuppliers(s)).catch(() => {})
  }, [])

  const hits = useMemo(
    () => (picked || modeToggle === 'new' ? [] : searchMedicines(medicines, query, 8)),
    [medicines, query, picked, modeToggle],
  )
  useEffect(() => setHighlight(0), [query, modeToggle])

  const pick = (m: MedicineWithBatches) => {
    setPicked(m)
    setQuery('')
    setError('')
    const fefo =
      m.batches
        .filter((b) => b.current_stock > 0)
        .sort((a, b) => a.expiry_date.localeCompare(b.expiry_date))[0] ?? m.batches[0]
    setDraft((d) => ({
      ...d,
      purchasePrice: d.purchasePrice || (fefo ? String(fefo.purchase_price) : ''),
      salePrice: d.salePrice || (fefo ? String(fefo.sale_price) : ''),
    }))
    requestAnimationFrame(() => batchRef.current?.focus())
  }

  const patchDraft = (patch: Partial<ItemDraft>) =>
    setDraft((d) => ({ ...d, ...patch }))

  const createNewHsn = async () => {
    const code = newHsnCode.trim()
    if (!code || modeToggle !== 'new') return
    setLineError('')
    try {
      const gst = parseFloat(newHsnGst) || 0
      const cgst = parseFloat(newHsnCgst) || gst / 2
      const sgst = parseFloat(newHsnSgst) || gst / 2
      const igst = parseFloat(newHsnIgst) || gst
      const cess = parseFloat(newHsnCess) || 0
      const hsn = await api.createHSNCode(code, newHsnDesc.trim())
      if (gst > 0 || cgst > 0 || sgst > 0 || igst > 0 || cess > 0) {
        await api.upsertTaxRate(hsn.id, {
          gst_rate: gst,
          cgst_rate: cgst,
          sgst_rate: sgst,
          igst_rate: igst,
          cess_rate: cess,
        })
      }
      const cfg: HSNWithRate = {
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
      await upsertCachedHSNWithRate(cfg)
      setHsnCodes((prev) => [...prev, cfg])
      patchDraft({ hsnCode: hsn.code })
      setCreatingHsn(false)
      setNewHsnCode('')
      setNewHsnDesc('')
      setNewHsnGst('')
      setNewHsnCgst('')
      setNewHsnSgst('')
      setNewHsnIgst('')
      setNewHsnCess('')
    } catch (err) {
      setLineError(err instanceof Error ? err.message : String(err))
    }
  }

  const onSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHighlight((h) => Math.min(h + 1, Math.max(hits.length - 1, 0)))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlight((h) => Math.max(h - 1, 0))
    } else if (e.key === 'Enter') {
      const hit = hits[Math.min(highlight, hits.length - 1)]
      if (hit) {
        e.preventDefault()
        pick(hit.medicine)
      }
    } else if (e.key === 'Escape') {
      setQuery('')
      setLineError('')
    }
  }

  const stageLine = () => {
    setLineError('')
    const qty = Number(draft.quantity)
    const bonus = Number(draft.bonusQty) || 0
    const pp = Number(draft.purchasePrice)
    const sp = Number(draft.salePrice)
    const dv = Number(draft.discountValue) || 0

    if (modeToggle === 'existing' && !picked) return setLineError('Pick a medicine from the catalog first.')
    if (modeToggle === 'new' && newMed.name.trim() === '')
      return setLineError('New medicine needs a brand name.')
    if (!draft.batchNumber.trim()) return setLineError('Batch number is required.')
    if (!draft.expiryDate) return setLineError('Expiry date is required.')
    if (daysUntil(draft.expiryDate) < 0) return setLineError('Expiry date is in the past.')
    if (!Number.isInteger(qty) || qty <= 0) return setLineError('Quantity must be a whole number above zero.')
    if (!Number.isInteger(bonus) || bonus < 0) return setLineError('Bonus quantity must be a whole number zero or above.')
    if (Number.isNaN(pp) || pp < 0 || Number.isNaN(sp) || sp < 0)
      return setLineError('Prices must be zero or more.')
    if (dv < 0) return setLineError('Discount must be zero or more.')

    setLines((prev) => [
      ...prev,
      {
        key: `${Date.now()}-${prev.length}`,
        kind: modeToggle,
        medicineId: picked?.id ?? '',
        name: modeToggle === 'existing' ? (picked?.name ?? '') : newMed.name.trim(),
        salt: modeToggle === 'existing' ? (picked?.salt_composition ?? '') : newMed.salt.trim(),
        manufacturer:
          modeToggle === 'existing' ? (picked?.manufacturer ?? '') : newMed.manufacturer.trim(),
        packing: modeToggle === 'existing' ? (picked?.packing ?? '') : newMed.packing.trim(),
        minReorder: modeToggle === 'new' ? Number(newMed.minReorder) || 0 : 0,
        hsnCode:
          modeToggle === 'new'
            ? draft.hsnCode.trim()
            : picked?.id
              ? (medTaxCache[picked.id]?.hsn_code ?? '')
              : '',
        priceIncludesTax: draft.priceIncludesTax,
        batchNumber: draft.batchNumber.trim(),
        expiryDate: draft.expiryDate,
        quantity: qty,
        bonusQty: bonus,
        purchasePrice: Math.round(pp * 100) / 100,
        salePrice: Math.round(sp * 100) / 100,
        discountType: dv > 0 ? draft.discountType : 'percent',
        discountValue: Math.round(dv * 100) / 100,
      },
    ])
    setPicked(null)
    setQuery('')
    setNewMed({ name: '', salt: '', manufacturer: '', packing: '', minReorder: '0' })
    setDraft(emptyDraft())
  }

  const removeLine = (key: string) =>
    setLines((prev) => prev.filter((l) => l.key !== key))

  const lineDiscountAmount = (l: StagedLine): number => {
    const gross = l.quantity * l.purchasePrice
    if (l.discountValue <= 0 || gross <= 0) return 0
    const raw = l.discountType === 'percent' ? (gross * l.discountValue) / 100 : l.discountValue
    return Math.min(Math.max(raw, 0), gross)
  }

  const totalGross = lines.reduce((acc, l) => acc + l.quantity * l.purchasePrice, 0)
  const totalLineDiscount = lines.reduce((acc, l) => acc + lineDiscountAmount(l), 0)
  const poDiscountNum = Number(poDiscount) || 0
  const total = Math.max(0, totalGross - totalLineDiscount - poDiscountNum)

  const submit = async () => {
    if (busy) return
    if (!supplier.trim()) {
      setError('Supplier name is required.')
      return
    }
    if (lines.length === 0) {
      setError('Add at least one item to the invoice.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const items: PurchaseLineInput[] = lines.map((l) =>
        l.kind === 'existing'
          ? {
              medicine_id: l.medicineId,
              batch_number: l.batchNumber,
              expiry_date: l.expiryDate,
              quantity: l.quantity,
              bonus_quantity: l.bonusQty,
              purchase_price: l.purchasePrice,
              sale_price: l.salePrice,
              discount_type: l.discountValue > 0 ? l.discountType : 'NONE',
              discount_value: l.discountValue,
            }
          : {
              medicine_name: l.name,
              salt_composition: l.salt,
              manufacturer: l.manufacturer,
              packing: l.packing,
              min_reorder_level: l.minReorder,
              hsn_code: l.hsnCode || undefined,
              price_includes_tax: l.priceIncludesTax || undefined,
              batch_number: l.batchNumber,
              expiry_date: l.expiryDate,
              quantity: l.quantity,
              bonus_quantity: l.bonusQty,
              purchase_price: l.purchasePrice,
              sale_price: l.salePrice,
              discount_type: l.discountValue > 0 ? l.discountType : 'NONE',
              discount_value: l.discountValue,
            },
      )
      const selSupplier = selectedSupplierId ? suppliers.find((s) => s.id === selectedSupplierId) : null
      const payload = {
        supplier_name: supplier.trim(),
        supplier_id: selSupplier?.id || undefined,
        supplier_gstin: selSupplier?.gstin || undefined,
        supplier_state: selSupplier?.state_code || undefined,
        invoice_no: invoiceNo.trim() || undefined,
        discount_total: poDiscountNum,
        items,
      }
      if (isSubmit) {
        const res = await api.createPurchaseRequest(payload)
        setSubmitted(res.request)
        setResult(null)
        await loadMyRequests()
      } else {
        const res = await api.createPurchase(payload)
        setResult(res.purchase_order)
        setSubmitted(null)
        await onMutated()
      }
      setLines([])
      setSupplier('')
      setInvoiceNo('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const cancelRequest = async (id: string) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api.cancelPurchaseRequest(id)
      await loadMyRequests()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
      {/* Add-item column */}
      <section className="space-y-4">
        <div className="rounded-xl border border-line bg-white p-4 shadow-sm">
          <h2 className="font-display text-lg font-bold tracking-tight">
            {isSubmit ? 'Propose a Purchase Inward' : 'Record Purchase Inward'}
          </h2>
          <p className="text-xs text-inksoft">
            {isSubmit
              ? 'Build the supplier bill once and submit it — your owner approves it, then stock lands.'
              : 'Enter the supplier bill once — every line stocks its batch and the inventory updates immediately.'}
          </p>

          <div className="mt-3 flex overflow-hidden rounded-lg border border-line text-sm font-semibold">
            {(['existing', 'new'] as const).map((m) => (
              <button
                key={m}
                onClick={() => {
                  setModeToggle(m)
                  setLineError('')
                  setPicked(null)
                  setQuery('')
                }}
                className={
                  'flex-1 px-3 py-1.5 transition-colors ' +
                  (modeToggle === m ? 'bg-pine-700 text-white' : 'bg-white text-inksoft hover:bg-mint-50')
                }
              >
                {m === 'existing' ? 'Catalog medicine' : 'New medicine'}
              </button>
            ))}
          </div>

          {modeToggle === 'existing' ? (
            <div className="mt-3">
              {picked ? (
                <div className="flex items-center justify-between gap-3 rounded-lg border border-pine-600/50 bg-mint-50 px-3 py-2">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold">{picked.name}</p>
                    <p className="truncate text-xs text-inksoft">{picked.salt_composition}</p>
                  </div>
                  <span className="shrink-0 font-mono text-xs text-inksoft">
                    {picked.batches.reduce((a, b) => a + b.current_stock, 0)} in stock
                  </span>
                  <button
                    onClick={() => setPicked(null)}
                    className="shrink-0 rounded-md border border-line bg-white px-2 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-white"
                  >
                    Change
                  </button>
                </div>
              ) : (
                <>
                  <input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={onSearchKeyDown}
                    placeholder="Search catalog by brand / salt / maker…"
                    className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                  />
                  <p className="mt-1 flex items-center gap-1.5 px-0.5 text-[11px] text-inksoft">
                    <kbd className="keycap">↑</kbd>
                    <kbd className="keycap">↓</kbd> navigate
                    <span className="text-line">·</span>
                    <kbd className="keycap">⏎</kbd> select
                    <span className="text-line">·</span>
                    <kbd className="keycap">esc</kbd> clear
                  </p>
                  {hits.length > 0 && (
                    <ul className="mt-1 divide-y divide-line-soft overflow-hidden rounded-lg border border-line">
                      {hits.map((h, i) => (
                        <li
                          key={h.medicine.id}
                          onMouseEnter={() => setHighlight(i)}
                          onClick={() => pick(h.medicine)}
                          className={
                            'flex cursor-pointer items-center justify-between gap-3 px-3 py-2 transition-colors ' +
                            (i === highlight ? 'bg-mint-50 shadow-[inset_3px_0_0_var(--color-pine-600)]' : '')
                          }
                        >
                          <div className="min-w-0">
                            <p className={'truncate text-sm ' + (i === highlight ? 'font-semibold' : 'font-medium')}>
                              {h.medicine.name}
                            </p>
                            <p className="truncate text-xs text-inksoft">
                              {h.medicine.salt_composition}
                            </p>
                          </div>
                          <span className="shrink-0 font-mono text-xs text-inksoft">
                            {h.medicine.batches.reduce((a, b) => a + b.current_stock, 0)} in stock
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </>
              )}
            </div>
          ) : (
            <div className="mt-3 grid grid-cols-2 gap-2">
              <input
                value={newMed.name}
                onChange={(e) => setNewMed((m) => ({ ...m, name: e.target.value }))}
                placeholder="Brand name *"
                className="col-span-2 rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
              />
              <input
                value={newMed.salt}
                onChange={(e) => setNewMed((m) => ({ ...m, salt: e.target.value }))}
                placeholder="Salt composition"
                className="col-span-2 rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
              />
              <input
                value={newMed.manufacturer}
                onChange={(e) => setNewMed((m) => ({ ...m, manufacturer: e.target.value }))}
                placeholder="Manufacturer"
                className="rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
              />
              <input
                value={newMed.packing}
                onChange={(e) => setNewMed((m) => ({ ...m, packing: e.target.value }))}
                placeholder="Packing (e.g. Strip of 10)"
                className="rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
              />
              <label className="col-span-2 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                Min reorder level
                <input
                  inputMode="numeric"
                  value={newMed.minReorder}
                  onChange={(e) => /^\d*$/.test(e.target.value) && setNewMed((m) => ({ ...m, minReorder: e.target.value }))}
                  className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-right font-mono text-sm tabular-nums text-ink focus:border-pine-600"
                />
              </label>
              <label className="col-span-2 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                HSN code
                <select
                  value={draft.hsnCode}
                  onChange={(e) => patchDraft({ hsnCode: e.target.value })}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-2 font-mono text-sm focus:border-pine-600"
                >
                  <option value="">— Select an HSN —</option>
                  {hsnCodes.map((h) => (
                    <option key={h.id} value={h.code}>
                      {h.code} — {h.description || 'No description'}
                    </option>
                  ))}
                </select>
                {!creatingHsn ? (
                  <button
                    type="button"
                    onClick={() => setCreatingHsn(true)}
                    className="mt-1 rounded-md border border-dashed border-pine-400 px-2.5 py-1 text-xs font-semibold text-pine-700 transition-colors hover:bg-mint-50"
                  >
                    + Create new HSN
                  </button>
                ) : (
                  <div className="mt-1 space-y-2 rounded-lg border border-line bg-white p-2.5">
                    <input
                      value={newHsnCode}
                      onChange={(e) => setNewHsnCode(e.target.value)}
                      placeholder="New HSN code e.g. 3004"
                      maxLength={12}
                      className="w-full rounded-lg border border-line px-2.5 py-1.5 font-mono text-sm focus:border-pine-600"
                    />
                    <input
                      value={newHsnDesc}
                      onChange={(e) => setNewHsnDesc(e.target.value)}
                      placeholder="Description (optional)"
                      className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                    />
                    <div className="grid grid-cols-2 gap-2">
                      <label className="flex flex-col gap-0.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-inksoft">GST %</span>
                        <input
                          type="number"
                          min={0}
                          step="0.01"
                          value={newHsnGst}
                          onChange={(e) => setNewHsnGst(e.target.value)}
                          placeholder="e.g. 18"
                          className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                        />
                      </label>
                      <label className="flex flex-col gap-0.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-inksoft">Cess %</span>
                        <input
                          type="number"
                          min={0}
                          step="0.01"
                          value={newHsnCess}
                          onChange={(e) => setNewHsnCess(e.target.value)}
                          placeholder="optional"
                          className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                        />
                      </label>
                      <label className="flex flex-col gap-0.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-inksoft">CGST %</span>
                        <input
                          type="number"
                          min={0}
                          step="0.01"
                          value={newHsnCgst}
                          onChange={(e) => setNewHsnCgst(e.target.value)}
                          placeholder="auto = GST/2"
                          className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                        />
                      </label>
                      <label className="flex flex-col gap-0.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-inksoft">SGST %</span>
                        <input
                          type="number"
                          min={0}
                          step="0.01"
                          value={newHsnSgst}
                          onChange={(e) => setNewHsnSgst(e.target.value)}
                          placeholder="auto = GST/2"
                          className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                        />
                      </label>
                      <label className="flex flex-col gap-0.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-inksoft">IGST %</span>
                        <input
                          type="number"
                          min={0}
                          step="0.01"
                          value={newHsnIgst}
                          onChange={(e) => setNewHsnIgst(e.target.value)}
                          placeholder="auto = GST"
                          className="w-full rounded-lg border border-line px-2.5 py-1.5 text-sm focus:border-pine-600"
                        />
                      </label>
                    </div>
                    <div className="flex justify-end gap-2">
                      <button
                        type="button"
                        onClick={() => { setCreatingHsn(false); setNewHsnCode(''); setNewHsnDesc(''); setNewHsnGst(''); setNewHsnCgst(''); setNewHsnSgst(''); setNewHsnIgst(''); setNewHsnCess(''); setLineError('') }}
                        className="rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-white"
                      >
                        Back
                      </button>
                      <button
                        type="button"
                        onClick={createNewHsn}
                        disabled={!newHsnCode.trim()}
                        className="rounded-md bg-pine-700 px-3 py-1 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
                      >
                        Create & select
                      </button>
                    </div>
                  </div>
                )}
              </label>
              <label className="col-span-2 flex items-center gap-2 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                <input
                  type="checkbox"
                  checked={draft.priceIncludesTax}
                  onChange={(e) => patchDraft({ priceIncludesTax: e.target.checked })}
                  className="rounded border-line"
                />
                MRP includes tax
              </label>
              <p className="col-span-2 rounded-lg bg-marigold-bg px-3 py-2 text-xs font-medium text-marigold-text">
                This medicine is not in your catalog yet — it will be registered together with its
                first stock entry.
              </p>
            </div>
          )}

          {/* Batch details */}
          <div className="mt-3 grid grid-cols-3 gap-2 sm:grid-cols-6">
            <label className="col-span-3 text-[10px] font-bold uppercase tracking-wider text-inksoft sm:col-span-1">
              Batch no.
              <input
                ref={batchRef}
                value={draft.batchNumber}
                onChange={(e) => patchDraft({ batchNumber: e.target.value })}
                placeholder="e.g. AB1234"
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 font-mono text-sm focus:border-pine-600"
              />
            </label>
            <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Expiry
              <input
                type="date"
                min={todayISO()}
                value={draft.expiryDate}
                onChange={(e) => patchDraft({ expiryDate: e.target.value })}
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 font-mono text-sm focus:border-pine-600"
              />
            </label>
            <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Qty
              <input
                inputMode="numeric"
                value={draft.quantity}
                onChange={(e) => /^\d*$/.test(e.target.value) && patchDraft({ quantity: e.target.value })}
                placeholder="0"
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 text-right font-mono text-sm tabular-nums focus:border-pine-600"
              />
            </label>
            <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Free
              <input
                inputMode="numeric"
                value={draft.bonusQty}
                onChange={(e) => /^\d*$/.test(e.target.value) && patchDraft({ bonusQty: e.target.value })}
                placeholder="0"
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 text-right font-mono text-sm tabular-nums focus:border-pine-600"
              />
            </label>
            <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              Buy ₹
              <input
                inputMode="decimal"
                value={draft.purchasePrice}
                onChange={(e) => /^\d*\.?\d{0,2}$/.test(e.target.value) && patchDraft({ purchasePrice: e.target.value })}
                placeholder="0.00"
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 text-right font-mono text-sm tabular-nums focus:border-pine-600"
              />
            </label>
            <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
              MRP ₹
              <input
                inputMode="decimal"
                value={draft.salePrice}
                onChange={(e) => /^\d*\.?\d{0,2}$/.test(e.target.value) && patchDraft({ salePrice: e.target.value })}
                placeholder="0.00"
                className="mt-1 w-full rounded-lg border border-line px-2 py-2 text-right font-mono text-sm tabular-nums focus:border-pine-600"
              />
            </label>
          </div>

          <div className="mt-2 flex items-center gap-2">
            <span className="text-[10px] font-bold uppercase tracking-wider text-inksoft">Disc</span>
            <input
              inputMode="decimal"
              value={draft.discountValue}
              onChange={(e) => {
                const v = e.target.value
                if (!/^\d*\.?\d{0,2}$/.test(v)) return
                patchDraft({ discountValue: v })
              }}
              placeholder="0"
              className="w-20 rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm tabular-nums focus:border-pine-600"
            />
            <div className="flex overflow-hidden rounded-lg border border-line">
              {(['percent', 'amount'] as DiscountType[]).map((t) => (
                <button
                  key={t}
                  onClick={() => patchDraft({ discountType: t })}
                  className={
                    'px-2.5 py-1.5 font-mono text-xs font-semibold transition-colors ' +
                    (draft.discountType === t
                      ? 'bg-pine-700 text-white'
                      : 'bg-white hover:bg-mint-50')
                  }
                >
                  {t === 'percent' ? '%' : '₹'}
                </button>
              ))}
            </div>
            {Number(draft.discountValue) > 0 && (
              <span className="rounded bg-safe-bg px-1.5 py-0.5 font-mono text-xs font-semibold text-safe-text">
                {draft.discountType === 'percent'
                  ? `${draft.discountValue}% off`
                  : `-₹${draft.discountValue}`}
              </span>
            )}
          </div>

          {lineError && (
            <p className="mt-2 rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">
              {lineError}
            </p>
          )}

          <button
            onClick={stageLine}
            className="mt-3 w-full rounded-lg bg-pine-700 px-4 py-2.5 text-sm font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
            disabled={
              busy ||
              (modeToggle === 'existing' ? !picked : !newMed.name.trim()) ||
              !draft.batchNumber.trim() ||
              !draft.quantity ||
              draft.purchasePrice === ''
            }
          >
            + Add to invoice
          </button>
        </div>
      </section>

      {/* Invoice column */}
      <section className="overflow-hidden rounded-xl border border-line bg-white shadow-md shadow-pine-950/[0.04]">
        <header className="space-y-2 px-4 pb-3 pt-3.5">
          <div className="flex items-baseline justify-between">
            <h2 className="font-display text-sm font-bold uppercase tracking-wide">Supplier invoice</h2>
            <span className="font-mono text-xs text-inksoft">
              {lines.length} {lines.length === 1 ? 'item' : 'items'}
            </span>
          </div>
          <input
            value={supplier}
            onChange={(e) => setSupplier(e.target.value)}
            placeholder="Supplier / distributor name *"
            className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
          {suppliers.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-[10px] font-bold uppercase tracking-wider text-inksoft whitespace-nowrap">or pick</span>
              <select
                value={selectedSupplierId}
                onChange={(e) => {
                  const s = suppliers.find((x) => x.id === e.target.value)
                  setSelectedSupplierId(e.target.value)
                  if (s) {
                    setSupplier(s.legal_name)
                  }
                }}
                className="flex-1 rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
              >
                <option value="">— Select supplier —</option>
                {suppliers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.legal_name}{s.gstin ? ` (${s.gstin})` : ''}
                  </option>
                ))}
              </select>
            </div>
          )}
          {selectedSupplierId && (() => {
            const sel = suppliers.find((s) => s.id === selectedSupplierId)
            if (!sel) return null
            return (
              <div className="flex flex-wrap gap-3 rounded-lg bg-mint-50 px-3 py-2 text-xs text-inksoft">
                {sel.gstin && <span>GSTIN: <span className="font-mono font-semibold">{sel.gstin}</span></span>}
                {sel.state_code && <span>State: <span className="font-mono font-semibold">{sel.state_code} — {sel.state}</span></span>}
              </div>
            )
          })()}
          <input
            value={invoiceNo}
            onChange={(e) => setInvoiceNo(e.target.value)}
            placeholder="Invoice no. (optional — auto-generated)"
            className="w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </header>

        {lines.length === 0 ? (
          <div className="mx-4 mb-4 rounded-lg border border-dashed border-line px-4 py-12 text-center">
            <p className="text-sm font-medium text-inksoft">No items staged.</p>
            <p className="mt-1 text-xs text-inksoft/70">
              Build the invoice on the left, then record it here.
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-dashed divide-line">
            {lines.map((l) => {
              const gross = l.quantity * l.purchasePrice
              const disc = lineDiscountAmount(l)
              const net = gross - disc
              return (
                <li key={l.key} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-sm">
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-semibold">
                      {l.name}{' '}
                      {l.kind === 'new' && (
                        <span className="ml-1 rounded bg-marigold-bg px-1.5 py-0.5 align-middle text-[10px] font-bold uppercase tracking-wider text-marigold-text">
                          New
                        </span>
                      )}
                    </p>
                    <p className="mt-0.5 truncate font-mono text-[11px] text-inksoft">
                      Batch {l.batchNumber} ·{' '}
                      <span className={`rounded px-1 ${expiryClass(daysUntil(l.expiryDate))}`}>
                        exp {l.expiryDate}
                      </span>{' '}
                      · {l.quantity}{l.bonusQty > 0 ? `+${l.bonusQty} free` : ''} × ₹{money(l.purchasePrice)}
                      {l.hsnCode && <> · HSN {l.hsnCode}</>}
                      {l.kind === 'existing' && medTaxCache[l.medicineId]?.tax_rate?.gst_rate != null && (
                        <> · GST {medTaxCache[l.medicineId]!.tax_rate!.gst_rate}%</>
                      )}
                    </p>
                  </div>
                  {disc > 0 && (
                    <span className="rounded bg-safe-bg px-1.5 py-0.5 font-mono text-[11px] font-semibold text-safe-text">
                      −₹{money(disc)}
                    </span>
                  )}
                  <span className={
                    'w-20 text-right font-mono font-semibold tabular-nums ' +
                    (disc > 0 ? 'text-safe-text' : '')
                  }>
                    ₹{money(net)}
                  </span>
                  {l.kind === 'existing' && (
                    <button
                      onClick={() => setEditTaxLineKey(l.key)}
                      title="Edit tax configuration"
                      className="rounded-md border border-pine-200 px-2 py-1 text-[11px] font-bold text-pine-700 transition-colors hover:bg-mint-50"
                    >
                      Edit tax
                    </button>
                  )}
                  <button
                    onClick={() => removeLine(l.key)}
                    aria-label={`Remove ${l.name}`}
                    className="rounded-md p-1 text-inksoft/60 transition-colors hover:bg-brick-bg hover:text-brick-text"
                  >
                    <svg viewBox="0 0 14 14" className="h-3.5 w-3.5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round">
                      <path d="M2 2l10 10M12 2L2 12" />
                    </svg>
                  </button>
                </li>
              )
            })}
          </ul>
        )}

        <footer className="space-y-3 border-t border-dashed border-line bg-mint-50/70 px-4 py-4">
          <div className="space-y-1.5 text-sm">
            {totalLineDiscount > 0 && (
              <div className="flex items-center justify-between text-inksoft">
                <span className="text-xs font-semibold uppercase tracking-wider">Line discounts</span>
                <span className="font-mono font-semibold text-brick-text tabular-nums">−₹{money(totalLineDiscount)}</span>
              </div>
            )}
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold uppercase tracking-wider text-inksoft">PO Discount ₹</span>
              <input
                inputMode="decimal"
                value={poDiscount}
                onChange={(e) => {
                  const v = e.target.value
                  if (/^\d*\.?\d{0,2}$/.test(v)) setPoDiscount(v)
                }}
                placeholder="0"
                className="w-24 rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm tabular-nums focus:border-pine-600"
              />
            </div>
          </div>
          <div className="flex items-end justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-inksoft">
              Purchase total
            </span>
            <span className="font-display text-2xl font-black tracking-tight tabular-nums">
              ₹{money(total)}
            </span>
          </div>
          {error && (
            <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">
              {error}
            </p>
          )}
          <button
            onClick={() => void submit()}
            disabled={busy || lines.length === 0 || !supplier.trim()}
            className="h-12 w-full rounded-xl bg-pine-700 font-display text-[15px] font-bold tracking-tight text-white shadow-sm transition-colors hover:bg-pine-600 active:bg-pine-800 disabled:bg-line disabled:text-inksoft disabled:shadow-none"
          >
            {busy
              ? isSubmit
                ? 'Submitting…'
                : 'Posting inward…'
              : isSubmit
                ? `Submit for approval — ₹${money(total)}`
                : `Record Purchase — ₹${money(total)}`}
          </button>
          <p className="text-center text-[11px] text-inksoft/70">
            {isSubmit
              ? 'Nothing is stocked yet — your owner approves this first.'
              : 'Recording updates batch stock instantly — billing picks it up after sync.'}
          </p>
        </footer>
      </section>

      {result && !isSubmit && (
        <div className="flex items-center gap-4 rounded-xl border border-dashed border-pine-600/60 bg-white p-3.5 shadow-sm lg:col-span-2">
          <span aria-hidden className="stamp shrink-0 px-2.5 py-1 text-[11px]">
            Stocked
          </span>
          <p className="min-w-0 flex-1 text-sm leading-snug">
            Purchase <span className="font-mono font-semibold">{result.invoice_no}</span> from{' '}
            {result.supplier_name} recorded ·{' '}
            <span className="font-mono font-semibold">₹{money(result.grand_total ?? result.total_amount)}</span> ·
            inventory updated.
          </p>
          <button
            onClick={() => setResult(null)}
            className="shrink-0 rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
          >
            Dismiss
          </button>
        </div>
      )}

      {submitted && isSubmit && (
        <div className="flex items-center gap-4 rounded-xl border border-dashed border-marigold-dot/70 bg-white p-3.5 shadow-sm lg:col-span-2">
          <span
            aria-hidden
            className="rounded-full bg-marigold-bg px-2.5 py-1 font-display text-[11px] font-black uppercase tracking-widest text-marigold-text"
          >
            Awaiting
          </span>
          <p className="min-w-0 flex-1 text-sm leading-snug">
            Purchase submitted for approval. Your owner reviews it on the counter — stock is added
            only after sign-off.
          </p>
          <button
            onClick={() => setSubmitted(null)}
            className="shrink-0 rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
          >
            Dismiss
          </button>
        </div>
      )}

      {isSubmit && myRequests.length > 0 && (
        <section className="lg:col-span-2">
          <h3 className="mb-2 font-display text-sm font-bold uppercase tracking-wide text-inksoft">
            Your submissions
          </h3>
          <ul className="overflow-hidden rounded-xl border border-line bg-white shadow-sm">
            {myRequests.map((r) => (
              <li
                key={r.id}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line-soft px-4 py-2.5 text-sm last:border-b-0"
              >
                <span className="min-w-0 flex-1">
                  <span className="font-semibold">{r.status === 'PENDING' ? 'Purchase proposal' : `Purchase ${r.status.toLowerCase()}`}</span>
                  <span className="ml-2 text-xs text-inksoft">
                    {new Date(r.created_at).toLocaleString('en-IN', {
                      day: 'numeric',
                      month: 'short',
                      hour: '2-digit',
                      minute: '2-digit',
                    })}
                  </span>
                  {r.status === 'REJECTED' && r.rejection_reason && (
                    <span className="ml-2 rounded bg-brick-bg px-1.5 py-0.5 text-[11px] font-semibold text-brick-text">
                      {r.rejection_reason}
                    </span>
                  )}
                  {r.status === 'APPROVED' && r.purchase_id && (
                    <span className="ml-2 rounded bg-safe-bg px-1.5 py-0.5 text-[11px] font-semibold text-safe-text">
                      stocked
                    </span>
                  )}
                </span>
                <span
                  className={
                    'rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider ' +
                    (r.status === 'PENDING'
                      ? 'bg-marigold-bg text-marigold-text'
                      : r.status === 'APPROVED'
                        ? 'bg-safe-bg text-safe-text'
                        : r.status === 'REJECTED'
                          ? 'bg-brick-bg text-brick-text'
                          : 'bg-line/70 text-inksoft')
                  }
                >
                  {r.status}
                </span>
                {r.status === 'PENDING' && (
                  <button
                    onClick={() => void cancelRequest(r.id)}
                    className="rounded-md border border-line px-2 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-brick-bg hover:text-brick-text"
                  >
                    Cancel
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      {editTaxLineKey && (() => {
        const line = lines.find((l) => l.key === editTaxLineKey)
        if (!line || line.kind !== 'existing') {
          setEditTaxLineKey(null)
          return null
        }
        return (
          <TaxEditor
            medicineId={line.medicineId}
            medicineName={line.name}
            taxConfig={medTaxCache[line.medicineId] ?? null}
            onClose={() => setEditTaxLineKey(null)}
            onSaved={(cfg) => {
              setMedTaxCache((prev) => ({ ...prev, [cfg.medicine_id]: cfg }))
              setEditTaxLineKey(null)
            }}
          />
        )
      })()}
    </div>
  )
}
