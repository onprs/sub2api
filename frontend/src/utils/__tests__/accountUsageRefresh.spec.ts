import { describe, expect, it } from 'vitest'
import {
  buildAccountUsageRefreshKey,
  buildGrokUsageRefreshKey,
  buildOpenAIAPIKeyBalanceRefreshKey,
  buildOpenAIUsageRefreshKey
} from '../accountUsageRefresh'

describe('buildOpenAIUsageRefreshKey', () => {
  it('OpenAI API Key 的 Base URL 或代理变化时会生成不同余额刷新 key', () => {
    const base = {
      id: 8,
      platform: 'openai',
      type: 'apikey',
      updated_at: '2026-08-13T10:00:00Z',
      proxy_id: null,
      credentials: { base_url: 'https://upstream-a.example/v1' },
      credentials_status: { has_api_key: true }
    } as any
    const next = {
      ...base,
      updated_at: '2026-08-13T10:01:00Z',
      proxy_id: 3,
      credentials: { base_url: 'https://upstream-b.example' }
    }

    expect(buildOpenAIAPIKeyBalanceRefreshKey(base)).not.toBe(buildOpenAIAPIKeyBalanceRefreshKey(next))
    expect(buildAccountUsageRefreshKey(base)).not.toBe(buildAccountUsageRefreshKey(next))
  })

  it('非 OpenAI API Key 不生成余额刷新 key', () => {
    expect(buildOpenAIAPIKeyBalanceRefreshKey({
      id: 9,
      platform: 'openai',
      type: 'oauth',
      updated_at: '2026-08-13T10:00:00Z',
      proxy_id: null
    } as any)).toBe('')
  })

  it('会在 codex 快照变化时生成不同 key', () => {
    const base = {
      id: 1,
      platform: 'openai',
      type: 'oauth',
      updated_at: '2026-03-07T10:00:00Z',
      last_used_at: '2026-03-07T09:59:00Z',
      extra: {
        codex_usage_updated_at: '2026-03-07T10:00:00Z',
        codex_5h_used_percent: 0,
        codex_7d_used_percent: 0
      }
    } as any

    const next = {
      ...base,
      extra: {
        ...base.extra,
        codex_usage_updated_at: '2026-03-07T10:01:00Z',
        codex_5h_used_percent: 100
      }
    }

    expect(buildOpenAIUsageRefreshKey(base)).not.toBe(buildOpenAIUsageRefreshKey(next))
  })

  it('会在 last_used_at 变化时生成不同 key', () => {
    const base = {
      id: 3,
      platform: 'openai',
      type: 'oauth',
      updated_at: '2026-03-07T10:00:00Z',
      last_used_at: '2026-03-07T10:00:00Z',
      extra: {
        codex_usage_updated_at: '2026-03-07T10:00:00Z',
        codex_5h_used_percent: 12,
        codex_7d_used_percent: 24
      }
    } as any

    const next = {
      ...base,
      last_used_at: '2026-03-07T10:02:00Z'
    }

    expect(buildOpenAIUsageRefreshKey(base)).not.toBe(buildOpenAIUsageRefreshKey(next))
  })

  it('非 OpenAI OAuth 账号返回空 key', () => {
    expect(buildOpenAIUsageRefreshKey({
      id: 2,
      platform: 'anthropic',
      type: 'oauth',
      updated_at: '2026-03-07T10:00:00Z',
      last_used_at: '2026-03-07T10:00:00Z',
      extra: {}
    } as any)).toBe('')
  })

  it('ClinePass official usage snapshot changes the account usage key', () => {
    const base = {
      id: 5,
      platform: 'clinepass',
      type: 'apikey',
      updated_at: '2026-07-22T10:00:00Z',
      last_used_at: null,
      rate_limit_reset_at: null,
      extra: {
        clinepass_usage_source: 'official_api',
        clinepass_usage_updated_at: '2026-07-22T10:00:00Z',
        clinepass_usage_5h_used_percent: 10
      }
    } as any
    const next = {
      ...base,
      extra: {
        ...base.extra,
        clinepass_usage_updated_at: '2026-07-22T10:05:00Z',
        clinepass_usage_5h_used_percent: 100
      }
    }
    expect(buildAccountUsageRefreshKey(base)).not.toBe(buildAccountUsageRefreshKey(next))
  })

  it('OpenCode Go 官方 Console 快照变化时生成不同账号 usage key', () => {
    const base = {
      id: 4,
      platform: 'opencode_go',
      type: 'apikey',
      updated_at: '2026-06-22T10:00:00Z',
      last_used_at: null,
      rate_limit_reset_at: null,
      extra: {
        opencode_go_console_auth_status: 'ready',
        opencode_go_usage_source: 'estimated',
        opencode_go_usage_updated_at: '2026-06-22T10:00:00Z',
        opencode_go_usage_5h_used_percent: 51
      }
    } as any

    const next = {
      ...base,
      extra: {
        ...base.extra,
        opencode_go_usage_source: 'official_console',
        opencode_go_usage_updated_at: '2026-06-22T10:05:00Z',
        opencode_go_usage_5h_used_percent: 19,
        opencode_go_usage_5h_resets_at: '2026-06-22T11:30:00Z',
        opencode_go_usage_7d_used_percent: 7,
        opencode_go_usage_30d_used_percent: 10
      }
    }

    expect(buildAccountUsageRefreshKey(base)).not.toBe(buildAccountUsageRefreshKey(next))
  })

  it('OpenRouter official usage snapshot changes the account usage key', () => {
    const base = {
      id: 6,
      platform: 'openrouter',
      type: 'apikey',
      updated_at: '2026-08-19T10:00:00Z',
      last_used_at: null,
      rate_limit_reset_at: null,
      extra: {
        openrouter_usage_source: 'official_api',
        openrouter_usage_updated_at: '2026-08-19T10:00:00Z',
        openrouter_usage_used_usd: 10
      }
    } as any
    const next = {
      ...base,
      extra: {
        ...base.extra,
        openrouter_usage_updated_at: '2026-08-19T10:05:00Z',
        openrouter_usage_used_usd: 20
      }
    }
    expect(buildAccountUsageRefreshKey(base)).not.toBe(buildAccountUsageRefreshKey(next))
  })
})

describe('buildGrokUsageRefreshKey', () => {
  it('changes when a canonical Grok billing or usage snapshot changes', () => {
    const base = {
      platform: 'grok',
      extra: {
        grok_billing_snapshot: { plan: 'Free', usage_percent: 0 },
        grok_usage_snapshot: { subscription_tier: 'Free', status_code: 200 }
      }
    } as any

    expect(buildGrokUsageRefreshKey(base)).not.toBe(buildGrokUsageRefreshKey({
      ...base,
      extra: {
        ...base.extra,
        grok_billing_snapshot: { plan: 'SuperGrok', usage_percent: 0 }
      }
    }))
    expect(buildGrokUsageRefreshKey(base)).not.toBe(buildGrokUsageRefreshKey({
      ...base,
      extra: {
        ...base.extra,
        grok_usage_snapshot: { subscription_tier: 'SuperGrok', status_code: 200 }
      }
    }))
  })

  it('ignores object key order and a legacy alias shadowed by canonical usage', () => {
    const first = {
      platform: 'grok',
      extra: {
        grok_billing_snapshot: {
          plan: 'SuperGrok',
          limits: { monthly: 100, weekly: 25 }
        },
        grok_usage_snapshot: { status_code: 200, subscription_tier: 'SuperGrok' },
        grok_quota_snapshot: { subscription_tier: 'Free' }
      }
    } as any
    const reordered = {
      platform: 'grok',
      extra: {
        grok_quota_snapshot: { subscription_tier: 'SuperGrok Heavy' },
        grok_usage_snapshot: { subscription_tier: 'SuperGrok', status_code: 200 },
        grok_billing_snapshot: {
          limits: { weekly: 25, monthly: 100 },
          plan: 'SuperGrok'
        }
      }
    } as any

    expect(buildGrokUsageRefreshKey(first)).toBe(buildGrokUsageRefreshKey(reordered))
  })

  it('uses the legacy quota alias only when the canonical snapshot is absent', () => {
    const base = {
      platform: 'grok',
      extra: { grok_quota_snapshot: { subscription_tier: 'Free' } }
    } as any
    const next = {
      platform: 'grok',
      extra: { grok_quota_snapshot: { subscription_tier: 'SuperGrok' } }
    } as any

    expect(buildGrokUsageRefreshKey(base)).not.toBe(buildGrokUsageRefreshKey(next))
  })

  it('tracks the legacy tier when the canonical snapshot has no usable tier', () => {
    for (const canonicalSnapshot of [
      { status_code: 200 },
      { status_code: 200, subscription_tier: '   ' },
    ]) {
      const base = {
        platform: 'grok',
        extra: {
          grok_usage_snapshot: canonicalSnapshot,
          grok_quota_snapshot: { subscription_tier: 'Free' },
        },
      } as any
      const next = {
        ...base,
        extra: {
          ...base.extra,
          grok_quota_snapshot: { subscription_tier: 'SuperGrok' },
        },
      }

      expect(buildGrokUsageRefreshKey(base)).not.toBe(buildGrokUsageRefreshKey(next))
    }
  })

  it('returns an empty key for non-Grok accounts', () => {
    expect(buildGrokUsageRefreshKey({
      platform: 'openai',
      extra: { grok_usage_snapshot: { subscription_tier: 'SuperGrok' } }
    } as any)).toBe('')
  })
})
