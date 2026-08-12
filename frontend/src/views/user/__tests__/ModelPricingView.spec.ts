import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModelPricingView from '../ModelPricingView.vue'
import type { UserAvailableChannel } from '@/api/channels'
import { BILLING_MODE_TOKEN } from '@/constants/channel'

const { getAvailable, getUserGroupRates, showError, extractApiErrorMessage } = vi.hoisted(() => ({
  getAvailable: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  extractApiErrorMessage: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.refresh': 'Refresh',
  'modelPricing.searchPlaceholder': 'Search pricing',
  'modelPricing.empty': 'No model pricing data',
  'modelPricing.modes.raw': 'Standard Price',
  'modelPricing.modes.actual': 'Actual Price',
  'modelPricing.columns.channel': 'Channel',
  'modelPricing.columns.platform': 'Platform',
  'modelPricing.columns.model': 'Model',
  'modelPricing.columns.contextTier': 'Context Tier',
  'modelPricing.columns.group': 'Group',
  'modelPricing.columns.groupMultiplier': 'Group Multiplier',
  'modelPricing.columns.promotion': 'Limited Promotion',
  'modelPricing.columns.finalMultiplier': 'Final Multiplier',
  'modelPricing.columns.source': 'Source',
  'modelPricing.columns.billingMode': 'Billing Mode',
  'modelPricing.columns.inputPerMillion': 'Input/M',
  'modelPricing.columns.outputPerMillion': 'Output/M',
  'modelPricing.columns.cacheWritePerMillion': 'Cache Write/M',
  'modelPricing.columns.cacheReadPerMillion': 'Cache Read/M',
  'modelPricing.columns.unitPrice': 'Per Request/Image',
  'modelPricing.sources.channel': 'Channel Pricing',
  'modelPricing.promotions.usageBonus': '{multiplier} usage',
  'modelPricing.promotions.priceMultiplier': 'Price {multiplier}',
  'modelPricing.promotions.detail': 'Standard price × group multiplier {group} × promotion price multiplier {promotion} = final multiplier {final}',
  'modelPricing.billingModes.token': 'Per Token',
  'modelPricing.contextTiers.all': 'All contexts',
  'modelPricing.contextTiers.upTo': 'Up to {tokens}',
  'modelPricing.contextTiers.above': 'Above {tokens}',
  'modelPricing.contextTiers.range': '{min} to {max}',
  'modelPricing.units.request': 'req',
  'modelPricing.units.image': 'img',
  'availableChannels.exclusive': 'Exclusive',
}

vi.mock('@/api/channels', () => ({
  default: { getAvailable },
  userChannelsAPI: { getAvailable },
}))

vi.mock('@/api/groups', () => ({
  default: { getUserGroupRates },
  userGroupsAPI: { getUserGroupRates },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, paramsOrFallback?: string | Record<string, string>) => {
        const fallback = typeof paramsOrFallback === 'string' ? paramsOrFallback : key
        const template = messages[key] ?? fallback
        if (typeof paramsOrFallback !== 'object') return template
        return Object.entries(paramsOrFallback).reduce(
          (text, [name, value]) => text.replace(`{${name}}`, value),
          template,
        )
      },
      te: (key: string) => key in messages,
    }),
  }
})

const AppLayoutStub = { template: '<main><slot /></main>' }
const TablePageLayoutStub = {
  template: '<section><slot name="filters" /><slot name="table" /></section>',
}
const IconStub = {
  props: ['name', 'size'],
  template: '<span class="icon-stub" :data-icon="name" />',
}
const PlatformIconStub = {
  props: ['platform', 'size'],
  template: '<span class="platform-icon-stub" :data-platform="platform" />',
}
const GroupBadgeStub = {
  props: ['name', 'platform', 'subscriptionType', 'rateMultiplier', 'userRateMultiplier'],
  template: '<span class="group-badge-stub">{{ name }} {{ userRateMultiplier ?? rateMultiplier }}x</span>',
}

function makeChannel(): UserAvailableChannel[] {
  return [
    {
      name: 'Gateway A',
      description: 'Primary channel',
      platforms: [
        {
          platform: 'opencode_go',
          groups: [
            {
              id: 20,
              name: 'Enterprise',
              platform: 'opencode_go',
              subscription_type: 'subscription',
              rate_multiplier: 2,
              peak_rate_enabled: false,
              peak_start: '',
              peak_end: '',
              peak_rate_multiplier: 1,
              is_exclusive: true,
            },
          ],
          supported_models: [
            {
              name: 'deepseek-v4-flash',
              platform: 'opencode_go',
              pricing: {
                billing_mode: BILLING_MODE_TOKEN,
                input_price: 0.000001,
                output_price: 0.000002,
                cache_write_price: 0.0000004,
                cache_read_price: 0,
                image_output_price: null,
                per_request_price: null,
                pricing_source: 'channel',
                pricing_source_label: 'modelPricing.sources.channel',
                pricing_source_detail: 'channel_model_pricing',
                intervals: [],
              },
              promotion: {
                code: 'opencode_go_usage_bonus',
                cost_multiplier: 0.5,
                usage_multiplier: 2,
              },
            },
          ],
        },
      ],
    },
  ]
}

function mountView() {
  return mount(ModelPricingView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        Icon: IconStub,
        PlatformIcon: PlatformIconStub,
        GroupBadge: GroupBadgeStub,
      },
    },
  })
}

describe('ModelPricingView', () => {
  beforeEach(() => {
    getAvailable.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    extractApiErrorMessage.mockReset()
    extractApiErrorMessage.mockReturnValue('Load failed')
  })

  it('loads pricing rows, refreshes, and switches between actual and raw prices', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({ 20: 0.5 })

    const wrapper = mountView()
    await flushPromises()

    expect(getAvailable).toHaveBeenCalledTimes(1)
    expect(getAvailable).toHaveBeenLastCalledWith({ purpose: 'model_pricing' })
    expect(getUserGroupRates).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Gateway A')
    expect(wrapper.text()).toContain('deepseek-v4-flash')
    expect(wrapper.text()).toContain('Enterprise')
    expect(wrapper.text()).toContain('Channel Pricing')
    expect(wrapper.text()).toContain('2x usage')
    expect(wrapper.text()).toContain('Price 0.5x')
    expect(wrapper.text()).toContain('0.25x')
    expect(wrapper.text()).toContain('$0.25')

    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(getAvailable).toHaveBeenCalledTimes(2)
    expect(getAvailable).toHaveBeenLastCalledWith({ purpose: 'model_pricing' })

    await wrapper.get('button:nth-of-type(1)').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('$1')
  })

  it('renders every context tier and its token prices', async () => {
    const channels = makeChannel()
    const pricing = channels[0].platforms[0].supported_models[0].pricing
    channels[0].platforms[0].supported_models[0].promotion = null
    if (!pricing) throw new Error('test pricing is required')
    pricing.intervals = [
      {
        min_tokens: 0,
        max_tokens: 256000,
        tier_label: '',
        input_price: 0.4e-6,
        output_price: 1.6e-6,
        cache_write_price: 0.5e-6,
        cache_read_price: 0.04e-6,
        per_request_price: null,
      },
      {
        min_tokens: 256000,
        max_tokens: null,
        tier_label: '',
        input_price: 1.2e-6,
        output_price: 4.8e-6,
        cache_write_price: 1.5e-6,
        cache_read_price: 0.12e-6,
        per_request_price: null,
      },
    ]
    getAvailable.mockResolvedValue(channels)
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Context Tier')
    expect(wrapper.text()).toContain('Up to 256K')
    expect(wrapper.text()).toContain('Above 256K')
    expect(wrapper.text()).toContain('$0.8')
    expect(wrapper.text()).toContain('$2.4')
    expect(wrapper.text()).toContain('$0.08')
    expect(wrapper.text()).toContain('$0.24')

    await wrapper.get('button:nth-of-type(1)').trigger('click')
    expect(wrapper.text()).toContain('$0.4')
    expect(wrapper.text()).toContain('$1.2')
    expect(wrapper.text()).toContain('$0.04')
    expect(wrapper.text()).toContain('$0.12')
  })

  it('shows an app error when available channels fail to load', async () => {
    const err = new Error('network down')
    getAvailable.mockRejectedValue(err)
    getUserGroupRates.mockResolvedValue({})

    mountView()
    await flushPromises()

    expect(extractApiErrorMessage).toHaveBeenCalledWith(err, 'common.error')
    expect(showError).toHaveBeenCalledWith('Load failed')
  })

  it('keeps the table header sticky while the pricing table scrolls', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    const header = wrapper.get('thead')
    expect(header.classes()).toEqual(expect.arrayContaining(['sticky', 'top-0', 'z-20']))
  })

  it('keeps the model column sticky while the pricing table scrolls horizontally', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    const stickyModelCells = wrapper.findAll('.model-pricing-sticky-model')
    expect(stickyModelCells).toHaveLength(2)
    expect(stickyModelCells[0].element.tagName).toBe('TH')
    expect(stickyModelCells[1].element.tagName).toBe('TD')
  })
})
