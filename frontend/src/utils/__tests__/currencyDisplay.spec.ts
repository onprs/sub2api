import { describe, expect, it } from 'vitest'

import {
  ACCOUNT_BALANCE_SYMBOL,
  ACTUAL_COST_SYMBOL,
  formatAccountBalance,
} from '../currencyDisplay'

describe('currencyDisplay', () => {
  it('formats account balances without changing their numeric value', () => {
    expect(ACCOUNT_BALANCE_SYMBOL).toBe('¥')
    expect(ACTUAL_COST_SYMBOL).toBe('¥')
    expect(formatAccountBalance(12.345)).toBe('¥12.35')
    expect(formatAccountBalance(Number.NaN)).toBe('¥0.00')
  })
})
