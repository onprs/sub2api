export const ACCOUNT_BALANCE_SYMBOL = '¥'
export const ACTUAL_COST_SYMBOL = ACCOUNT_BALANCE_SYMBOL

export function formatAccountBalance(value: number, fractionDigits = 2): string {
  const amount = Number.isFinite(value) ? value : 0
  return `${ACCOUNT_BALANCE_SYMBOL}${amount.toFixed(fractionDigits)}`
}
