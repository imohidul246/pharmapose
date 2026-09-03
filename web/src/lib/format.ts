export function money(v: number): string {
  return v.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export function daysUntil(dateStr: string): number {
  const target = new Date(dateStr + 'T00:00:00Z')
  const now = new Date()
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  return Math.round((target.getTime() - today) / 86400000)
}

export function todayISO(offsetDays = 0): string {
  const d = new Date()
  d.setUTCDate(d.getUTCDate() + offsetDays)
  return d.toISOString().slice(0, 10)
}

export function expiryClass(days: number): string {
  if (days < 0) return 'bg-brick-bg text-brick-text'
  if (days <= 90) return 'bg-marigold-bg text-marigold-text'
  return 'bg-safe-bg text-safe-text'
}
