import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import TicketCreateView from '../TicketCreateView.vue'

const { usageList, getMyOrders, getActiveSubscriptions, create, refresh, push, replace } = vi.hoisted(() => ({
  usageList: vi.fn(),
  getMyOrders: vi.fn(),
  getActiveSubscriptions: vi.fn(),
  create: vi.fn(),
  refresh: vi.fn(),
  push: vi.fn(),
  replace: vi.fn(),
}))

vi.mock('@/api', () => ({
  paymentAPI: { getMyOrders },
  ticketsAPI: { create, uploadAttachment: vi.fn() },
  usageAPI: { list: usageList },
}))

vi.mock('@/api/subscriptions', () => ({
  default: { getActiveSubscriptions },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
  useTicketNotificationsStore: () => ({
    capabilities: { attachments: { enabled: false } },
    refresh,
  }),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRouter: () => ({ push, replace }),
    onBeforeRouteLeave: vi.fn(),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'tickets.create.relatedUsage': 'Related usage record',
        'tickets.create.usageLog': 'Usage log ID',
        'tickets.create.apiKey': 'API key ID',
        'tickets.create.relatedSubscription': 'Related subscription (optional)',
        'tickets.create.optionalResourcePlaceholder': 'Optional; submit without linking',
      })[key] ?? key,
    }),
  }
})

function mountTicketCreateView() {
  return mount(TicketCreateView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Select: {
          props: ['modelValue', 'options', 'placeholder'],
          template: '<div class="select-stub"><span class="placeholder">{{ placeholder }}</span><span v-for="option in options" :key="option.value">{{ option.label }}</span></div>',
        },
        Icon: true,
        TicketAttachmentUploader: true,
      },
    },
  })
}

describe('TicketCreateView', () => {
  beforeEach(() => {
    usageList.mockReset()
    getMyOrders.mockReset()
    getActiveSubscriptions.mockReset()
    create.mockReset()
    refresh.mockReset()
    push.mockReset()
    replace.mockReset()

    usageList.mockResolvedValue({ items: [] })
    getMyOrders.mockResolvedValue({ data: { items: [] } })
    getActiveSubscriptions.mockResolvedValue([])
    create.mockResolvedValue({ ticket: { ticket_no: 'TK-20260722-ABC234' } })
    refresh.mockResolvedValue(undefined)
    replace.mockResolvedValue(undefined)
  })

  it('uses a single divider and offers usage records for API issues', async () => {
    usageList.mockResolvedValue({
      items: [{ id: 42, request_id: 'client:req-ticket-context' }],
    })
    getMyOrders.mockResolvedValue({ data: { items: [] } })

    const wrapper = mountTicketCreateView()
    await flushPromises()

    const resourceSection = wrapper.find('section')
    expect(resourceSection.exists()).toBe(true)
    expect(resourceSection.classes()).toContain('border-t')
    expect(resourceSection.classes()).not.toContain('border-y')
    expect(resourceSection.text()).toContain('Related usage record')
    expect(resourceSection.text()).toContain('Usage log ID: req-ticket-context')
    expect(resourceSection.text()).not.toContain('client:')
    expect(resourceSection.text()).not.toContain('API key ID')
  })

  it('offers only active subscriptions and submits without selecting one', async () => {
    getActiveSubscriptions.mockResolvedValue([
      { id: 73, group: { name: 'Pro plan' }, status: 'active' },
    ])

    const wrapper = mountTicketCreateView()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.form.category = 'subscription'
    vm.form.subject = 'Subscription question'
    vm.form.body = 'Please help with this subscription.'
    await wrapper.vm.$nextTick()

    const resourceSection = wrapper.find('section')
    expect(getActiveSubscriptions).toHaveBeenCalledOnce()
    expect(resourceSection.text()).toContain('Related subscription (optional)')
    expect(resourceSection.text()).toContain('Optional; submit without linking')
    expect(resourceSection.text()).toContain('Pro plan')

    await vm.submit()

    const payload = create.mock.calls[0][0]
    expect(payload.category).toBe('subscription')
    expect(payload).not.toHaveProperty('user_subscription_id')
    expect(replace).toHaveBeenCalledWith('/tickets/TK-20260722-ABC234')
  })

  it('submits other questions without showing or requiring a related resource', async () => {
    getActiveSubscriptions.mockResolvedValue([
      { id: 73, group: { name: 'Pro plan' }, status: 'active' },
    ])

    const wrapper = mountTicketCreateView()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.form.category = 'other'
    vm.form.subject = 'General question'
    vm.form.body = 'No related resource is needed.'
    await wrapper.vm.$nextTick()

    expect(wrapper.find('section').exists()).toBe(false)

    await vm.submit()

    const payload = create.mock.calls[0][0]
    expect(payload.category).toBe('other')
    expect(payload).not.toHaveProperty('usage_log_id')
    expect(payload).not.toHaveProperty('payment_order_id')
    expect(payload).not.toHaveProperty('user_subscription_id')
  })
})
