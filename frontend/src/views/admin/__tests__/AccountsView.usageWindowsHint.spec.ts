import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getBatchUsage,
  getUsage,
  getAllProxies,
  getAllGroups
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getBatchUsage: vi.fn(),
  getUsage: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getBatchUsage,
      getUsage,
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
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

// Render the per-column header slots so we can assert the usage-window header hint.
const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <div data-test="data-table">
      <template v-for="column in columns" :key="column.key">
        <div v-if="column.key === 'usage'" data-test="usage-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
        <div v-if="column.key === 'upstream_billing_rate'" data-test="upstream-billing-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
      </template>
      <div v-for="row in data" :key="'usage-' + row.id" data-test="account-usage">
        <slot name="cell-usage" :row="row" />
      </div>
      <div v-for="row in data" :key="'rate-' + row.id" data-test="account-rate">
        <slot name="cell-rate_multiplier" :row="row" />
      </div>
    </div>
  `
}

// Expose the content passed to HelpTooltip without dealing with its <Teleport>.
const HelpTooltipStub = {
  props: ['content', 'widthClass'],
  template: '<span data-test="usage-windows-hint">{{ content }}</span>'
}

const AccountUsageCellStub = {
  props: ['account', 'requestBatchedUsage'],
  template: `
    <button
      type="button"
      data-test="request-account-usage"
      :data-platform="account.platform"
      :data-batch-managed="typeof requestBatchedUsage === 'function'"
      @click="requestBatchedUsage && requestBatchedUsage(account)"
    />
  `
}

function mountView(options: { renderAccountUsageCell?: boolean } = {}) {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: HelpTooltipStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: {
          props: ['groups'],
          template: '<div data-test="account-filters" :data-group-count="groups.length"></div>'
        },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: options.renderAccountUsageCell ? false : AccountUsageCellStub,
        Icon: true
      }
    }
  })
}

describe('admin AccountsView usage windows hint', () => {
  beforeEach(() => {
    localStorage.clear()

    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getBatchUsage.mockReset()
    getUsage.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()

    listAccounts.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
    listWithEtag.mockResolvedValue({
      notModified: true,
      etag: null,
      data: null
    })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getBatchUsage.mockResolvedValue({ usage: {}, errors: {} })
    getUsage.mockResolvedValue({ upstream_balance: { status: 'unsupported' } })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('keeps groups available when loading proxies fails', async () => {
    getAllProxies.mockRejectedValue(new Error('proxy service unavailable'))
    getAllGroups.mockResolvedValue([{ id: 7, name: 'production' }])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="account-filters"]').attributes('data-group-count')).toBe('1')
  })

  it('renders an explanatory tooltip next to the usage windows column header', async () => {
    const wrapper = mountView()
    await flushPromises()

    const header = wrapper.find('[data-test="usage-header"]')
    expect(header.exists()).toBe(true)
    // Column label is still shown alongside the help icon.
    expect(header.text()).toContain('admin.accounts.columns.usageWindows')

    const hint = wrapper.find('[data-test="usage-windows-hint"]')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toBe('admin.accounts.usageWindowsHint')
  })

  it('批量加载滚动用量和 OpenAI 实时余额，但只缓存滚动窗口结果', async () => {
    getBatchUsage.mockResolvedValueOnce({
      usage: {
        '81': { updated_at: null, five_hour: { utilization: 10 } },
        '82': { updated_at: null, five_hour: { utilization: 20 } },
        '83': {
          updated_at: '2026-09-04T00:00:00Z',
          upstream_balance: {
            status: 'available',
            source: 'sub2api',
            kind: 'wallet',
            amount: 12.34,
            unit: 'USD'
          }
        }
      },
      errors: {}
    })
    listAccounts.mockResolvedValueOnce({
      items: [
        {
          id: 81,
          name: 'OpenCode usage account',
          platform: 'opencode_go',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          extra: {},
          created_at: '2026-09-04T00:00:00Z',
          updated_at: '2026-09-04T00:00:00Z'
        },
        {
          id: 82,
          name: 'Command Code usage account',
          platform: 'commandcode',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          extra: {},
          created_at: '2026-09-04T00:00:00Z',
          updated_at: '2026-09-04T00:00:00Z'
        },
        {
          id: 83,
          name: 'Live balance account',
          platform: 'openai',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          extra: {},
          created_at: '2026-09-04T00:00:00Z',
          updated_at: '2026-09-04T00:00:00Z'
        }
      ],
      total: 3,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const usageButtons = wrapper.findAll('[data-test="request-account-usage"]')
    expect(usageButtons).toHaveLength(3)
    expect(usageButtons[0].attributes('data-batch-managed')).toBe('true')
    expect(usageButtons[1].attributes('data-batch-managed')).toBe('true')
    expect(usageButtons[2].attributes('data-batch-managed')).toBe('true')

    await Promise.all(usageButtons.map(button => button.trigger('click')))

    await vi.waitFor(() => {
      expect(getBatchUsage).toHaveBeenCalledTimes(1)
    })
    expect(getBatchUsage).toHaveBeenLastCalledWith([81, 82, 83], false)

    await Promise.all(usageButtons.map(button => button.trigger('click')))

    await vi.waitFor(() => {
      expect(getBatchUsage).toHaveBeenCalledTimes(2)
    })
    expect(getBatchUsage).toHaveBeenLastCalledWith([83], false)
  })

  it('账号表通过批量接口展示 OpenAI API Key 的实时上游余额', async () => {
    listAccounts.mockResolvedValueOnce({
      items: [{
        id: 84,
        name: 'Live upstream balance account',
        platform: 'openai',
        type: 'apikey',
        status: 'active',
        schedulable: true,
        extra: {},
        created_at: '2026-09-04T00:00:00Z',
        updated_at: '2026-09-04T00:00:00Z'
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getBatchUsage.mockResolvedValueOnce({
      usage: {
        '84': {
          updated_at: '2026-09-04T00:00:01Z',
          upstream_balance: {
            status: 'available',
            source: 'sub2api',
            kind: 'wallet',
            amount: 18.75,
            unit: 'USD'
          }
        }
      },
      errors: {}
    })

    const wrapper = mountView({ renderAccountUsageCell: true })

    await vi.waitFor(() => {
      expect(getBatchUsage).toHaveBeenCalledWith([84], false)
    })
    await flushPromises()

    expect(getUsage).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="account-usage"]').text()).toContain('admin.accounts.upstreamBalance.wallet')
    expect(wrapper.get('[data-test="account-usage"]').text()).toMatch(/18[.,]75/)
  })

  it('keeps Ollama Cloud in the single usage column and ignores legacy column preferences', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['ollama_cloud_usage']))
    const wrapper = mountView()
    await flushPromises()

    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{ key: string }>
    expect(columns.filter(column => column.key === 'usage')).toHaveLength(1)
    expect(columns.some(column => column.key === 'ollama_cloud_usage')).toBe(false)
  })

  it('renders the upstream billing trust warning next to the declared-rate column', async () => {
    const wrapper = mountView()
    await flushPromises()

    const header = wrapper.find('[data-test="upstream-billing-header"]')
    expect(header.exists()).toBe(true)
    expect(header.text()).toContain('admin.accounts.columns.upstreamBillingRate')
    expect(wrapper.findAll('[data-test="usage-windows-hint"]').some(node =>
      node.text() === 'admin.accounts.upstreamBilling.trustWarning'
    )).toBe(true)
    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{ key: string; sortable: boolean }>
    expect(columns.find(column => column.key === 'upstream_billing_rate')?.sortable).toBe(true)
  })

  it('shows account multipliers with enough precision to match declared rates', async () => {
    listAccounts.mockResolvedValueOnce({
      items: [{
        id: 7,
        name: 'precision-account',
        platform: 'gemini',
        type: 'apikey',
        status: 'active',
        schedulable: true,
        rate_multiplier: 0.065,
        extra: {
          upstream_billing_probe_enabled: true,
          upstream_billing_rate_sync_enabled: true
        },
        created_at: '2026-07-13T00:00:00Z',
        updated_at: '2026-07-13T00:00:00Z'
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="account-rate"]').text()).toBe('0.065x')
    const indicator = wrapper.get('[data-testid="account-rate-sync-indicator"]')
    expect(indicator.attributes('title')).toBe('admin.accounts.upstreamBilling.syncedRateTooltip')
  })
})
