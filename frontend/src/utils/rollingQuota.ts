export type RollingQuotaKey = 'five_hour' | 'seven_day' | 'thirty_day'

export interface RollingQuotaWindow {
  key: RollingQuotaKey
  label: string
  shortLabel: string
  labelKey: string
  shortLabelKey: string
  limitField: 'five_hour_limit_usd' | 'seven_day_limit_usd' | 'thirty_day_limit_usd'
  usageField: 'five_hour_usage_usd' | 'seven_day_usage_usd' | 'thirty_day_usage_usd'
  windowStartField: 'five_hour_window_start' | 'seven_day_window_start' | 'thirty_day_window_start'
  hours: number
}

export type RollingQuotaSource = Partial<Record<RollingQuotaWindow['limitField'], number | null | undefined>>

export const rollingQuotaWindows: RollingQuotaWindow[] = [
  {
    key: 'five_hour',
    label: '5 hours',
    shortLabel: '5h',
    labelKey: 'payment.quotaWindows.fiveHour',
    shortLabelKey: 'payment.quotaWindows.fiveHourShort',
    limitField: 'five_hour_limit_usd',
    usageField: 'five_hour_usage_usd',
    windowStartField: 'five_hour_window_start',
    hours: 5,
  },
  {
    key: 'seven_day',
    label: '7 days',
    shortLabel: '7d',
    labelKey: 'payment.quotaWindows.sevenDay',
    shortLabelKey: 'payment.quotaWindows.sevenDayShort',
    limitField: 'seven_day_limit_usd',
    usageField: 'seven_day_usage_usd',
    windowStartField: 'seven_day_window_start',
    hours: 168,
  },
  {
    key: 'thirty_day',
    label: '30 days',
    shortLabel: '30d',
    labelKey: 'payment.quotaWindows.thirtyDay',
    shortLabelKey: 'payment.quotaWindows.thirtyDayShort',
    limitField: 'thirty_day_limit_usd',
    usageField: 'thirty_day_usage_usd',
    windowStartField: 'thirty_day_window_start',
    hours: 720,
  },
]

export function formatUsdLimit(value: number | null | undefined): string {
  if (value == null) return '∞'
  return `$${value.toFixed(2)}`
}

export function hasRollingQuotaLimits(source: RollingQuotaSource | null | undefined): boolean {
  if (!source) return false
  return rollingQuotaWindows.some(window => source[window.limitField] != null)
}

export function effectiveWindowEnd(
  windowStart: string | null | undefined,
  hours: number,
  expiresAt?: string | null,
): Date | null {
  if (!windowStart) return null
  const start = new Date(windowStart)
  if (Number.isNaN(start.getTime())) return null

  const windowEnd = new Date(start.getTime() + hours * 60 * 60 * 1000)
  if (!expiresAt) return windowEnd

  const expires = new Date(expiresAt)
  if (Number.isNaN(expires.getTime())) return windowEnd

  return expires.getTime() < windowEnd.getTime() ? expires : windowEnd
}

export function windowEndsBySubscriptionExpiry(
  windowStart: string | null | undefined,
  hours: number,
  expiresAt?: string | null,
): boolean {
  if (!windowStart || !expiresAt) return false
  const start = new Date(windowStart)
  const expires = new Date(expiresAt)
  if (Number.isNaN(start.getTime()) || Number.isNaN(expires.getTime())) return false

  const windowEnd = start.getTime() + hours * 60 * 60 * 1000
  return expires.getTime() < windowEnd
}
