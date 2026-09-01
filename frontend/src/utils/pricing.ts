/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000)    → "$3"      // per 1M tokens
 *   formatScaled(0.5,        1)          → "$0.5"    // per request
 *   formatScaled(null,       1_000_000)  → "-"
 *   formatScaled(0.000003, 1_000_000, 2) → "$3.00"   // pad to ≥2 decimals
 *   formatScaled(1.25e-8,  1_000_000, 2) → "$0.0125" // longer decimals kept as-is
 *
 * Uses toPrecision(10) then strips trailing zeros to avoid IEEE 754 display noise.
 * `minFractionDigits` pads the result back up to a minimum number of decimals.
 * The third argument accepts either a minimum decimal count or a currency symbol
 * for backward compatibility. Pass both as `(value, scale, minDigits, symbol)`.
 */
export function formatScaled(
  value: number | null,
  scale: number,
  minFractionDigitsOrCurrencySymbol: number | string = 0,
  currencySymbol = '$',
): string {
  if (value == null) return '-'
  const minFractionDigits =
    typeof minFractionDigitsOrCurrencySymbol === 'number'
      ? minFractionDigitsOrCurrencySymbol
      : 0
  const symbol =
    typeof minFractionDigitsOrCurrencySymbol === 'string'
      ? minFractionDigitsOrCurrencySymbol
      : currencySymbol
  let formatted = (value * scale).toPrecision(10).replace(/\.?0+$/, '')
  if (minFractionDigits > 0 && !formatted.includes('e')) {
    const dot = formatted.indexOf('.')
    const digits = dot === -1 ? 0 : formatted.length - dot - 1
    if (digits < minFractionDigits) {
      formatted =
        (dot === -1 ? `${formatted}.` : formatted) +
        '0'.repeat(minFractionDigits - digits)
    }
  }
  return `${symbol}${formatted}`
}
