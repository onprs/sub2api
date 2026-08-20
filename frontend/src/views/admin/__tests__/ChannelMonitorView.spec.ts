import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ChannelMonitorView from '../ChannelMonitorView.vue'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  update: vi.fn(),
  runNow: vi.fn(),
  del: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      list: mocks.list,
      update: mocks.update,
      runNow: mocks.runNow,
      del: mocks.del,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key === 'admin.channelMonitor.form.targetExternal' ? '外站' : key,
    }),
  }
})

vi.mock('@/composables/usePersistedPageSize', () => ({
  getPersistedPageSize: () => 20,
}))

vi.mock('@/composables/useChannelMonitorFormat', () => ({
  useChannelMonitorFormat: () => ({
    providerLabel: (provider: string) => provider,
    providerBadgeClass: () => '',
    formatLatency: (latency: number | null) => latency == null ? '-' : String(latency),
    formatAvailability: (monitor: ChannelMonitor) => `${monitor.availability_7d}%`,
  }),
}))

const AppLayoutStub = {
  name: 'AppLayout',
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  name: 'TablePageLayout',
  template: '<main><slot name="filters" /><slot name="table" /><slot name="pagination" /></main>',
}

const DataTableStub = {
  name: 'DataTable',
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" class="monitor-row">
        <slot name="cell-target" :row="row" />
      </div>
    </div>
  `,
}

const IconStub = {
  name: 'Icon',
  props: ['name'],
  template: '<i :data-icon="name" />',
}

function monitor(overrides: Partial<ChannelMonitor>): ChannelMonitor {
  return {
    id: 1,
    name: 'Monitor',
    provider: 'openai',
    api_mode: 'chat_completions',
    target_type: 'local',
    group_id: 20,
    endpoint: '',
    api_key_masked: '',
    api_key_decrypt_failed: false,
    primary_model: 'gpt-5.4',
    extra_models: [],
    group_name: 'OpenAI 主线路',
    enabled: true,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 0,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    ...overrides,
  }
}

describe('ChannelMonitorView 监控目标', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({
      items: [
        monitor({ id: 34 }),
        monitor({
          id: 51,
          target_type: 'external',
          group_id: null,
          endpoint: 'https://api.partner.example',
          group_name: '合作站',
        }),
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1,
    })
  })

  it('分别显示本站分组和外站 endpoint', async () => {
    const wrapper = mount(ChannelMonitorView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          DataTable: DataTableStub,
          Icon: IconStub,
          MonitorFiltersBar: { name: 'MonitorFiltersBar', template: '<div />' },
          MonitorFormDialog: { name: 'MonitorFormDialog', template: '<div />' },
          MonitorTemplateManagerDialog: { name: 'MonitorTemplateManagerDialog', template: '<div />' },
          MonitorRunResultDialog: { name: 'MonitorRunResultDialog', template: '<div />' },
          ConfirmDialog: { name: 'ConfirmDialog', template: '<div />' },
          Pagination: { name: 'Pagination', template: '<div />' },
          EmptyState: { name: 'EmptyState', template: '<div />' },
          HelpTooltip: { name: 'HelpTooltip', template: '<span><slot /></span>' },
          Toggle: { name: 'Toggle', template: '<span />' },
          MonitorPrimaryModelCell: { name: 'MonitorPrimaryModelCell', template: '<span />' },
          MonitorActionsCell: { name: 'MonitorActionsCell', template: '<span />' },
        },
      },
    })
    await flushPromises()

    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('OpenAI 主线路')
    expect(wrapper.text()).toContain('外站 · https://api.partner.example')
    expect(wrapper.find('[data-icon="server"]').exists()).toBe(true)
    expect(wrapper.find('[data-icon="globe"]').exists()).toBe(true)
  })
})
