import { useState } from 'react'
import { useAuth } from '../lib/auth'
import { stateCodeToName } from '../lib/states'

interface LoginMode {
  mode: 'login'
}

interface RegisterMode {
  mode: 'register'
}

type Mode = LoginMode | RegisterMode

export default function Login() {
  const { login, register } = useAuth()
  const [mode, setMode] = useState<Mode>({ mode: 'login' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [phone, setPhone] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [business, setBusiness] = useState('')
  const [storeName, setStoreName] = useState('')
  const [storeAddress, setStoreAddress] = useState('')
  const [storePhone, setStorePhone] = useState('')
  const [gstin, setGstin] = useState('')
  const [pan, setPan] = useState('')
  const [dlNumber, setDlNumber] = useState('')
  const [dlExpiry, setDlExpiry] = useState('')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (mode.mode === 'register') {
        await register({
          name: name.trim(),
          phone: phone.trim(),
          password,
          business_name: business.trim(),
          store_name: storeName.trim(),
          store_address: storeAddress.trim(),
          store_phone: storePhone.trim(),
          gstin: gstin.trim() || undefined,
          pan: pan.trim() || undefined,
          drug_license_number: dlNumber.trim() || undefined,
          drug_license_expiry: dlExpiry.trim() || undefined,
        })
      } else {
        await login(phone.trim(), password)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const other = mode.mode === 'login'

  return (
    <div className="min-h-screen bg-pine-950">
      <div className="pointer-events-none fixed inset-0 opacity-[0.035]" aria-hidden
        style={{
          backgroundImage:
            'radial-gradient(circle, white 1px, transparent 1.6px)',
          backgroundSize: '22px 22px',
        }}
      />
      <div className="relative mx-auto flex min-h-screen max-w-[1400px] flex-col items-center justify-center px-4 py-12 lg:flex-row lg:gap-16">
        <div className="mb-8 hidden lg:mb-0 lg:block">
          <div className="flex h-36 w-36 items-center justify-center rounded-3xl border border-white/15 bg-white/[0.04]">
            <span className="font-display text-6xl font-black text-white">℞</span>
          </div>
          <h1 className="mt-6 font-display text-4xl font-black uppercase tracking-tight text-white">
            PharmaPOS
          </h1>
          <p className="mt-2 max-w-xs text-sm leading-relaxed text-porcelain/60">
            Medical store billing — one shop, one counter, every batch, every bill, every rupee.
            Owner approves; the counter records.
          </p>
        </div>

        <div className="w-full max-w-md rounded-2xl bg-white p-6 shadow-2xl shadow-black/40 lg:p-8">
          <div className="mb-5 flex items-center gap-3 lg:hidden">
            <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-pine-700 font-display text-xl font-black text-mint-100">
              ℞
            </span>
            <span className="font-display text-xl font-black uppercase tracking-tight text-pine-900">
              PharmaPOS
            </span>
          </div>

          <h2 className="font-display text-lg font-bold tracking-tight text-ink">
            {other ? 'Sign in to the counter' : 'First run — open a new store'}
          </h2>
          <p className="mt-1 text-xs leading-relaxed text-inksoft">
            {other
              ? 'Use the phone number your owner registered for you.'
              : 'Register the owner login and this store starts empty. Existing installs skip this.'}
          </p>

          <form onSubmit={(e) => void submit(e)} className="mt-5 space-y-3">
            {mode.mode === 'register' && (
              <>
                <fieldset className="rounded-lg border border-line bg-cream/50 p-3">
                  <legend className="px-1 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                    Account information
                  </legend>
                  <div className="space-y-3">
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Owner name *
                      <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="e.g. R. Kadam"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Login phone *
                      <input
                        inputMode="numeric"
                        autoComplete="tel"
                        value={phone}
                        onChange={(e) => /^\d{0,12}$/.test(e.target.value) && setPhone(e.target.value)}
                        placeholder="e.g. 98200 00000"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Password *
                      <input
                        type="password"
                        autoComplete="new-password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder="8 characters or more"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                  </div>
                </fieldset>

                <fieldset className="rounded-lg border border-line bg-cream/50 p-3">
                  <legend className="px-1 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                    Shop information
                  </legend>
                  <div className="space-y-3">
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Store name *
                      <input
                        value={storeName}
                        onChange={(e) => setStoreName(e.target.value)}
                        placeholder="e.g. Kadam Medicals — Camp Branch"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Owner name *
                      <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        disabled
                        title="Same as the account owner name"
                        className="mt-1 w-full rounded-lg border border-line bg-line/30 px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Store phone *
                      <input
                        inputMode="numeric"
                        value={storePhone}
                        onChange={(e) => /^\d{0,12}$/.test(e.target.value) && setStorePhone(e.target.value)}
                        placeholder="e.g. 020 2621 4455"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Store address *
                      <textarea
                        value={storeAddress}
                        onChange={(e) => setStoreAddress(e.target.value)}
                        rows={2}
                        placeholder="Shop no., street, city, PIN"
                        className="mt-1 w-full resize-none rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                  </div>
                </fieldset>

                <fieldset className="rounded-lg border border-line bg-cream/50 p-3">
                  <legend className="px-1 text-[10px] font-bold uppercase tracking-wider text-inksoft">
                    Business information <span className="font-normal normal-case text-inksoft/70">(optional)</span>
                  </legend>
                  <div className="space-y-3">
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      Business name <span className="font-normal normal-case text-inksoft/70">(optional)</span>
                      <input
                        value={business}
                        onChange={(e) => setBusiness(e.target.value)}
                        placeholder="e.g. Kadam Medicals"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                      />
                    </label>
                    <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                      GSTIN <span className="font-normal normal-case text-inksoft/70">(optional)</span>
                      <input
                        value={gstin}
                        onChange={(e) => setGstin(e.target.value)}
                        placeholder="e.g. 27AAPBC1234F1ZV"
                        className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
                      />
                    </label>
                    {/^[0-9]{2}/.test(gstin.trim()) && stateCodeToName(gstin.trim().slice(0, 2)) && (
                      <p className="text-xs text-inksoft">
                        Store state: {stateCodeToName(gstin.trim().slice(0, 2) as string)} (
                        {gstin.trim().slice(0, 2)})
                      </p>
                    )}
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
                  </div>
                </fieldset>
              </>
            )}

            {other && (
              <>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                  Phone
                  <input
                    inputMode="numeric"
                    autoComplete="tel"
                    value={phone}
                    onChange={(e) => /^\d{0,12}$/.test(e.target.value) && setPhone(e.target.value)}
                    placeholder="e.g. 98200 00000"
                    className="mt-1 w-full rounded-lg border border-line px-3 py-2 font-mono text-sm focus:border-pine-600"
                  />
                </label>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-inksoft">
                  Password
                  <input
                    type="password"
                    autoComplete={other ? 'current-password' : 'new-password'}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="8 characters or more"
                    className="mt-1 w-full rounded-lg border border-line px-3 py-2 text-sm focus:border-pine-600"
                  />
                </label>
              </>
            )}

            {error && (
              <p
                role="alert"
                className={
                  'rounded-lg px-3 py-2 text-xs font-medium ' +
                  (/subscription|renewal|administrator/i.test(error)
                    ? 'bg-marigold-bg text-marigold-text'
                    : 'bg-brick-bg text-brick-text')
                }
              >
                {/subscription|renewal|administrator/i.test(error) ? `⚠ ${error}` : error}
              </p>
            )}

            <button
              type="submit"
              disabled={busy || !phone.trim() || password.length < 8}
              className="w-full rounded-xl bg-pine-700 py-2.5 font-display text-[15px] font-bold tracking-tight text-white transition-colors hover:bg-pine-600 disabled:bg-line disabled:text-inksoft"
            >
              {busy ? (other ? 'Signing in…' : 'Registering…') : other ? 'Sign in' : 'Register store'}
            </button>
          </form>

          <button
            type="button"
            onClick={() => {
              setMode({ mode: other ? 'register' : 'login' })
              setError('')
            }}
            className="mt-4 w-full text-center text-xs font-semibold text-pine-700 underline decoration-pine-300 underline-offset-4 transition-colors hover:text-pine-600"
          >
            {other
              ? "First run? Register a new store here"
              : 'Already registered? Sign in'}
          </button>
        </div>
      </div>
    </div>
  )
}