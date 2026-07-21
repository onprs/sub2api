import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import TicketCreateView from '../TicketCreateView.vue'

const { usageList, getMyOrders, getMySubscriptions, refresh, push, replace } = vi.hoisted(() => ({
  usageList: vi.fn(),
  getMyOrders: vi.fn(),
  getMySubscriptions: vi.fn(),
  refresh: vi.fn(),
  push: vi.fn(),
  replace: vi.fn(),
}))

vi.mock('@/api', () => ({
  paymentAPI: { getMyOrders },
  ticketsAPI: { create: vi.fn(), uploadAttachment: vi.fn() },
  usageAPI: { list: usageList },
}))

vi.mock('@/api/subscriptions', () => ({
  default: { getMySubscriptions },
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
      })[key] ?? key,
    }),
  }
})

describe('TicketCreateView', () => {
  it('uses a single divider and offers usage records for API issues', async () => {
    usageList.mockResolvedValue({
      items: [{ id: 42, request_id: 'client:req-ticket-context' }],
    })
    getMyOrders.mockResolvedValue({ data: { items: [] } })
    getMySubscriptions.mockResolvedValue([])
    refresh.mockResolvedValue(undefined)

    const wrapper = mount(TicketCreateView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Select: {
            props: ['modelValue', 'options'],
            template: '<div class="select-stub"><span v-for="option in options" :key="option.value">{{ option.label }}</span></div>',
          },
          Icon: true,
          TicketAttachmentUploader: true,
        },
      },
    })
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
})
