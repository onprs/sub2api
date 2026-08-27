import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountUsageCell from '../AccountUsageCell.vue'
import type { Account } from '@/types'

const { getUsage } = vi.hoisted(() => ({
  getUsage: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getUsage
    }
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'antigravity',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('AccountUsageCell', () => {
  beforeEach(() => {
    getUsage.mockReset()
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: true,
        media: '(min-width: 768px)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    })
  })

  it('OpenAI API Key 会实时展示 sub2api 钱包余额并支持手动刷新', async () => {
    getUsage
      .mockResolvedValueOnce({
        updated_at: '2026-08-13T10:00:00Z',
        upstream_balance: {
          status: 'available',
          source: 'sub2api',
          kind: 'wallet',
          amount: 12.34,
          unit: 'USD',
          plan_name: '钱包余额',
          is_valid: true,
          updated_at: '2026-08-13T10:00:00Z'
        }
      })
      .mockResolvedValueOnce({
        upstream_balance: {
          status: 'available',
          source: 'sub2api',
          kind: 'wallet',
          amount: 9.87,
          unit: 'USD',
          is_valid: true,
          updated_at: null
        }
      })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 8101,
          platform: 'openai',
          type: 'apikey',
          credentials: { base_url: 'https://sub2api.example/v1' },
          credentials_status: { has_api_key: true }
        })
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(8101)
    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.wallet')
    expect(wrapper.text()).toMatch(/12[.,]34/)
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBalance.source')
    expect(wrapper.text()).not.toContain('钱包余额')

    await wrapper.find('button').trigger('click')
    await flushPromises()

    expect(getUsage).toHaveBeenLastCalledWith(8101, 'active', true)
    expect(wrapper.text()).toMatch(/9[.,]87/)
  })

  it('OpenAI API Key 会正确显示 0 余额和 Key 配额详情', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: {
        status: 'available',
        source: 'sub2api',
        kind: 'api_key_quota',
        amount: 0,
        limit: 20,
        used: 20,
        unit: 'USD',
        is_valid: false
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 8102, platform: 'openai', type: 'apikey' })
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.quotaRemaining')
    expect(wrapper.text()).toMatch(/0[.,]00/)
    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.keyUnavailable')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBalance.quotaDetail')
  })

  it('OpenAI API Key 会区分无限订阅和滚动限额', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: {
        status: 'available',
        source: 'sub2api',
        kind: 'subscription',
        amount: null,
        unit: 'USD',
        plan_name: '无限套餐'
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 8107, platform: 'openai', type: 'apikey' })
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.subscriptionRemaining')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.unlimitedSubscription')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBalance.rateLimited')
  })

  it('普通 OpenAI 兼容上游不支持余额接口时静默显示占位符', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: { status: 'unsupported', error_code: 'unsupported' }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 8103, platform: 'openai', type: 'apikey' })
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()

    expect(wrapper.text()).toBe('-')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBalance.queryFailed')

    await wrapper.setProps({ liveBalanceRefreshToken: 1 })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(1)
  })

  it('OpenAI API Key 上游鉴权失败时展示可重试错误', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: { status: 'error', error_code: 'unauthorized' }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 8104, platform: 'openai', type: 'apikey' })
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.unauthorized')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.retry')
  })

  it('OpenAI API Key 展示上游余额时保留原有今日统计和本地配额', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: {
        status: 'available',
        source: 'sub2api',
        kind: 'wallet',
        amount: 5,
        unit: 'USD'
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 8105,
          platform: 'openai',
          type: 'apikey',
          quota_daily_limit: 10,
          quota_daily_used: 2
        }),
        todayStats: {
          requests: 3,
          tokens: 1200,
          cost: 0.5,
          user_cost: 0.75
        }
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('3 req')
    expect(wrapper.text()).toContain('1.2K')
    expect(wrapper.text()).toContain('A $0.50')
    expect(wrapper.text()).toContain('U $0.75')
    expect(wrapper.text()).toContain('1d|20')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBalance.wallet')
  })

  it('余额专用刷新信号会刷新已识别的 OpenAI API Key 上游', async () => {
    getUsage.mockResolvedValue({
      upstream_balance: {
        status: 'available',
        source: 'sub2api',
        kind: 'wallet',
        amount: 5,
        unit: 'USD'
      }
    })
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 8106, platform: 'openai', type: 'apikey' }),
        liveBalanceRefreshToken: 0
      },
      global: { stubs: { UsageProgressBar: true, AccountQuotaInfo: true } }
    })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ liveBalanceRefreshToken: 1 })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(2)
  })

  it('renders ClinePass official 5h/7d/30d windows without an estimated label', async () => {
    getUsage.mockResolvedValue({
      source: 'official_api',
      five_hour: { utilization: 18.5, resets_at: null, source: 'official_api' },
      seven_day: { utilization: 42, resets_at: '2026-07-29T00:00:00Z', source: 'official_api' },
      thirty_day: { utilization: 67, resets_at: null, source: 'official_api' }
    })
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({ id: 7001, platform: 'clinepass', type: 'apikey', extra: {} })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt'],
            template: '<div>{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })
    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(7001)
    expect(wrapper.text()).toContain('5h|18.5|')
    expect(wrapper.text()).toContain('7d|42|2026-07-29T00:00:00Z')
    expect(wrapper.text()).toContain('30d|67|')
    expect(wrapper.text()).toContain('official')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.estimatedData')
  })

  it('Antigravity 图片用量会聚合新旧 image 模型', async () => {
    getUsage.mockResolvedValue({
      antigravity_quota: {
        'gemini-2.5-flash-image': {
          utilization: 45,
          reset_time: '2026-03-01T11:00:00Z'
        },
        'gemini-3.1-flash-image': {
          utilization: 20,
          reset_time: '2026-03-01T10:00:00Z'
        },
        'gemini-3-pro-image': {
          utilization: 70,
          reset_time: '2026-03-01T09:00:00Z'
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 1001,
          platform: 'antigravity',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.usageWindow.gemini3Image|70|2026-03-01T09:00:00Z')
  })

  it('Antigravity 会显示 AI Credits 余额信息', async () => {
    getUsage.mockResolvedValue({
      ai_credits: [
        {
          credit_type: 'GOOGLE_ONE_AI',
          amount: 25,
          minimum_balance: 5
        }
      ]
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 1002,
          platform: 'antigravity',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.aiCreditsBalance')
    expect(wrapper.text()).toContain('25')
  })


  it('OpenAI OAuth 快照已过期时首屏会重新请求 usage', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 15,
        resets_at: '2026-03-08T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 3,
          tokens: 300,
          cost: 0.03,
          standard_cost: 0.03,
          user_cost: 0.03
        }
      },
      seven_day: {
        utilization: 77,
        resets_at: '2026-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 3,
          tokens: 300,
          cost: 0.03,
          standard_cost: 0.03,
          user_cost: 0.03
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2000,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2026-03-07T00:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2026-03-08T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2026-03-13T12:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(2000)
    expect(wrapper.text()).toContain('5h|15|300')
    expect(wrapper.text()).toContain('7d|77|300')
  })

  it('OpenAI OAuth 有 codex 快照时仍然使用 /usage API 数据渲染', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 18,
        resets_at: '2099-03-07T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      },
      seven_day: {
        utilization: 36,
        resets_at: '2099-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2001,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2099-03-07T10:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2099-03-07T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2099-03-13T12:00:00Z'
          }
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(2001)
    // 单一数据源：始终使用 /usage API 返回值，忽略 codex 快照
    expect(wrapper.text()).toContain('5h|18|900')
    expect(wrapper.text()).toContain('7d|36|900')
  })

  it('OpenCode Go API key 会展示估算的 5h/7d/30d 用量并支持刷新', async () => {
    const usagePayload = {
      five_hour: {
        utilization: 25,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: {
          requests: 4,
          tokens: 600,
          cost: 3,
          standard_cost: 3,
          user_cost: 3
        }
      },
      seven_day: {
        utilization: 50,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: {
          requests: 8,
          tokens: 1200,
          cost: 15,
          standard_cost: 15,
          user_cost: 15
        }
      },
      thirty_day: {
        utilization: 75,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: {
          requests: 12,
          tokens: 2400,
          cost: 45,
          standard_cost: 45,
          user_cost: 45
        }
      }
    }
    getUsage.mockResolvedValue(usagePayload)

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 5001,
          platform: 'opencode_go',
          type: 'apikey',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(5001)
    expect(wrapper.text()).toContain('5h|25|600')
    expect(wrapper.text()).toContain('7d|50|1200')
    expect(wrapper.text()).toContain('30d|75|2400')
    expect(wrapper.text()).toContain('admin.accounts.usageWindow.estimatedData')
    expect(wrapper.find('span[title="Based on Sub2API logs"]').exists()).toBe(true)

    await wrapper.find('button').trigger('click')
    await flushPromises()

    expect(getUsage).toHaveBeenLastCalledWith(5001, 'active', true)
  })

  it('OpenCode Go API key 会展示官方 Console 用量来源和 reset 时间', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 19,
        resets_at: '2026-06-22T05:43:10Z',
        remaining_seconds: 5590,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      },
      seven_day: {
        utilization: 7,
        resets_at: '2026-06-29T05:43:10Z',
        remaining_seconds: 588490,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      },
      thirty_day: {
        utilization: 10,
        resets_at: '2026-07-22T05:43:10Z',
        remaining_seconds: 2265176,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 5002,
          platform: 'opencode_go',
          type: 'apikey',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(5002)
    expect(wrapper.text()).toContain('5h|19|2026-06-22T05:43:10Z')
    expect(wrapper.text()).toContain('7d|7|2026-06-29T05:43:10Z')
    expect(wrapper.text()).toContain('30d|10|2026-07-22T05:43:10Z')
    expect(wrapper.text()).toContain('official')
    expect(wrapper.find('span[title="OpenCode official Console"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.estimatedData')
  })

  it('OpenCode Go 官方快照变化时会丢弃旧 estimated 缓存并重新拉取 usage', async () => {
    getUsage
      .mockResolvedValueOnce({
        five_hour: {
          utilization: 25,
          resets_at: null,
          remaining_seconds: 0,
          estimated: true,
          source: 'estimated',
          source_label: 'Based on Sub2API logs',
          window_stats: { requests: 4, tokens: 600, cost: 3, standard_cost: 3, user_cost: 3 }
        },
        seven_day: {
          utilization: 50,
          resets_at: null,
          remaining_seconds: 0,
          estimated: true,
          source: 'estimated',
          source_label: 'Based on Sub2API logs',
          window_stats: { requests: 8, tokens: 1200, cost: 15, standard_cost: 15, user_cost: 15 }
        },
        thirty_day: {
          utilization: 75,
          resets_at: null,
          remaining_seconds: 0,
          estimated: true,
          source: 'estimated',
          source_label: 'Based on Sub2API logs',
          window_stats: { requests: 12, tokens: 2400, cost: 45, standard_cost: 45, user_cost: 45 }
        }
      })
      .mockResolvedValueOnce({
        five_hour: {
          utilization: 19,
          resets_at: '2026-06-22T05:43:10Z',
          remaining_seconds: 5590,
          source: 'official_console',
          source_label: 'OpenCode official Console',
          window_stats: null
        },
        seven_day: {
          utilization: 7,
          resets_at: '2026-06-29T05:43:10Z',
          remaining_seconds: 588490,
          source: 'official_console',
          source_label: 'OpenCode official Console',
          window_stats: null
        },
        thirty_day: {
          utilization: 10,
          resets_at: '2026-07-22T05:43:10Z',
          remaining_seconds: 2265176,
          source: 'official_console',
          source_label: 'OpenCode official Console',
          window_stats: null
        }
      })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 5010,
          platform: 'opencode_go',
          type: 'apikey',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('admin.accounts.usageWindow.estimatedData')
    expect(wrapper.text()).toContain('5h|25||600')

    await wrapper.setProps({
      account: makeAccount({
        id: 5010,
        platform: 'opencode_go',
        type: 'apikey',
        extra: {
          opencode_go_console_auth_status: 'ready',
          opencode_go_usage_source: 'official_console',
          opencode_go_usage_updated_at: '2026-06-22T04:10:00Z',
          opencode_go_usage_5h_used_percent: 19,
          opencode_go_usage_5h_resets_at: '2026-06-22T05:43:10Z',
          opencode_go_usage_7d_used_percent: 7,
          opencode_go_usage_7d_resets_at: '2026-06-29T05:43:10Z',
          opencode_go_usage_30d_used_percent: 10,
          opencode_go_usage_30d_resets_at: '2026-07-22T05:43:10Z'
        }
      })
    })
    await flushPromises()

    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(getUsage).toHaveBeenLastCalledWith(5010)
    expect(wrapper.text()).toContain('official')
    expect(wrapper.text()).toContain('5h|19|2026-06-22T05:43:10Z|')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.estimatedData')
  })

  it('OpenCode Go 旧 estimated 请求晚返回时不会覆盖新的 official usage', async () => {
    const firstRequest = deferred<any>()
    const secondRequest = deferred<any>()
    getUsage
      .mockImplementationOnce(() => firstRequest.promise)
      .mockImplementationOnce(() => secondRequest.promise)

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 5011,
          platform: 'opencode_go',
          type: 'apikey',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await vi.waitFor(() => {
      expect(getUsage).toHaveBeenCalledTimes(1)
    })

    await wrapper.setProps({
      account: makeAccount({
        id: 5011,
        platform: 'opencode_go',
        type: 'apikey',
        extra: {
          opencode_go_console_auth_status: 'ready',
          opencode_go_usage_source: 'official_console',
          opencode_go_usage_updated_at: '2026-06-22T12:29:09Z',
          opencode_go_usage_5h_used_percent: 0,
          opencode_go_usage_5h_resets_at: '2026-06-22T17:29:41Z',
          opencode_go_usage_7d_used_percent: 0,
          opencode_go_usage_7d_resets_at: '2026-06-29T00:00:01Z',
          opencode_go_usage_30d_used_percent: 0,
          opencode_go_usage_30d_resets_at: '2026-07-22T06:04:05Z'
        }
      })
    })

    await vi.waitFor(() => {
      expect(getUsage).toHaveBeenCalledTimes(2)
    })

    secondRequest.resolve({
      five_hour: {
        utilization: 0,
        resets_at: '2026-06-22T17:29:41Z',
        remaining_seconds: 17990,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      },
      seven_day: {
        utilization: 0,
        resets_at: '2026-06-29T00:00:01Z',
        remaining_seconds: 559200,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      },
      thirty_day: {
        utilization: 0,
        resets_at: '2026-07-22T06:04:05Z',
        remaining_seconds: 2560000,
        source: 'official_console',
        source_label: 'OpenCode official Console',
        window_stats: null
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('official')
    expect(wrapper.text()).toContain('5h|0|2026-06-22T17:29:41Z|')

    firstRequest.resolve({
      five_hour: {
        utilization: 25,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: { requests: 4, tokens: 600, cost: 3, standard_cost: 3, user_cost: 3 }
      },
      seven_day: {
        utilization: 50,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: { requests: 8, tokens: 1200, cost: 15, standard_cost: 15, user_cost: 15 }
      },
      thirty_day: {
        utilization: 75,
        resets_at: null,
        remaining_seconds: 0,
        estimated: true,
        source: 'estimated',
        source_label: 'Based on Sub2API logs',
        window_stats: { requests: 12, tokens: 2400, cost: 45, standard_cost: 45, user_cost: 45 }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('official')
    expect(wrapper.text()).toContain('5h|0|2026-06-22T17:29:41Z|')
    expect(wrapper.text()).not.toContain('admin.accounts.usageWindow.estimatedData')
    expect(wrapper.text()).not.toContain('5h|25||600')
  })

  it('OpenAI OAuth 有现成快照时，手动刷新信号会触发 usage 重拉', async () => {
    getUsage.mockResolvedValue({
      five_hour: {
        utilization: 18,
        resets_at: '2099-03-07T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      },
      seven_day: {
        utilization: 36,
        resets_at: '2099-03-13T12:00:00Z',
        remaining_seconds: 3600,
        window_stats: {
          requests: 9,
          tokens: 900,
          cost: 0.09,
          standard_cost: 0.09,
          user_cost: 0.09
        }
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 2010,
          platform: 'openai',
          type: 'oauth',
          extra: {
            codex_usage_updated_at: '2099-03-07T10:00:00Z',
            codex_5h_used_percent: 12,
            codex_5h_reset_at: '2099-03-07T12:00:00Z',
            codex_7d_used_percent: 34,
            codex_7d_reset_at: '2099-03-13T12:00:00Z'
          },
          rate_limit_reset_at: null
        }),
        manualRefreshToken: 0
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
          },
          AccountQuotaInfo: true
        }
      }
    })

    await flushPromises()
    // mount 时已经拉取一次
    expect(getUsage).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ manualRefreshToken: 1 })
    await flushPromises()

    // 手动刷新再拉一次
    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(getUsage).toHaveBeenCalledWith(2010)
    // 单一数据源：始终使用 /usage API 值
    expect(wrapper.text()).toContain('5h|18|900')
  })

  it('OpenAI OAuth 在无 codex 快照时会回退显示 usage 接口窗口', async () => {
	getUsage.mockResolvedValue({
	  five_hour: {
	    utilization: 0,
	    resets_at: null,
	    remaining_seconds: 0,
	    window_stats: {
	      requests: 2,
	      tokens: 27700,
	      cost: 0.06,
	      standard_cost: 0.06,
	      user_cost: 0.06
	    }
	  },
	  seven_day: {
	    utilization: 0,
	    resets_at: null,
	    remaining_seconds: 0,
	    window_stats: {
	      requests: 2,
	      tokens: 27700,
	      cost: 0.06,
	      standard_cost: 0.06,
	      user_cost: 0.06
	    }
	  }
	})

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2002,
		      platform: 'openai',
		      type: 'oauth',
		      extra: {}
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()

	expect(getUsage).toHaveBeenCalledWith(2002)
	expect(wrapper.text()).toContain('5h|0|27700')
	expect(wrapper.text()).toContain('7d|0|27700')
  })

  it('OpenAI OAuth 在行数据刷新但仍无 codex 快照时会重新拉取 usage', async () => {
	getUsage
	  .mockResolvedValueOnce({
	    five_hour: {
	      utilization: 0,
	      resets_at: null,
	      remaining_seconds: 0,
	      window_stats: {
	        requests: 1,
	        tokens: 100,
	        cost: 0.01,
	        standard_cost: 0.01,
	        user_cost: 0.01
	      }
	    },
	    seven_day: null
	  })
	  .mockResolvedValueOnce({
	    five_hour: {
	      utilization: 0,
	      resets_at: null,
	      remaining_seconds: 0,
	      window_stats: {
	        requests: 2,
	        tokens: 200,
	        cost: 0.02,
	        standard_cost: 0.02,
	        user_cost: 0.02
	      }
	    },
	    seven_day: null
	  })

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2003,
		      platform: 'openai',
		      type: 'oauth',
		      updated_at: '2026-03-07T10:00:00Z',
		      extra: {}
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()
	expect(wrapper.text()).toContain('5h|0|100')
	expect(getUsage).toHaveBeenCalledTimes(1)

	await wrapper.setProps({
	  account: {
	    id: 2003,
	    platform: 'openai',
	    type: 'oauth',
	    updated_at: '2026-03-07T10:01:00Z',
	    extra: {}
	  }
	})

	await flushPromises()
	expect(getUsage).toHaveBeenCalledTimes(2)
	expect(wrapper.text()).toContain('5h|0|200')
  })

  it('OpenAI OAuth 已限额时显示 /usage API 返回的限额数据', async () => {
	getUsage.mockResolvedValue({
	  five_hour: {
	    utilization: 100,
	    resets_at: '2026-03-07T12:00:00Z',
	    remaining_seconds: 3600,
	    window_stats: {
	      requests: 211,
	      tokens: 106540000,
	      cost: 38.13,
	      standard_cost: 38.13,
	      user_cost: 38.13
	    }
	  },
	  seven_day: {
	    utilization: 100,
	    resets_at: '2026-03-13T12:00:00Z',
	    remaining_seconds: 3600,
	    window_stats: {
	      requests: 211,
	      tokens: 106540000,
	      cost: 38.13,
	      standard_cost: 38.13,
	      user_cost: 38.13
	    }
	  }
	})

		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 2004,
		      platform: 'openai',
		      type: 'oauth',
		      rate_limit_reset_at: '2099-03-07T12:00:00Z',
		      extra: {
		        codex_5h_used_percent: 0,
		        codex_7d_used_percent: 0
		      }
		    })
		  },
	  global: {
	    stubs: {
	      UsageProgressBar: {
	        props: ['label', 'utilization', 'resetsAt', 'windowStats', 'color'],
	        template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ windowStats?.tokens }}</div>'
	      },
	      AccountQuotaInfo: true
	    }
	  }
	})

	await flushPromises()

  expect(getUsage).toHaveBeenCalledWith(2004)
  expect(wrapper.text()).toContain('5h|100|106540000')
  expect(wrapper.text()).toContain('7d|100|106540000')
  })

  it('Key 账号会展示 today stats 徽章并带 A/U 提示', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3001,
		      platform: 'anthropic',
		      type: 'apikey'
		    }),
		    todayStats: {
		      requests: 1_000_000,
		      tokens: 1_000_000_000,
		      cost: 12.345,
		      standard_cost: 12.345,
		      user_cost: 6.789
		    }
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text()).toContain('1.0M req')
		expect(wrapper.text()).toContain('1.0B')
		expect(wrapper.text()).toContain('A $12.35')
		expect(wrapper.text()).toContain('U $6.79')

		const badges = wrapper.findAll('span[title]')
		expect(badges.some(node => node.attributes('title') === 'usage.accountBilled')).toBe(true)
		expect(badges.some(node => node.attributes('title') === 'usage.userBilled')).toBe(true)
  })

  it('Grok OAuth 会展示本地 user billed 用量并把耗尽配额显示为 0% 剩余', async () => {
    getUsage.mockResolvedValue({
      grok_local_usage: {
        requests: 4,
        tokens: 1200,
        cost: 0.12,
        standard_cost: 0.12,
        user_cost: 0.34
      },
      grok_request_quota: {
        limit: 10,
        remaining: -2,
        reset_at: '2026-07-09T16:00:00Z'
      },
      grok_quota_snapshot_state: 'observed'
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 3861,
          platform: 'grok',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ resetsAt }}</div>'
          },
          AccountQuotaInfo: true,
          GrokQuotaProbeCell: true
        }
      }
    })

    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(3861)
    expect(wrapper.text()).toContain('4 req')
    expect(wrapper.text()).toContain('1.2K')
    expect(wrapper.text()).toContain('A $0.12')
    expect(wrapper.text()).toContain('U $0.34')
    expect(wrapper.text()).toContain('admin.accounts.usageWindow.grokRequests|0|2026-07-09T16:00:00Z')

    const badges = wrapper.findAll('span[title]')
    expect(badges.some(node => node.attributes('title') === 'usage.accountBilled')).toBe(true)
    expect(badges.some(node => node.attributes('title') === 'usage.userBilled')).toBe(true)
  })

  it('Grok OAuth 配额条按剩余容量显示 100% 满格和 25% 低量', async () => {
    getUsage.mockResolvedValue({
      grok_request_quota: {
        limit: 100,
        remaining: 100,
        reset_at: '2026-07-09T16:00:00Z'
      },
      grok_token_quota: {
        limit: 1000,
        remaining: 250,
        reset_at: '2026-07-09T16:00:00Z'
      },
      grok_quota_snapshot_state: 'observed'
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 4073,
          platform: 'grok',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color', 'remainingCapacity'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}|{{ remainingCapacity }}</div>'
          },
          AccountQuotaInfo: true,
          GrokQuotaProbeCell: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.usageWindow.grokRequests|100|true')
    expect(wrapper.text()).toContain('admin.accounts.usageWindow.grokTokens|25|true')
  })

  it('Key 账号在 today stats loading 时显示骨架屏', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3002,
		      platform: 'anthropic',
		      type: 'apikey'
		    }),
		    todayStats: null,
		    todayStatsLoading: true
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.findAll('.animate-pulse').length).toBeGreaterThan(0)
  })

  it('Key 账号在无 today stats 且无配额时显示兜底短横线', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 3003,
		      platform: 'anthropic',
		      type: 'apikey',
		      quota_limit: 0,
		      quota_daily_limit: 0,
		      quota_weekly_limit: 0
		    }),
		    todayStats: null,
		    todayStatsLoading: false
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text().trim()).toBe('-')
  })

  it('Vertex 账号会在 Gemini 用量窗口里展示 today stats 徽章', async () => {
		const wrapper = mount(AccountUsageCell, {
		  props: {
		    account: makeAccount({
		      id: 4001,
		      platform: 'gemini',
		      type: 'service_account',
          credentials: {
            tier_id: 'vertex',
            project_id: 'vertex-proj',
            client_email: 'svc@vertex-proj.iam.gserviceaccount.com',
            location: 'global'
          },
		      extra: {}
		    }),
		    todayStats: {
		      requests: 0,
		      tokens: 0,
		      cost: 0,
		      standard_cost: 0,
		      user_cost: 0
		    }
		  },
		  global: {
		    stubs: {
		      UsageProgressBar: true,
		      AccountQuotaInfo: true
		    }
		  }
		})

		await flushPromises()

		expect(wrapper.text()).toContain('0 req')
		expect(wrapper.text()).toContain('0')
		expect(wrapper.text()).toContain('A $0.00')
		expect(wrapper.text()).toContain('U $0.00')
  })

  it('Anthropic OAuth 会渲染 7d F (Fable) 进度条，且 7d S 逻辑保留', async () => {
    getUsage.mockResolvedValue({
      source: 'passive',
      five_hour: {
        utilization: 41,
        resets_at: '2026-07-03T10:00:00Z',
        remaining_seconds: 3600
      },
      seven_day: {
        utilization: 56,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      },
      seven_day_sonnet: {
        utilization: 30,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      },
      seven_day_fable: {
        utilization: 100,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 3001,
          platform: 'anthropic',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
          GrokQuotaProbeCell: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('5h|41')
    expect(wrapper.text()).toContain('7d|56')
    expect(wrapper.text()).toContain('7d S|30')
    expect(wrapper.text()).toContain('7d F|100')
  })

  it('Anthropic OAuth 无 Fable 数据时不渲染 7d F 进度条', async () => {
    getUsage.mockResolvedValue({
      source: 'passive',
      five_hour: {
        utilization: 41,
        resets_at: '2026-07-03T10:00:00Z',
        remaining_seconds: 3600
      },
      seven_day: {
        utilization: 56,
        resets_at: '2026-07-06T22:00:00Z',
        remaining_seconds: 300000
      }
    })

    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          id: 3002,
          platform: 'anthropic',
          type: 'oauth',
          extra: {}
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'utilization', 'resetsAt', 'color'],
            template: '<div class="usage-bar">{{ label }}|{{ utilization }}</div>'
          },
          AccountQuotaInfo: true,
          GrokQuotaProbeCell: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('5h|41')
    expect(wrapper.text()).toContain('7d|56')
    expect(wrapper.text()).not.toContain('7d S')
    expect(wrapper.text()).not.toContain('7d F')
  })
})
