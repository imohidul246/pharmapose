import { useAuth } from '../lib/auth'

function daysUntil(iso: string | null | undefined): number | null {
  if (!iso) return null
  const target = new Date(iso).getTime()
  if (Number.isNaN(target)) return null
  return Math.floor((target - Date.now()) / (24 * 3600 * 1000))
}

function formatExpiry(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString('en-IN', { day: '2-digit', month: 'short', year: 'numeric' })
}

// SubscriptionBanner is the unavoidable top warning shown to store
// owners/staff when the subscription is expiring or suspended. Platform
// admins never see it (they manage renewals, they don't consume them).
export default function SubscriptionBanner() {
  const { session } = useAuth()
  const p = session?.principal
  if (!p || p.is_platform_admin) return null

  const status = p.subscription_status ?? 'ACTIVE'
  const days = daysUntil(p.subscription_valid_until)

  // Suspended stores should never reach here (login/session gate), but if
  // they do, shout loudly.
  if (status === 'SUSPENDED') {
    return (
      <div role="alert" className="bg-brick-text px-4 py-2.5 text-center text-sm font-bold text-white">
        This store is SUSPENDED. Billing is paused — please arrange a cash renewal with the
        administrator immediately.
      </div>
    )
  }

  // No window yet (grace for bootstrapped/legacy stores): stay silent.
  if (days === null) return null

  // Unavoidable warning at ≤ 5 days, including past-due.
  if (days > 5) return null

  const message =
    days < 0
      ? `Subscription expired ${Math.abs(days)} day${Math.abs(days) === 1 ? '' : 's'} ago${p.subscription_valid_until ? ` (expired ${formatExpiry(p.subscription_valid_until)})` : ''}. Please arrange a cash renewal with the administrator — sign-in will stop working.`
      : days === 0
        ? 'Subscription expires TODAY. Please arrange a cash renewal with the administrator to avoid billing interruption.'
        : `Subscription expires in ${days} day${days === 1 ? '' : 's'}${p.subscription_valid_until ? ` (on ${formatExpiry(p.subscription_valid_until)})` : ''}. Please arrange a cash renewal with the administrator.`

  const urgent = days <= 1

  return (
    <div
      role="alert"
      className={
        'px-4 py-2.5 text-center text-[13px] font-semibold ' +
        (urgent ? 'bg-brick-text text-white' : 'bg-marigold-bg text-marigold-text')
      }
    >
      ⚠ {message}
    </div>
  )
}
