import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getConfig, getStatus, listLogs, getGroups } = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  getGroups: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    ...actual,
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      getConfig,
      getStatus,
      listLogs,
      updateConfig: vi.fn(),
      testAPIKeys: vi.fn(),
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes: vi.fn(),
      unbanUser: vi.fn()
    },
    groups: { getAll: getGroups },
    proxies: { getAll: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))

import RiskControlView from '../RiskControlView.vue'
import { testLocale, translateLocaleMessage } from '@/i18n/__tests__/testTranslator'

const config = {
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  thresholds: { 'harassment/threatening': 0.9 },
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: 'blocked',
  email_on_hit: true,
  auto_ban_enabled: true,
  ban_threshold: 10,
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: [],
  keyword_blocking_mode: 'keyword_and_api',
  model_filter: { type: 'all', models: [] },
  cyber_policy_exclude_from_ban_count: false
}

const runtime = {
  enabled: true,
  risk_control_enabled: true,
  mode: 'pre_block',
  worker_count: 4,
  max_workers: 32,
  active_workers: 0,
  idle_workers: 4,
  queue_size: 32768,
  queue_length: 0,
  queue_usage_percent: 0,
  enqueued: 0,
  dropped: 0,
  processed: 0,
  errors: 0,
  pre_block_active: 0,
  pre_block_checked: 0,
  pre_block_allowed: 0,
  pre_block_blocked: 0,
  pre_block_errors: 0,
  pre_block_avg_latency_ms: 0,
  pre_block_api_key_active: 0,
  pre_block_api_key_available_count: 0,
  pre_block_api_key_total_calls: 0,
  pre_block_api_key_loads: [],
  api_key_statuses: [],
  flagged_hash_count: 0,
  last_cleanup_deleted_hit: 0,
  last_cleanup_deleted_non_hit: 0
}

const log = {
  id: 1,
  request_id: 'req-1',
  user_id: 1,
  user_email: 'user@example.com',
  api_key_id: 2,
  api_key_name: 'key',
  group_id: 3,
  group_name: 'group',
  endpoint: '/v1/responses',
  provider: 'openai',
  model: 'gpt-5.5',
  mode: 'pre_block',
  action: 'block',
  flagged: true,
  highest_category: 'harassment/threatening',
  highest_score: 0.95,
  matched_keyword: '',
  category_scores: { 'harassment/threatening': 0.95 },
  threshold_snapshot: { 'harassment/threatening': 0.9 },
  input_excerpt: 'input',
  upstream_latency_ms: 20,
  error: '',
  violation_count: 1,
  auto_banned: false,
  email_sent: false,
  user_status: 'active',
  queue_delay_ms: 0,
  created_at: '2026-01-01T00:00:00Z'
}

async function mountView() {
  const wrapper = mount(RiskControlView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>'
        },
        Icon: true,
        Select: true,
        Toggle: true,
        Pagination: true,
        ModelWhitelistSelector: true,
        ProxySelector: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('RiskControlView category labels', () => {
  beforeEach(() => {
    testLocale.value = 'en'
    getConfig.mockResolvedValue(config)
    getStatus.mockResolvedValue(runtime)
    listLogs.mockResolvedValue({ items: [log], total: 1, page: 1, page_size: 20, pages: 1 })
    getGroups.mockResolvedValue([])
  })

  it('localizes log and threshold categories in English and Chinese', async () => {
    const english = await mountView()
    expect(english.text()).toContain('Harassment / Threatening')
    expect(english.text()).not.toContain('harassment/threatening')

    const settingsButton = english.findAll('button').find(button =>
      button.text().includes(translateLocaleMessage('admin.riskControl.openSettings'))
    )
    expect(settingsButton).toBeDefined()
    await settingsButton!.trigger('click')
    const thresholdTab = english.findAll('button').find(button =>
      button.text().includes(translateLocaleMessage('admin.riskControl.tabs.riskThresholds'))
    )
    expect(thresholdTab).toBeDefined()
    await thresholdTab!.trigger('click')
    expect(english.text()).toContain('Self-harm / Instructions')

    testLocale.value = 'zh'
    const chinese = await mountView()
    expect(chinese.text()).toContain('骚扰 / 威胁')
    expect(chinese.text()).not.toContain('harassment/threatening')
  })
})
