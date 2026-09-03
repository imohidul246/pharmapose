import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import Pagination, { usePaged } from '../components/Pagination'
import { INDIAN_STATES } from '../lib/states'
import type { Supplier } from '../types'

type SupplierForm = {
  legal_name: string
  trade_name: string
  gstin: string
  pan: string
  address: string
  state: string
  state_code: string
  phone: string
  email: string
}

const emptyForm: SupplierForm = {
  legal_name: '', trade_name: '', gstin: '', pan: '',
  address: '', state: '', state_code: '', phone: '', email: '',
}

export default function Suppliers({ onMutated }: { onMutated: () => Promise<void> }) {
  const [suppliers, setSuppliers] = useState<Supplier[]>([])
  const [form, setForm] = useState<SupplierForm>(emptyForm)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [editFor, setEditFor] = useState<Supplier | null>(null)
  const supplierPage = usePaged(suppliers, 10)

  const load = async () => {
    try {
      const res = await api.suppliers()
      setSuppliers(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  useEffect(() => { void load() }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const payload = {
        legal_name: form.legal_name.trim(),
        trade_name: form.trade_name.trim() || undefined,
        gstin: form.gstin.trim() || undefined,
        pan: form.pan.trim() || undefined,
        address: form.address.trim() || undefined,
        state: form.state || undefined,
        state_code: form.state_code || undefined,
        phone: form.phone.trim() || undefined,
        email: form.email.trim() || undefined,
      }
      if (editFor) {
        await api.updateSupplier(editFor.id, payload)
        setNotice(`Updated ${form.legal_name}.`)
        setEditFor(null)
      } else {
        await api.createSupplier(payload as Omit<Supplier, 'id' | 'created_at' | 'updated_at'>)
        setNotice(`Created ${form.legal_name}.`)
      }
      setForm(emptyForm)
      await Promise.all([load(), onMutated()])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const del = async (s: Supplier) => {
    if (!confirm(`Delete supplier "${s.legal_name}"?`)) return
    try {
      await api.deleteSupplier(s.id)
      await Promise.all([load(), onMutated()])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const startEdit = (s: Supplier) => {
    setEditFor(s)
    setForm({
      legal_name: s.legal_name,
      trade_name: s.trade_name || '',
      gstin: s.gstin || '',
      pan: s.pan || '',
      address: s.address || '',
      state: s.state || '',
      state_code: s.state_code || '',
      phone: s.phone || '',
      email: s.email || '',
    })
    setNotice('')
    setError('')
  }

  const patch = (patch: Partial<SupplierForm>) => setForm((f) => ({ ...f, ...patch }))

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
      <form onSubmit={(e) => void submit(e)} className="space-y-3 rounded-xl border border-line bg-white p-4 shadow-sm">
        <h3 className="font-display text-sm font-bold uppercase tracking-wide">
          {editFor ? 'Edit supplier' : 'Add supplier'}
        </h3>
        <input
          value={form.legal_name}
          onChange={(e) => patch({ legal_name: e.target.value })}
          placeholder="Legal / trade name *"
          className="w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
        />
        <div className="grid grid-cols-2 gap-2">
          <input
            value={form.trade_name}
            onChange={(e) => patch({ trade_name: e.target.value })}
            placeholder="Trade name (optional)"
            className="rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
          <input
            value={form.phone}
            onChange={(e) => patch({ phone: e.target.value })}
            placeholder="Phone"
            inputMode="tel"
            className="rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
          />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            GSTIN
            <input
              value={form.gstin}
              onChange={(e) => patch({ gstin: e.target.value.toUpperCase() })}
              placeholder="22AAAAA0000A1Z5"
              maxLength={15}
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
            />
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            PAN
            <input
              value={form.pan}
              onChange={(e) => patch({ pan: e.target.value.toUpperCase() })}
              placeholder="AAAAA0000A"
              maxLength={10}
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
            />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            State
            <select
              value={form.state_code}
              onChange={(e) => {
                const st = INDIAN_STATES.find((s) => s.code === e.target.value)
                patch({ state_code: e.target.value, state: st?.name ?? '' })
              }}
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            >
              <option value="">— Select state —</option>
              {INDIAN_STATES.map((s) => (
                <option key={s.code} value={s.code}>{s.code} — {s.name}</option>
              ))}
            </select>
          </label>
          <label className="text-[10px] font-bold uppercase tracking-wider text-inksoft">
            Email
            <input
              type="email"
              value={form.email}
              onChange={(e) => patch({ email: e.target.value })}
              placeholder="supplier@example.com"
              className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
            />
          </label>
        </div>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Address
          <textarea
            value={form.address}
            onChange={(e) => patch({ address: e.target.value })}
            placeholder="Full address"
            rows={2}
            className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
          />
        </label>
        <div className="flex gap-2">
          <button
            type="submit"
            disabled={busy || !form.legal_name.trim()}
            className="flex-1 rounded-lg bg-pine-700 px-4 py-2.5 text-sm font-bold text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
          >
            {busy ? 'Saving…' : editFor ? 'Update supplier' : 'Create supplier'}
          </button>
          {editFor && (
            <button
              type="button"
              onClick={() => { setEditFor(null); setForm(emptyForm) }}
              className="rounded-lg border border-line px-4 py-2.5 text-sm font-semibold text-inksoft transition-colors hover:bg-mint-50"
            >
              Cancel
            </button>
          )}
        </div>
        {notice && (
          <p className="rounded-lg bg-safe-bg px-3 py-2 text-xs font-medium text-safe-text">{notice}</p>
        )}
        {error && (
          <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
        )}
      </form>

      <div className="space-y-3">
        <div className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
          <table className="w-full min-w-[620px] text-sm">
            <thead>
              <tr className="border-b border-line bg-mint-50/70 text-left text-[11px] font-bold uppercase tracking-wider text-inksoft">
                <th className="px-4 py-2.5">Supplier</th>
                <th className="px-4 py-2.5">GSTIN</th>
                <th className="px-4 py-2.5">State</th>
                <th className="px-4 py-2.5 text-center">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-soft">
              {supplierPage.slice.map((s) => (
                <tr key={s.id} className="hover:bg-mint-50/50">
                  <td className="px-4 py-2">
                    <p className="font-medium">{s.legal_name}</p>
                    {s.trade_name && <p className="text-xs text-inksoft">{s.trade_name}</p>}
                    {s.phone && <p className="font-mono text-xs text-inksoft">{s.phone}</p>}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-inksoft">
                    {s.gstin || <span className="italic text-inksoft/50">—</span>}
                  </td>
                  <td className="px-4 py-2 text-xs text-inksoft">
                    {s.state_code ? `${s.state_code} — ${s.state}` : '—'}
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex justify-center gap-1.5">
                      <button
                        onClick={() => startEdit(s)}
                        className="rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => void del(s)}
                        className="rounded-md border border-brick-line px-2.5 py-1 text-xs font-semibold text-brick-text transition-colors hover:bg-brick-bg"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {suppliers.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-10 text-center text-sm text-inksoft">
                    No suppliers yet — add the first one here.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
          <Pagination
            page={supplierPage.page}
            pageCount={supplierPage.pageCount}
            total={supplierPage.total}
            start={supplierPage.start}
            pageSize={10}
            onPage={supplierPage.setPage}
          />
        </div>
      </div>
    </div>
  )
}
