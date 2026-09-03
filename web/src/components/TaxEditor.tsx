import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { loadCachedHSNCodes, upsertCachedHSNWithRate, upsertCachedMedicineTax } from '../lib/db'
import Modal from './Modal'
import type { HSNWithRate, MedicineTaxConfig } from '../types'

/**
 * Shared HSN + tax editor used across POS (Billing) and Purchases. Reads the
 * store-scoped HSN list from the offline cache (falling back to the network),
 * auto-fills the rate fields when an HSN is chosen, and on save persists via
 * the API + refreshes the local cache. Also supports "create a new HSN".
 */
export default function TaxEditor({
  medicineId,
  medicineName,
  taxConfig,
  onClose,
  onSaved,
}: {
  medicineId: string
  medicineName: string
  taxConfig?: MedicineTaxConfig | null
  onClose: () => void
  onSaved: (cfg: MedicineTaxConfig) => void
}) {
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

  // "Create new HSN" mini-form state
  const [creating, setCreating] = useState(false)
  const [newCode, setNewCode] = useState('')
  const [newDesc, setNewDesc] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      const cached = await loadCachedHSNCodes()
      if (!cancelled) setHsnCodes(cached)
      api.listHSNCodes()
        .then(({ hsn_codes }) => {
          if (cancelled) return
          // The live list carries only code + description. Merge each code with
          // the cached rate metadata so choosing an HSN still auto-fills the
          // rate fields (cache is populated on login / after a sync).
          const byId = new Map<string, HSNWithRate>(cached.map((c) => [c.id, c]))
          const zero = { gst_rate: 0, cgst_rate: 0, sgst_rate: 0, igst_rate: 0, cess_rate: 0 }
          setHsnCodes(hsn_codes.map((h) => byId.get(h.id) ?? { ...h, ...zero }))
        })
        .catch(() => { /* stay on cache */ })
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const chooseHSN = (id: string) => {
    setSelectedHSNId(id)
    const hit = hsnCodes.find((h) => h.id === id)
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
    if (!selectedHSNId) {
      setError('Please select an HSN code.')
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
      const taxRate = await api.upsertTaxRate(selectedHSNId, {
        gst_rate: gst,
        cgst_rate: cgst,
        sgst_rate: sgst,
        igst_rate: igst,
        cess_rate: cess,
      })
      const cfg = await api.upsertMedicineTaxConfig(medicineId, {
        hsn_code_id: selectedHSNId,
        tax_rate_id: taxRate.id,
        price_includes_tax: priceIncl,
      })
      const hsnHit = hsnCodes.find((h) => h.id === selectedHSNId)
      const enriched: MedicineTaxConfig = {
        ...cfg,
        hsn_code: hsnHit?.code ?? '',
        tax_rate: {
          id: taxRate.id,
          hsn_code_id: selectedHSNId,
          gst_rate: gst,
          cgst_rate: cgst,
          sgst_rate: sgst,
          igst_rate: igst,
          cess_rate: cess,
          effective_from: cfg.effective_from,
          effective_to: null,
          created_at: cfg.created_at,
        },
      }
      if (hsnHit) {
        await upsertCachedHSNWithRate({ ...hsnHit, gst_rate: gst, cgst_rate: cgst, sgst_rate: sgst, igst_rate: igst, cess_rate: cess })
      }
      await upsertCachedMedicineTax(enriched)
      onSaved(enriched)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={`Tax — ${medicineName}`} onClose={onClose} wide>
      <div className="space-y-3">
        {!creating ? (
          <div className="space-y-2">
            <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
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
            <button
              onClick={() => setCreating(true)}
              className="rounded-md border border-dashed border-pine-400 px-2.5 py-1 text-xs font-semibold text-pine-700 transition-colors hover:bg-mint-50"
            >
              + Create new HSN
            </button>
          </div>
        ) : (
          <div className="space-y-2 rounded-lg border border-line bg-mint-50 p-3">
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
                <input inputMode="decimal" value={gstRate} onChange={(e) => setGstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                CGST %
                <input inputMode="decimal" value={cgstRate} onChange={(e) => setCgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                SGST %
                <input inputMode="decimal" value={sgstRate} onChange={(e) => setSgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                IGST %
                <input inputMode="decimal" value={igstRate} onChange={(e) => setIgstRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
              </label>
              <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
                Cess %
                <input inputMode="decimal" value={cessRate} onChange={(e) => setCessRate(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
              </label>
            </div>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setCreating(false)}
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
            <input inputMode="decimal" value={gstRate} onChange={(e) => setGstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            CGST %
            <input inputMode="decimal" value={cgstRate} onChange={(e) => setCgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            SGST %
            <input inputMode="decimal" value={sgstRate} onChange={(e) => setSgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            IGST %
            <input inputMode="decimal" value={igstRate} onChange={(e) => setIgstRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            Cess %
            <input inputMode="decimal" value={cessRate} onChange={(e) => setCessRate(e.target.value)}
              className="mt-1 w-full rounded-lg border border-line px-2 py-1.5 text-right font-mono text-sm focus:border-pine-600" />
          </label>
        </div>
        )}

        <label className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-wider text-inksoft">
          <input type="checkbox" checked={priceIncl} onChange={(e) => setPriceIncl(e.target.checked)}
            className="rounded border-line" />
          MRP includes tax (price-inclusive)
        </label>

        {error && (
          <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
        )}

        <div className="flex justify-end gap-2">
          <button onClick={onClose}
            className="rounded-lg border border-line px-3 py-1.5 text-xs font-semibold text-inksoft transition-colors hover:bg-white">
            Cancel
          </button>
          <button onClick={save} disabled={busy || !selectedHSNId}
            className="rounded-lg bg-pine-700 px-3.5 py-1.5 text-xs font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft">
            {busy ? 'Saving…' : 'Save tax config'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
