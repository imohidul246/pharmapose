import { useEffect, useState } from 'react'
import Modal from './Modal'
import { api } from '../lib/api'
import { INDIAN_STATES, isValidGSTINShape, stateCodeToName } from '../lib/states'
import type { Customer } from '../types'

export interface CustomerFormProps {
  open: boolean
  onClose: () => void
  defaultType?: 'B2C' | 'B2B'
  title?: string
  submitLabel?: string
  accent?: 'pine' | 'amber'
  onCreated: (c: Customer) => void | Promise<void>
}

const inputClass =
  'w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600'
const monoClass =
  'w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600'

export default function CustomerForm({
  open,
  onClose,
  defaultType = 'B2C',
  title = 'New customer',
  submitLabel = 'Create & select',
  accent = 'pine',
  onCreated,
}: CustomerFormProps) {
  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [customerType, setCustomerType] = useState<'B2C' | 'B2B'>(defaultType)
  const [stateCode, setStateCode] = useState('')
  const [gstin, setGstin] = useState('')
  const [creditLimit, setCreditLimit] = useState('0')
  const [billingAddress, setBillingAddress] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      setName('')
      setPhone('')
      setCustomerType(defaultType)
      setStateCode('')
      setGstin('')
      setCreditLimit('0')
      setBillingAddress('')
      setBusy(false)
      setError('')
    }
  }, [open, defaultType])

  if (!open) return null

  const submit = async () => {
    if (busy) return
    if (!name.trim() || !phone.trim()) {
      setError('Customer name and phone are required.')
      return
    }
    const gst = gstin.trim()
    if (gst && !isValidGSTINShape(gst)) {
      setError('GSTIN looks invalid — check the 15-character format.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const created = await api.createCustomer({
        name: name.trim(),
        phone: phone.trim(),
        credit_limit: Number(creditLimit) || 0,
        gstin: gst || undefined,
        customer_type: customerType,
        state_code: stateCode || undefined,
        state: stateCode ? stateCodeToName(stateCode) : undefined,
        billing_address: billingAddress.trim() || undefined,
      })
      await onCreated(created)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const code = stateCode

  return (
    <Modal title={title} onClose={onClose}>
      {error && (
        <p className="rounded-lg bg-brick-bg px-3 py-2 text-xs font-medium text-brick-text">{error}</p>
      )}
      <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
        Full name *
        <input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Customer name"
          className={'mt-1 ' + inputClass}
        />
      </label>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
        Phone (unique) *
        <input
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          placeholder="Mobile number"
          inputMode="tel"
          className={'mt-1 ' + monoClass}
        />
      </label>
      <div className="grid grid-cols-2 gap-2">
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Customer type
          <select
            value={customerType}
            onChange={(e) => setCustomerType(e.target.value as 'B2C' | 'B2B')}
            className={'mt-1 ' + inputClass}
          >
            <option value="B2C">B2C (Retail)</option>
            <option value="B2B">B2B (Business)</option>
          </select>
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          State (derives state code)
          <select
            value={code}
            onChange={(e) => setStateCode(e.target.value)}
            className={'mt-1 ' + inputClass}
          >
            <option value="">— Select state —</option>
            {INDIAN_STATES.map((s) => (
              <option key={s.code} value={s.code}>
                {s.name} · {s.code}
              </option>
            ))}
          </select>
        </label>
      </div>
      {code && (
        <p className="text-xs text-inksoft">
          State code <span className="font-mono font-semibold">{code}</span> will be recorded and used as
          the place of supply on invoices.
        </p>
      )}
      <div className="grid grid-cols-2 gap-2">
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          GSTIN {customerType === 'B2B' ? '' : '(optional)'}
          <input
            value={gstin}
            onChange={(e) => setGstin(e.target.value.toUpperCase())}
            placeholder="22AAAAA0000A1Z5"
            maxLength={15}
            className={'mt-1 ' + monoClass}
          />
        </label>
        <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
          Credit limit (₹)
          <input
            inputMode="decimal"
            value={creditLimit}
            onChange={(e) => {
              if (!/^\d*\.?\d{0,2}$/.test(e.target.value)) return
              setCreditLimit(e.target.value)
            }}
            className={'mt-1 text-right tabular-nums ' + monoClass}
          />
        </label>
      </div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
        Billing address (optional)
        <textarea
          value={billingAddress}
          onChange={(e) => setBillingAddress(e.target.value)}
          rows={2}
          className={'mt-1 resize-none ' + inputClass}
        />
      </label>
      <button
        onClick={() => void submit()}
        disabled={busy || !name.trim() || !phone.trim()}
        className={
          'w-full rounded-lg px-4 py-2.5 text-sm font-bold text-white transition-colors disabled:bg-line disabled:text-inksoft ' +
          (accent === 'amber'
            ? 'bg-amber-600 hover:bg-amber-500'
            : 'bg-pine-700 hover:bg-pine-600')
        }
      >
        {busy ? 'Saving…' : submitLabel}
      </button>
    </Modal>
  )
}