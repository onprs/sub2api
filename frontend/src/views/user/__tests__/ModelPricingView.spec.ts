import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ModelPricingView from '../ModelPricingView.vue'
import type { UserAvailableChannel } from '@/api/channels'
import { BILLING_MODE_TOKEN } from '@/constants/channel'

const { getAvailable, getUserGroupRates, showError, showSuccess, extractApiErrorMessage, copyToClipboard } = vi.hoisted(() => ({
  getAvailable: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  extractApiErrorMessage: vi.fn(),
  copyToClipboard: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.refresh': 'Refresh',
  'common.close': 'Close',
  'modelPricing.selection.platform': 'Available Platform',
  'modelPricing.selection.group': 'Available Group',
  'modelPricing.selection.platformPlaceholder': 'Select a platform',
  'modelPricing.selection.groupPlaceholder': 'Select a group',
  'modelPricing.searchPlaceholder': 'Search pricing',
  'modelPricing.empty': 'No model pricing data',
  'modelPricing.columns.channel': 'Channel',
  'modelPricing.columns.platform': 'Platform',
  'modelPricing.columns.model': 'Model',
  'modelPricing.copyModelId': 'Copy Model ID',
  'modelPricing.modelCopied': 'Model ID copied',
  'modelPricing.modes.raw': 'Original Billing',
  'modelPricing.modes.actual': 'Actual Billing',
  'modelPricing.help.open': 'View pricing guide',
  'modelPricing.help.title': 'Model Pricing Guide',
  'modelPricing.help.priceModes.formula': 'Actual token unit price = Original token unit price × Effective multiplier',
  'modelPricing.help.multipliers.title': 'How actual multipliers are formed',
  'modelPricing.help.contextTiers.title': 'Context tiers',
  'modelPricing.help.timeBands.title': 'Peak and off-peak original prices',
  'modelPricing.help.offers.title': 'Official quota offers',
  'modelPricing.help.offers.noPricingEffect': 'The offer is reflected in actual prices, charges, and Usage History.',
  'modelPricing.columns.contextTier': 'Pricing Period / Context Tier',
  'modelPricing.columns.group': 'Group',
  'modelPricing.columns.groupMultiplier': 'Group Multiplier',
  'modelPricing.columns.modelSpecificMultiplier': 'Offer-Adjusted Model Multiplier',
  'modelPricing.columns.effectiveMultiplier': 'Effective Multiplier',
  'modelPricing.columns.usageOffer': 'Official Quota Offer',
  'modelPricing.columns.source': 'Source',
  'modelPricing.columns.billingMode': 'Billing Mode',
  'modelPricing.columns.inputPerMillion': 'Input/M',
  'modelPricing.columns.outputPerMillion': 'Output/M',
  'modelPricing.columns.cacheWritePerMillion': 'Cache Write/M',
  'modelPricing.columns.cacheReadPerMillion': 'Cache Read/M',
  'modelPricing.columns.unitPrice': 'Per Request/Image',
  'modelPricing.sources.channel': 'Channel Pricing',
  'modelPricing.usageOffers.multiplier': '{multiplier} usage limits',
  'modelPricing.usageOffers.detail': 'Included in actual prices and charges',
  'modelPricing.timeBands.off_peak': 'Off-Peak',
  'modelPricing.timeBands.peak': 'Peak',
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
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
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
  props: {
    showTable: { type: Boolean, default: true },
  },
  template:
    '<section><slot name="filters" /><div v-if="showTable"><slot name="table" /></div></section>',
}
const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'placeholder', 'disabled'],
  emits: ['update:modelValue'],
  template: '<button type="button" class="select-stub" :disabled="disabled">{{ placeholder }}</button>',
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
              peak_rate_enabled: true,
              peak_start: '14:00',
              peak_end: '18:00',
              peak_rate_multiplier: 3,
              current_peak_multiplier: 3,
              is_exclusive: true,
            },
          ],
          supported_models: [
            {
              name: 'deepseek-v4-flash',
              platform: 'opencode_go',
              model_specific_multiplier: 1,
              usage_offer: {
                code: 'opencode_go_usage_offer',
                usage_multiplier: 2,
              },
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
                time_bands: [
                  {
                    code: 'off_peak',
                    time_zone: 'UTC',
                    time_ranges: ['00:00-01:00', '04:00-06:00', '10:00-24:00'],
                    input_price: 0.22e-6,
                    output_price: 0.66e-6,
                    cache_write_price: null,
                    cache_read_price: 0.007e-6,
                  },
                  {
                    code: 'peak',
                    time_zone: 'UTC',
                    time_ranges: ['01:00-04:00', '06:00-10:00'],
                    input_price: 0.44e-6,
                    output_price: 1.32e-6,
                    cache_write_price: null,
                    cache_read_price: 0.014e-6,
                  },
                ],
              },
            },
          ],
        },
        {
          platform: 'gemini',
          groups: [
            {
              id: 30,
              name: 'Free',
              platform: 'gemini',
              subscription_type: 'standard',
              rate_multiplier: 0,
              peak_rate_enabled: false,
              peak_start: '',
              peak_end: '',
              peak_rate_multiplier: 1,
              current_peak_multiplier: 1,
              is_exclusive: false,
            },
          ],
          supported_models: [
            {
              name: 'gemini-2.5-flash',
              platform: 'gemini',
              pricing: {
                billing_mode: BILLING_MODE_TOKEN,
                input_price: 0.000003,
                output_price: 0.000004,
                cache_write_price: null,
                cache_read_price: 0,
                image_output_price: null,
                per_request_price: null,
                pricing_source: 'channel',
                pricing_source_label: 'modelPricing.sources.channel',
                pricing_source_detail: 'channel_model_pricing',
                intervals: [],
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
        Select: SelectStub,
        Icon: IconStub,
        PlatformIcon: PlatformIconStub,
        GroupBadge: GroupBadgeStub,
      },
    },
  })
}

async function selectPricingScope(
  wrapper: ReturnType<typeof mountView>,
  platform = 'opencode_go',
  groupId = 20,
) {
  const [platformSelect, groupSelect] = wrapper.findAllComponents({ name: 'Select' })
  platformSelect.vm.$emit('update:modelValue', platform)
  await wrapper.vm.$nextTick()
  groupSelect.vm.$emit('update:modelValue', groupId)
  await wrapper.vm.$nextTick()
}

describe('ModelPricingView', () => {
  beforeEach(() => {
    getAvailable.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    extractApiErrorMessage.mockReset()
    extractApiErrorMessage.mockReturnValue('Load failed')
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('requires a platform and matching group before showing pricing rows', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({ 20: 0.5 })

    const wrapper = mountView()
    await flushPromises()

    const [platformSelect, groupSelect] = wrapper.findAllComponents({ name: 'Select' })
    expect(platformSelect.props('options')).toEqual([
      { value: 'gemini', label: 'Gemini' },
      { value: 'opencode_go', label: 'OpenCode Go' },
    ])
    expect(groupSelect.props('disabled')).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('[data-test="pricing-search"]').exists()).toBe(false)

    platformSelect.vm.$emit('update:modelValue', 'opencode_go')
    await wrapper.vm.$nextTick()

    expect(groupSelect.props('disabled')).toBe(false)
    expect(groupSelect.props('options')).toEqual([
      {
        value: 20,
        label: 'Enterprise',
        platform: 'opencode_go',
        subscriptionType: 'subscription',
        defaultMultiplier: 2,
        userMultiplier: 0.5,
        peakRateEnabled: true,
        peakStart: '14:00',
        peakEnd: '18:00',
        peakRateMultiplier: 3,
        isExclusive: true,
      },
    ])
    expect(wrapper.find('table').exists()).toBe(false)

    groupSelect.vm.$emit('update:modelValue', 20)
    await wrapper.vm.$nextTick()

    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.text()).toContain('deepseek-v4-flash')
    expect(wrapper.text()).not.toContain('gemini-2.5-flash')
    expect(wrapper.find('[data-test="pricing-search"]').exists()).toBe(true)

    platformSelect.vm.$emit('update:modelValue', 'gemini')
    await wrapper.vm.$nextTick()

    expect(groupSelect.props('modelValue')).toBeNull()
    expect(groupSelect.props('options')).toEqual([
      {
        value: 30,
        label: 'Free',
        platform: 'gemini',
        subscriptionType: 'standard',
        defaultMultiplier: 0,
        userMultiplier: null,
        peakRateEnabled: false,
        peakStart: '',
        peakEnd: '',
        peakRateMultiplier: 1,
        isExclusive: false,
      },
    ])
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('[data-test="pricing-search"]').exists()).toBe(false)

    groupSelect.vm.$emit('update:modelValue', 30)
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('gemini-2.5-flash')
    expect(wrapper.text()).not.toContain('deepseek-v4-flash')
  })

  it('clears a selected group when refresh removes it from the platform', async () => {
    const refreshedChannels = makeChannel()
    refreshedChannels[0].platforms[0].groups = [
      {
        ...refreshedChannels[0].platforms[0].groups[0],
        id: 22,
        name: 'Replacement',
      },
    ]
    getAvailable
      .mockResolvedValueOnce(makeChannel())
      .mockResolvedValueOnce(refreshedChannels)
    getUserGroupRates.mockResolvedValue({ 20: 0.5 })

    const wrapper = mountView()
    await flushPromises()
    await selectPricingScope(wrapper)
    await wrapper.get('[data-test="pricing-search"]').setValue('deepseek')

    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()

    const [platformSelect, groupSelect] = wrapper.findAllComponents({ name: 'Select' })
    expect(platformSelect.props('modelValue')).toBe('opencode_go')
    expect(groupSelect.props('modelValue')).toBeNull()
    expect(groupSelect.props('options')).toEqual([
      {
        value: 22,
        label: 'Replacement',
        platform: 'opencode_go',
        subscriptionType: 'subscription',
        defaultMultiplier: 2,
        userMultiplier: null,
        peakRateEnabled: true,
        peakStart: '14:00',
        peakEnd: '18:00',
        peakRateMultiplier: 3,
        isExclusive: true,
      },
    ])
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('[data-test="pricing-search"]').exists()).toBe(false)
  })

  it('loads pricing rows, refreshes, and switches between original and actual token prices', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({ 20: 0.5 })

    const wrapper = mountView()
    await flushPromises()
    await selectPricingScope(wrapper)

    expect(getAvailable).toHaveBeenCalledTimes(1)
    expect(getAvailable).toHaveBeenLastCalledWith({ purpose: 'model_pricing' })
    expect(getUserGroupRates).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain('Gateway A')
    expect(wrapper.text()).toContain('deepseek-v4-flash')
    expect(wrapper.text()).toContain('Channel Pricing')
    expect(wrapper.text()).toContain('Off-Peak · UTC 00:00-01:00, 04:00-06:00, 10:00-24:00')
    expect(wrapper.text()).toContain('Peak · UTC 01:00-04:00, 06:00-10:00')
    expect(wrapper.text()).toContain('Offer-Adjusted Model Multiplier')
    expect(wrapper.text()).toContain('Effective Multiplier')
    expect(wrapper.text()).not.toContain('Monthly Usage')
    expect(wrapper.text()).not.toContain('Quota Cost Multiplier')
    expect(wrapper.text()).toContain('0.5x')
    expect(wrapper.text()).toContain('2x usage limits')
    expect(wrapper.get('tbody td:nth-child(5)').text()).toBe('1.5x')

    const headers = wrapper.findAll('thead th').map((header) => header.text())
    expect(headers).toHaveLength(13)
    expect(headers).not.toContain('Channel')
    expect(headers).not.toContain('Platform')
    expect(headers).not.toContain('Group')
    expect(wrapper.get('tbody tr').findAll('td')).toHaveLength(13)
    expect(wrapper.find('tbody .group-badge-stub').exists()).toBe(false)

    const rawMode = wrapper.get('[data-pricing-mode="raw"]')
    const actualMode = wrapper.get('[data-pricing-mode="actual"]')
    const inputPrices = () => wrapper.get('tbody td:nth-child(9)').text()
    expect(rawMode.text()).toBe('Original Billing')
    expect(actualMode.text()).toBe('Actual Billing')
    expect(actualMode.attributes('aria-pressed')).toBe('true')
    expect(inputPrices()).toContain('¥0.33')
    expect(inputPrices()).toContain('¥0.66')
    expect(inputPrices()).not.toContain('¥0.22')
    expect(inputPrices()).not.toContain('$0.33')

    await rawMode.trigger('click')
    expect(rawMode.attributes('aria-pressed')).toBe('true')
    expect(inputPrices()).toContain('$0.22')
    expect(inputPrices()).toContain('$0.44')
    expect(inputPrices()).not.toContain('$0.66')

    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(getAvailable).toHaveBeenCalledTimes(2)
    expect(getAvailable).toHaveBeenLastCalledWith({ purpose: 'model_pricing' })
    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.findAll('[data-pricing-mode]')).toHaveLength(2)
    expect(wrapper.get('[data-pricing-mode="raw"]').attributes('aria-pressed')).toBe('true')
  })

  it('renders every context tier and its token prices', async () => {
    const channels = makeChannel()
    const pricing = channels[0].platforms[0].supported_models[0].pricing
    if (!pricing) throw new Error('test pricing is required')
    pricing.time_bands = []
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
    await selectPricingScope(wrapper)

    expect(wrapper.text()).toContain('Context Tier')
    expect(wrapper.text()).toContain('Up to 256K')
    expect(wrapper.text()).toContain('Above 256K')
    await wrapper.get('[data-pricing-mode="raw"]').trigger('click')
    const inputPrices = wrapper.get('tbody td:nth-child(9)').text()
    expect(inputPrices).toContain('$0.4')
    expect(inputPrices).toContain('$1.2')
    expect(wrapper.text()).toContain('$0.04')
    expect(wrapper.text()).toContain('$0.12')
    expect(wrapper.findAll('[data-pricing-mode]')).toHaveLength(2)
  })

  it('shows the current UTC time and updates it every second', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-12T03:04:05Z'))
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    const clock = wrapper.get('[data-test="utc-clock"]')
    expect(clock.text()).toContain('UTC')
    expect(clock.get('[data-test="utc-date"]').text()).toBe('2026-08-12')
    expect(clock.get('[data-test="utc-time"]').text()).toBe('03:04:05')
    expect(clock.get('time').attributes('datetime')).toBe('2026-08-12T03:04:05.000Z')

    await vi.advanceTimersByTimeAsync(1000)

    expect(clock.get('[data-test="utc-time"]').text()).toBe('03:04:06')
    expect(clock.get('time').attributes('datetime')).toBe('2026-08-12T03:04:06.000Z')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('opens and closes the pricing guide from the top-right help button', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    const helpButton = wrapper.get('[data-test="pricing-help-button"]')
    expect(helpButton.attributes('title')).toBe('View pricing guide')
    expect(helpButton.attributes('aria-haspopup')).toBe('dialog')
    expect(helpButton.attributes('aria-expanded')).toBe('false')
    expect(helpButton.get('[data-icon="questionCircle"]').exists()).toBe(true)
    expect(document.body.querySelector('[data-test="pricing-help-dialog"]')).toBeNull()

    await helpButton.trigger('click')
    await wrapper.vm.$nextTick()

    const dialog = document.body.querySelector<HTMLElement>('[data-test="pricing-help-dialog"]')
    expect(helpButton.attributes('aria-expanded')).toBe('true')
    expect(dialog).not.toBeNull()
    expect(dialog?.textContent).toContain('Actual token unit price = Original token unit price × Effective multiplier')
    expect(dialog?.textContent).toContain('How actual multipliers are formed')
    expect(dialog?.textContent).toContain('Context tiers')
    expect(dialog?.textContent).toContain('Peak and off-peak original prices')
    expect(dialog?.textContent).toContain('The offer is reflected in actual prices, charges, and Usage History.')

    const modal = dialog?.closest('[role="dialog"]')
    const closeButton = Array.from(modal?.querySelectorAll('button') ?? []).find(
      (button) => button.textContent?.trim() === 'Close',
    )
    expect(closeButton).toBeDefined()
    closeButton?.click()
    await wrapper.vm.$nextTick()

    expect(helpButton.attributes('aria-expanded')).toBe('false')
    expect(wrapper.findComponent({ name: 'ModelPricingHelpDialog' }).props('show')).toBe(false)
    wrapper.unmount()
    modal?.remove()
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
    await selectPricingScope(wrapper)

    const header = wrapper.get('thead')
    expect(header.classes()).toEqual(expect.arrayContaining(['sticky', 'top-0', 'z-20']))
  })

  it('keeps the model column sticky while the pricing table scrolls horizontally', async () => {
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()
    await selectPricingScope(wrapper)

    const stickyModelCells = wrapper.findAll('.model-pricing-sticky-model')
    expect(stickyModelCells).toHaveLength(2)
    expect(stickyModelCells[0].element.tagName).toBe('TH')
    expect(stickyModelCells[1].element.tagName).toBe('TD')
  })

  it('copies model id to clipboard and triggers success feedback', async () => {
    copyToClipboard.mockResolvedValue(true)
    getAvailable.mockResolvedValue(makeChannel())
    getUserGroupRates.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()
    await selectPricingScope(wrapper)

    const copyBtn = wrapper.find('.model-pricing-sticky-model button')
    expect(copyBtn.exists()).toBe(true)

    await copyBtn.trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('deepseek-v4-flash', 'Model ID copied')
  })
})
