import { describe, expect, it } from 'vitest'

import { formatRequestId } from '@/utils/requestId'

describe('formatRequestId', () => {
  it.each([
    ['client:client-request-123', 'client-request-123'],
    ['local:server-request-123', 'server-request-123'],
    ['generated:fallback-request-123', 'fallback-request-123'],
  ])('hides the internal prefix in %s', (value, expected) => {
    expect(formatRequestId(value)).toBe(expected)
  })

  it('preserves other request ID formats and trims surrounding whitespace', () => {
    expect(formatRequestId('  req:external-123  ')).toBe('req:external-123')
    expect(formatRequestId(null)).toBe('')
  })
})
