import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import MonitorKeyPickerDialog from '../MonitorKeyPickerDialog.vue'
import type { ApiKey } from '@/types'
import type { Provider } from '@/api/admin/channelMonitor'
import {
  PROVIDER_ANTIGRAVITY_CLAUDE,
  PROVIDER_ANTIGRAVITY_GEMINI,
  PROVIDER_ANTHROPIC,
  PROVIDER_OPENCODE_GO,
} from '@/constants/channelMonitor'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

function apiKey(overrides: Partial<ApiKey>): ApiKey {
  return {
    id: 1,
    user_id: 1,
    key: 'sk-test-key',
    name: 'Test Key',
    group_id: 1,
    status: 'active',
    ip_whitelist: [],
    ip_blacklist: [],
    last_used_at: null,
    quota: 0,
    quota_used: 0,
    expires_at: null,
    created_at: '2026-06-13T00:00:00Z',
    updated_at: '2026-06-13T00:00:00Z',
    rate_limit_5h: 0,
    rate_limit_1d: 0,
    rate_limit_7d: 0,
    usage_5h: 0,
    usage_1d: 0,
    usage_7d: 0,
    window_5h_start: null,
    window_1d_start: null,
    window_7d_start: null,
    reset_5h_at: null,
    reset_1d_at: null,
    reset_7d_at: null,
    ...overrides,
  }
}

function antigravityKey(name: string): ApiKey {
  return apiKey({
    name,
    group: {
      id: 10,
      name: 'Antigravity-TEST',
      description: null,
      platform: 'antigravity',
      rate_multiplier: 0.5,
      is_exclusive: false,
      status: 'active',
      subscription_type: 'standard',
      daily_limit_usd: null,
      weekly_limit_usd: null,
      monthly_limit_usd: null,
      allow_image_generation: false,
      image_rate_independent: false,
      image_rate_multiplier: 1,
      image_price_1k: null,
      image_price_2k: null,
      image_price_4k: null,
      claude_code_only: false,
      fallback_group_id: null,
      fallback_group_id_on_invalid_request: null,
      require_oauth_only: false,
      require_privacy_set: false,
      created_at: '2026-06-13T00:00:00Z',
      updated_at: '2026-06-13T00:00:00Z',
    },
  })
}

function opencodeGoKey(name: string): ApiKey {
  return apiKey({
    name,
    group: {
      id: 11,
      name: 'OpenCode-Go-TEST',
      description: null,
      platform: 'opencode_go' as any,
      rate_multiplier: 0.5,
      is_exclusive: false,
      status: 'active',
      subscription_type: 'standard',
      daily_limit_usd: null,
      weekly_limit_usd: null,
      monthly_limit_usd: null,
      allow_image_generation: false,
      image_rate_independent: false,
      image_rate_multiplier: 1,
      image_price_1k: null,
      image_price_2k: null,
      image_price_4k: null,
      claude_code_only: false,
      fallback_group_id: null,
      fallback_group_id_on_invalid_request: null,
      require_oauth_only: false,
      require_privacy_set: false,
      created_at: '2026-06-13T00:00:00Z',
      updated_at: '2026-06-13T00:00:00Z',
    },
  })
}

function mountDialog(provider: Provider, keys: ApiKey[]) {
  return mount(MonitorKeyPickerDialog, {
    props: {
      show: true,
      loading: false,
      keys,
      provider,
      userGroupRates: {},
    },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        GroupBadge: {
          props: ['name'],
          template: '<span>{{ name }}</span>',
        },
      },
    },
  })
}

describe('MonitorKeyPickerDialog', () => {
  it.each([
    [PROVIDER_ANTIGRAVITY_CLAUDE],
    [PROVIDER_ANTIGRAVITY_GEMINI],
  ])('shows Antigravity group keys for %s monitor providers', (provider) => {
    const wrapper = mountDialog(provider, [antigravityKey('Shared Antigravity Key')])

    expect(wrapper.text()).toContain('Shared Antigravity Key')
    expect(wrapper.text()).toContain('Antigravity-TEST')
  })

  it('keeps non-Antigravity providers matched to their own group platform', () => {
    const wrapper = mountDialog(PROVIDER_ANTHROPIC, [antigravityKey('Wrong Platform Key')])

    expect(wrapper.text()).not.toContain('Wrong Platform Key')
    expect(wrapper.text()).toContain('admin.channelMonitor.form.noActiveKey')
  })

  it('shows OpenCode Go group keys only for OpenCode Go monitor provider', () => {
    const key = opencodeGoKey('OpenCode Go Key')

    const opencodeWrapper = mountDialog(PROVIDER_OPENCODE_GO, [key])
    expect(opencodeWrapper.text()).toContain('OpenCode Go Key')
    expect(opencodeWrapper.text()).toContain('OpenCode-Go-TEST')

    const anthropicWrapper = mountDialog(PROVIDER_ANTHROPIC, [key])
    expect(anthropicWrapper.text()).not.toContain('OpenCode Go Key')
    expect(anthropicWrapper.text()).toContain('admin.channelMonitor.form.noActiveKey')
  })
})
