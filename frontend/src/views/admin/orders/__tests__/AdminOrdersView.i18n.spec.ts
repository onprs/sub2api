import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getOrders, getOrder } = vi.hoisted(() => ({
  getOrders: vi.fn(),
  getOrder: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    ...actual,
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

vi.mock('@/api/admin/payment', () => {
  const adminPaymentAPI = {
    getOrders,
    getOrder,
    cancelOrder: vi.fn(),
    retryRecharge: vi.fn(),
    refundOrder: vi.fn(),
    queryRefund: vi.fn()
  }
  return { adminPaymentAPI, default: adminPaymentAPI }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))

import AdminOrdersView from '../AdminOrdersView.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

const order = {
  id: 21,
  out_trade_no: 'ORDER-21',
  status: 'COMPLETED',
  amount: 10,
  pay_amount: 10,
  currency: 'USD',
  fee_rate: 0,
  payment_type: 'stripe',
  created_at: '2026-01-01T00:00:00Z',
  expires_at: '2026-01-01T01:00:00Z',
  paid_at: null,
  refund_amount: 0,
  refund_reason: '',
  refund_requested_at: null
}

const OrderTableStub = defineComponent({
  props: { orders: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    return () => h('div', props.orders.length > 0 ? slots.actions?.({ row: props.orders[0] }) : [])
  }
})

async function mountAndOpen(action: string) {
  getOrders.mockResolvedValue({ data: { items: [order], total: 1 } })
  getOrder.mockResolvedValue({
    data: {
      order,
      auditLogs: [{
        id: 1,
        action,
        detail: 'preserved detail',
        operator: 'system',
        created_at: '2026-01-01T00:00:00Z'
      }]
    }
  })

  const wrapper = mount(AdminOrdersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        OrderTable: OrderTableStub,
        Pagination: true,
        Select: true,
        Icon: true,
        OrderStatusBadge: true,
        AdminRefundDialog: true,
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>'
        }
      }
    }
  })
  await flushPromises()
  const viewButton = wrapper.findAll('button').find(button => button.text().includes(
    testLocale.value === 'zh' ? '查看' : 'View'
  ))
  expect(viewButton).toBeDefined()
  await viewButton!.trigger('click')
  await flushPromises()
  return wrapper
}

describe('AdminOrdersView audit action labels', () => {
  beforeEach(() => {
    testLocale.value = 'en'
    getOrders.mockReset()
    getOrder.mockReset()
  })

  it('renders known payment audit actions in English and Chinese while preserving detail', async () => {
    const english = await mountAndOpen('SUBSCRIPTION_SUCCESS')
    expect(english.text()).toContain('Subscription fulfilled')
    expect(english.text()).toContain('preserved detail')
    expect(english.text()).not.toContain('SUBSCRIPTION_SUCCESS')

    testLocale.value = 'zh'
    const chinese = await mountAndOpen('REFUND_ROLLBACK_FAILED')
    expect(chinese.text()).toContain('退款回滚失败')
    expect(chinese.text()).not.toContain('REFUND_ROLLBACK_FAILED')
  })
})
