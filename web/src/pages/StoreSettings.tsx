import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { stateCodeToName } from '../lib/states'
import type { Store } from '../types'

export default function StoreSettings() {
  const [store, setStore] = useState<Store | null>(null)
  const [staffCount, setStaffCount] = useState(0)
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [seats, setSeats] = useState('')
  const [ownerName, setOwnerName] = useState('')
  const [phone, setPhone] = useState('')
  const [gstin, setGstin] = useState('')
  const [dlNumber, setDlNumber] = useState('')
  const [dlExpiry, setDlExpiry] = useState('')
  const [pan, setPan] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  useEffect(() => {
    void (async () => {
      const [s, m] = await Promise.all([api.store(), api.employees()])
      setStore(s.store)
      setName(s.store.name)
      setAddress(s.store.address)
      setOwnerName(s.store.owner_name || '')
      setPhone(s.store.phone || '')
      setGstin(s.store.gstin || '')
      setDlNumber(s.store.drug_license_number || '')
      setDlExpiry(s.store.drug_license_expiry || '')
      setPan(s.store.pan || '')
      setSeats(String(s.store.max_employees))
      setStaffCount(m.members.filter((x) => x.role === 'EMPLOYEE' && x.user_active).length)
    })().catch(() => setError('Could not load store settings.'))
  }, [])

  const save = async () => {
    if (busy || !name.trim() || !ownerName.trim() || !phone.trim() || !address.trim() || !seats) return
    setBusy(true)
    setError('')
    setDone(false)
    try {
      const max = Number(seats) || 0
      const res = await api.updateStore({
        name: name.trim(),
        address: address.trim(),
        phone: phone.trim(),
        owner_name: ownerName.trim(),
        max_employees: max,
        gstin: gstin.trim() || null,
        pan: pan.trim() || null,
        drug_license_number: dlNumber.trim() || undefined,
        drug_license_expiry: dlExpiry.trim() || null,
      })
      setStore(res.store)
      setDone(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (!store) {
    return <p className="py-16 text-center text-sm text-inksoft">Loading store settings…</p>
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5">
      <header>
        <h2 className="font-display text-lg font-bold tracking-tight">Store settings</h2>
        <p className="text-xs text-inksoft">
          Owner-only territory — the shop's details, compliance, and how many staff seats are on the counter.
        </p>
      </header>

      {error && (
        <p role="alert" className="rounded-xl border-l-4 border-brick bg-brick-bg px-4 py-3 text-sm font-medium text-brick-text">
          {error}
        </p>
      )}
      {done && (
        <p className="rounded-xl border-l-4 border-pine-600 bg-safe-bg px-4 py-3 text-sm font-medium text-safe-text">
          Settings saved.
        </p>
      )}

      <section className="space-y-3 rounded-xl border border-line bg-white p-5 shadow-sm">
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Store name *
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Owner name *
          <input
            value={ownerName}
            onChange={(e) => setOwnerName(e.target.value)}
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Phone *
          <input
            inputMode="numeric"
            value={phone}
            onChange={(e) => /^\d{0,12}$/.test(e.target.value) && setPhone(e.target.value)}
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Store address *
          <textarea
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            rows={2}
            className="mt-1 w-full resize-none rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
        </label>
        <div className="rounded-lg border border-line bg-cream/50 px-3 py-2">
          <span className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">State</span>
          <p className="mt-1 text-sm">
            {store.state_code
              ? `${stateCodeToName(store.state_code) ?? store.state_code} (${store.state_code})`
              : 'Not set'}
          </p>
          <p className="mt-0.5 text-xs text-inksoft">
            Taken from the store's GSTIN. It decides intra-state vs inter-state GST at billing — add or
            update the GSTIN to change it.
          </p>
        </div>

        <h3 className="pt-1 text-[10px] font-bold uppercase tracking-wider text-inksoft">Compliance (optional)</h3>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          GSTIN <span className="font-normal normal-case text-inksoft/70">(optional)</span>
          <input
            value={gstin}
            onChange={(e) => setGstin(e.target.value)}
            placeholder="e.g. 27AAPBC1234F1ZV"
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Drug license number <span className="font-normal normal-case text-inksoft/70">(optional)</span>
          <input
            value={dlNumber}
            onChange={(e) => setDlNumber(e.target.value)}
            placeholder="e.g. MH/DRG/2020/12345"
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Drug license expiry <span className="font-normal normal-case text-inksoft/70">(optional)</span>
          <input
            type="date"
            value={dlExpiry}
            onChange={(e) => setDlExpiry(e.target.value)}
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          PAN <span className="font-normal normal-case text-inksoft/70">(optional)</span>
          <input
            value={pan}
            onChange={(e) => setPan(e.target.value)}
            placeholder="e.g. AABBC1234D"
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </label>

        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Staff seat limit
          <input
            inputMode="numeric"
            value={seats}
            onChange={(e) => /^\d*$/.test(e.target.value) && setSeats(e.target.value)}
            className="mt-1 w-28 rounded-lg border border-line px-3 py-2 text-right font-mono text-sm tabular-nums focus:border-pine-600"
          />
        </label>
        <p className="text-xs text-inksoft">
          {staffCount} staff logins are active. Raising the limit opens empty seats; lowering it to below
          the active count is refused.
        </p>
        <div className="flex items-center justify-end gap-2 pt-1">
          <button
            onClick={() => void save()}
            disabled={busy || !name.trim() || !ownerName.trim() || !phone.trim() || !address.trim() || !seats}
            className="rounded-lg bg-pine-700 px-4 py-2 text-sm font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
          >
            {busy ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </section>
    </div>
  )
}