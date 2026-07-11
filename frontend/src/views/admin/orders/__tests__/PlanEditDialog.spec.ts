import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlanEditDialog from '../PlanEditDialog.vue'

const createPlan = vi.hoisted(() => vi.fn().mockResolvedValue({}))
const updatePlan = vi.hoisted(() => vi.fn().mockResolvedValue({}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
      if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan,
    updatePlan,
  },
}))

function mountDialog(paymentConfig: Record<string, unknown> | null, plan: Record<string, unknown> | null = null) {
  return mount(PlanEditDialog, {
    props: {
      show: plan === null,
      plan,
      groups: [{ id: 10, name: 'OpenAI', platform: 'openai', subscription_type: 'subscription', rate_multiplier: 1 }],
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        },
        Select: true,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog subscription CNY payment preview', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 7.15,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 0,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })

  it('restores and submits all rolling quota limits when editing a plan', async () => {
    updatePlan.mockClear()
    const wrapper = mountDialog(null, {
      id: 7,
      group_id: 10,
      name: 'Rolling Plan',
      description: 'Quota snapshot',
      price: 10,
      original_price: null,
      renewal_discount_percent: null,
      stock: null,
      validity_days: 30,
      validity_unit: 'days',
      five_hour_limit_usd: 5,
      seven_day_limit_usd: 70,
      thirty_day_limit_usd: 300,
      sort_order: 1,
      for_sale: true,
      features: [],
    })
    await wrapper.setProps({ show: true })

    expect((wrapper.find('[data-testid="five-hour-limit-input"]').element as HTMLInputElement).value).toBe('5')
    expect((wrapper.find('[data-testid="seven-day-limit-input"]').element as HTMLInputElement).value).toBe('70')
    expect((wrapper.find('[data-testid="thirty-day-limit-input"]').element as HTMLInputElement).value).toBe('300')

    await wrapper.find('form').trigger('submit.prevent')

    expect(updatePlan).toHaveBeenCalledWith(7, expect.objectContaining({
      five_hour_limit_usd: 5,
      seven_day_limit_usd: 70,
      thirty_day_limit_usd: 300,
    }))
  })
})
