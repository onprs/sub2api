import { describe, expect, it } from 'vitest'
import { COMPOSITE_ROUTE_PLATFORM_OPTIONS } from '@/constants/platforms'

describe('GroupsView Composite route options', () => {
  it('offers exactly the backend-supported route targets', () => {
    expect(COMPOSITE_ROUTE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      'anthropic',
      'openai',
      'gemini',
      'antigravity',
      'grok',
      'kimi',
      'zhipu',
      'deepseek'
    ])
  })
})
