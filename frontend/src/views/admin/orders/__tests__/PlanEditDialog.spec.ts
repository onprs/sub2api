import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import PlanEditDialog from '../PlanEditDialog.vue'

const updatePlan = vi.hoisted(() => vi.fn())
const createPlan = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    updatePlan,
    createPlan,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      common: {
        cancel: 'Cancel',
        save: 'Save',
        saving: 'Saving',
        saved: 'Saved',
        error: 'Error',
      },
      payment: {
        admin: {
          createPlan: 'Create Plan',
          editPlan: 'Edit Plan',
          planName: 'Plan Name',
          group: 'Group',
          selectGroup: 'Select a group',
          planDescription: 'Plan Description',
          price: 'Price',
          originalPrice: 'Original Price',
          renewalDiscountPercent: 'Renewal Discount Percent',
          renewalDiscountHint: 'Empty or 0 = no renewal discount',
          renewalDiscountInvalid: 'Renewal discount invalid',
          stock: 'Stock',
          stockHint: 'Empty = unlimited, 0 = sold out',
          stockInvalid: 'Stock cannot be negative',
          validityDays: 'Validity days',
          validityUnit: 'Validity unit',
          days: 'days',
          weeks: 'weeks',
          months: 'months',
          rollingQuotaLimits: 'Rolling Quota Limits',
          rollingQuotaHint: 'Empty = unlimited',
          fiveHourLimit: '5h Limit',
          sevenDayLimit: '7d Limit',
          thirtyDayLimit: '30d Limit',
          sortOrder: 'Sort Order',
          features: 'Features',
          featuresPlaceholder: 'Features',
          featuresHint: 'One feature per line',
          forSale: 'For Sale',
          unlimited: 'Unlimited',
        },
      },
    },
  },
})

const stubs = {
  BaseDialog: {
    props: ['show', 'title'],
    template: '<section v-if="show"><slot /><slot name="footer" /></section>',
  },
  Select: {
    props: ['modelValue', 'options', 'placeholder'],
    emits: ['update:modelValue'],
    methods: {
      onChange(event: Event) {
        this.$emit('update:modelValue', Number((event.target as HTMLSelectElement).value))
      },
    },
    template: '<select :value="modelValue" @change="onChange"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
  },
  GroupBadge: {
    props: ['name'],
    template: '<span>{{ name }}</span>',
  },
  Icon: true,
}

describe('PlanEditDialog', () => {
  beforeEach(() => {
    updatePlan.mockReset().mockResolvedValue({})
    createPlan.mockReset().mockResolvedValue({})
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('previews the current plan rolling quota values instead of legacy group limits', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: {
          id: 1,
          group_id: 10,
          name: 'Rolling Plan',
          description: 'Plan with rolling windows',
          price: 12,
          original_price: 15,
          validity_days: 30,
          validity_unit: 'days',
          five_hour_limit_usd: 0,
          seven_day_limit_usd: null,
          thirty_day_limit_usd: 30,
          sort_order: 1,
          for_sale: true,
          features: [],
        },
        groups: [
          {
            id: 10,
            name: 'Legacy Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
            daily_limit_usd: 99,
            weekly_limit_usd: 999,
            monthly_limit_usd: 9999,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })

    await wrapper.setProps({ show: true })

    const text = wrapper.text()
    expect(text).toContain('$0.00')
    expect(text).toContain('payment.admin.unlimited')
    expect(text).toContain('$30.00')
    expect(text).not.toContain('$99')
    expect(text).not.toContain('$999')
    expect(text).not.toContain('$9999')
  })

  it('sends renewal_discount_percent when saving an edited plan', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: {
          id: 1,
          group_id: 10,
          name: 'Renewal Plan',
          description: 'Plan with renewal discount',
          price: 20,
          original_price: 25,
          renewal_discount_percent: 15,
          validity_days: 30,
          validity_unit: 'days',
          five_hour_limit_usd: null,
          seven_day_limit_usd: null,
          thirty_day_limit_usd: null,
          sort_order: 1,
          for_sale: true,
          features: [],
        },
        groups: [
          {
            id: 10,
            name: 'Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })

    await wrapper.setProps({ show: true })
    await wrapper.find('form').trigger('submit.prevent')

    expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({
      renewal_discount_percent: 15,
    }))
  })

  it('sends null to clear renewal_discount_percent', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: {
          id: 1,
          group_id: 10,
          name: 'Renewal Plan',
          description: 'Plan with renewal discount',
          price: 20,
          original_price: 25,
          renewal_discount_percent: 15,
          validity_days: 30,
          validity_unit: 'days',
          five_hour_limit_usd: null,
          seven_day_limit_usd: null,
          thirty_day_limit_usd: null,
          sort_order: 1,
          for_sale: true,
          features: [],
        },
        groups: [
          {
            id: 10,
            name: 'Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })
    await wrapper.setProps({ show: true })
    const renewalInput = wrapper.findAll('input[type="number"]')[2]
    await renewalInput.setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({
      renewal_discount_percent: null,
    }))
  })

  it('sends stock as null when the stock input is empty', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: {
          id: 1,
          group_id: 10,
          name: 'Stock Plan',
          description: 'Plan with stock',
          price: 20,
          original_price: 25,
          renewal_discount_percent: null,
          stock: 12,
          validity_days: 30,
          validity_unit: 'days',
          five_hour_limit_usd: null,
          seven_day_limit_usd: null,
          thirty_day_limit_usd: null,
          sort_order: 1,
          for_sale: true,
          features: [],
        },
        groups: [
          {
            id: 10,
            name: 'Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })
    await wrapper.setProps({ show: true })
    await wrapper.find('[data-testid="plan-stock-input"]').setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({
      stock: null,
    }))
  })

  it('rejects negative stock before saving', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: null,
        groups: [
          {
            id: 10,
            name: 'Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })
    await wrapper.setProps({ show: true })
    const inputs = wrapper.findAll('input[type="number"]')
    const groupSelect = wrapper.find('select')
    await wrapper.find('input[type="text"]').setValue('Invalid Stock Plan')
    await groupSelect.setValue('10')
    await wrapper.find('textarea').setValue('desc')
    await inputs[0].setValue('20')
    await wrapper.find('[data-testid="plan-stock-input"]').setValue('-1')
    await wrapper.find('form').trigger('submit.prevent')

    expect(showError).toHaveBeenCalledWith('payment.admin.stockInvalid')
    expect(createPlan).not.toHaveBeenCalled()
  })

  it('rejects invalid renewal_discount_percent before saving', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: false,
        plan: null,
        groups: [
          {
            id: 10,
            name: 'Group',
            platform: 'openai',
            subscription_type: 'subscription',
            rate_multiplier: 1,
          },
        ],
      },
      global: {
        plugins: [createPinia(), i18n],
        stubs,
      },
    })
    await wrapper.setProps({ show: true })
    const inputs = wrapper.findAll('input[type="number"]')
    const groupSelect = wrapper.find('select')
    await wrapper.find('input[type="text"]').setValue('Invalid Discount Plan')
    await groupSelect.setValue('10')
    await wrapper.find('textarea').setValue('desc')
    await inputs[0].setValue('20')
    await inputs[2].setValue('100')
    await wrapper.find('form').trigger('submit.prevent')

    expect(showError).toHaveBeenCalledWith('payment.admin.renewalDiscountInvalid')
    expect(createPlan).not.toHaveBeenCalled()
  })
})
