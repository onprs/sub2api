import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'
import ChannelsView from '@/views/admin/ChannelsView.vue'

const { listChannels, createChannel, getGroups, syncPricingModels, showSuccess, showError } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  createChannel: vi.fn(),
  getGroups: vi.fn(),
  syncPricingModels: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, create: createChannel, syncPricingModels },
    groups: { getAll: getGroups },
    settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }
  }
}))

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        }),
        PricingEntryCard: defineComponent({
          props: ['entry'],
          template: '<div class="pricing-entry">{{ entry.models.join(", ") }}</div>'
        }),
        DataTable: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        Toggle: true
      }
    }
  })
}

async function openDialog(wrapper: ReturnType<typeof mountView>) {
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.channels.createChannel')!.trigger('click')
  await flushPromises()
}

async function enablePlatform(wrapper: ReturnType<typeof mountView>, platform: string) {
  const label = wrapper.findAll('label').find(label => label.text() === `admin.groups.platforms.${platform}`)
  expect(label).toBeDefined()
  await label!.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button.channel-tab').find(button => button.text() === `admin.groups.platforms.${platform}`)!.trigger('click')
}

function visibleSyncButtons(wrapper: ReturnType<typeof mountView>) {
  return wrapper.findAll('button').filter(button => button.isVisible() && [
    'admin.channels.form.syncLatestModels',
    'admin.channels.form.syncingModels'
  ].includes(button.text()))
}

describe('ChannelsView pricing model sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    createChannel.mockResolvedValue({ id: 1 })
    getGroups.mockResolvedValue([])
    syncPricingModels.mockReset()
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it('保存 OpenCode 规则时使用渠道定价来源，不提交重复的模型价格', async () => {
    getGroups.mockResolvedValue([{ id: 1, name: 'OpenCode 分组', platform: 'opencode_go', rate_multiplier: 1, status: 'active' }])
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'opencode_go')
    await wrapper.get('input[placeholder="admin.channels.form.namePlaceholder"]').setValue('OpenCode')
    const group = wrapper.findAll('label').find(label => label.isVisible() && label.text().includes('OpenCode 分组'))
    expect(group).toBeDefined()
    await group!.get('input[type="checkbox"]').setValue(true)
    const addRule = wrapper.findAll('button').find(button => button.isVisible() && button.text().includes('admin.channels.form.addRule'))
    expect(addRule).toBeDefined()
    await addRule!.trigger('click')
    const source = wrapper.get('select')
    expect((source.element as HTMLSelectElement).value).toBe('channel_model_multiplier')
    expect(wrapper.text()).toContain('admin.channels.form.channelRulePricingHint')
    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(createChannel).toHaveBeenCalledWith(expect.objectContaining({
      account_stats_pricing_rules: [expect.objectContaining({
        pricing_source: 'channel_model_multiplier',
        platform: 'opencode_go',
        group_ids: [],
        account_ids: [],
        pricing: []
      })]
    }))
    wrapper.unmount()
  })

  it.each([
    ['openai', true],
    ['opencode_go', true],
    ['typesafe', true],
    ['command_code', false],
    ['cline', false]
  ] as const)('offers pricing model sync for %s only when its pricing catalog exists', async (platform, expected) => {
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform)

    expect(visibleSyncButtons(wrapper)).toHaveLength(expected ? 1 : 0)
    expect(syncPricingModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reacts to a new platform gaining a pricing catalog', async () => {
    const platform = { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, platform]
    })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform.id)
    expect(visibleSyncButtons(wrapper)).toHaveLength(0)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, { ...platform, litellm_provider: 'acme' }]
    })
    await flushPromises()
    expect(visibleSyncButtons(wrapper)).toHaveLength(1)
    wrapper.unmount()
  })

  it('blocks additional syncs while one request is in flight and adds its models once', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    syncPricingModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await enablePlatform(wrapper, 'opencode_go')

    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    await visibleSyncButtons(wrapper)[0].trigger('click')
    expect(syncPricingModels).toHaveBeenCalledOnce()
    expect(syncPricingModels).toHaveBeenCalledWith('openai')

    resolveSync({ models: ['gpt-latest'] })
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(1)
    expect(wrapper.get('.pricing-entry').text()).toBe('gpt-latest')
    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeUndefined()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
