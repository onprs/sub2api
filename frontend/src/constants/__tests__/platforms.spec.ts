import { describe, expect, it } from 'vitest'
import {
  COMPOSITE_ROUTE_PLATFORM_OPTIONS,
  CONCRETE_PLATFORM_OPTIONS,
  GROUP_PLATFORM_OPTIONS,
} from '@/constants/platforms'

const concretePlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'opencode_go',
  'clinepass',
  'openrouter',
  'commandcode',
  'kimi',
  'zhipu',
  'deepseek'
]

describe('platform option catalogs', () => {
  it('exposes every concrete account platform', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(concretePlatforms)
  })

  it('exposes every Composite route target platform', () => {
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

  it('adds composite for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'composite'
    ])
  })
})
