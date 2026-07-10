import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import RedeemView from '../RedeemView.vue'

const {
  listRedeemCodes,
  generateRedeemCodes,
  getAllGroups,
  getPlans,
  showSuccess,
  showError,
  showInfo
} = vi.hoisted(() => ({
  listRedeemCodes: vi.fn(),
  generateRedeemCodes: vi.fn(),
  getAllGroups: vi.fn(),
  getPlans: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    redeem: {
      list: listRedeemCodes,
      generate: generateRedeemCodes,
      delete: vi.fn(),
      batchDelete: vi.fn(),
      batchUpdate: vi.fn(),
      exportCodes: vi.fn()
    },
    groups: {
      getAll: getAllGroups
    },
    payment: {
      getPlans
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError,
    showInfo
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
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

const DataTableStub = {
  props: ['columns', 'data'],
  template: '<table></table>'
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  setup(
    props: { options: Array<{ value: unknown; label: string }> },
    { emit }: { emit: (event: string, ...args: unknown[]) => void }
  ) {
    const onChange = (event: Event) => {
      const raw = (event.target as HTMLSelectElement).value
      const option = props.options.find((item) => String(item.value ?? '') === raw)
      const value = option ? option.value : raw
      emit('update:modelValue', value)
      emit('change', value, option ?? null)
    }
    return { onChange }
  },
  template: `
    <select v-bind="$attrs" :value="modelValue ?? ''" @change="onChange">
      <option v-for="option in options" :key="String(option.value ?? '')" :value="option.value ?? ''">
        {{ option.label }}
      </option>
    </select>
  `
}

const mountRedeemView = () =>
  mount(RedeemView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: true,
        Select: SelectStub,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true,
        Teleport: true
      }
    }
  })

describe('admin RedeemView subscription plan generation', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''

    listRedeemCodes.mockReset()
    generateRedeemCodes.mockReset()
    getAllGroups.mockReset()
    getPlans.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    showInfo.mockReset()

    listRedeemCodes.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
    getAllGroups.mockResolvedValue([
      {
        id: 2,
        name: 'Subscription group',
        platform: 'openai',
        subscription_type: 'subscription',
        rate_multiplier: 1,
        description: null
      }
    ])
    getPlans.mockResolvedValue({
      data: [
        {
          id: 9,
          group_id: 2,
          name: 'Pro',
          description: 'Pro plan',
          price: 19.9,
          validity_days: 30,
          validity_unit: 'days',
          features: [],
          for_sale: true,
          sort_order: 1,
          five_hour_limit_usd: 1,
          seven_day_limit_usd: 7,
          thirty_day_limit_usd: 30
        }
      ]
    })
    generateRedeemCodes.mockResolvedValue([
      {
        id: 1,
        code: 'PLAN-CODE',
        type: 'subscription',
        value: 0,
        status: 'unused',
        used_by: null,
        used_at: null,
        created_at: '2026-01-01T00:00:00Z',
        subscription_plan_id: 9
      }
    ])
  })

  it('sends subscription_plan_id when plan mode is selected', async () => {
    const wrapper = mountRedeemView()

    await flushPromises()
    await wrapper.get('[data-test="generate-open"]').trigger('click')
    await wrapper.get('[data-test="generate-type"]').setValue('subscription')
    await flushPromises()

    await wrapper.get('[data-test="subscription-plan-select"]').setValue('9')
    await wrapper.get('[data-test="generate-form"]').trigger('submit')
    await flushPromises()

    expect(generateRedeemCodes).toHaveBeenCalledWith(1, 'subscription', 10, {
      subscriptionPlanId: 9,
      expiresInDays: undefined
    })
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps group mode generation available for legacy subscription codes', async () => {
    const wrapper = mountRedeemView()

    await flushPromises()
    await wrapper.get('[data-test="generate-open"]').trigger('click')
    await wrapper.get('[data-test="generate-type"]').setValue('subscription')
    await flushPromises()

    await wrapper.get('[data-test="subscription-mode-group"]').trigger('click')
    await wrapper.get('[data-test="subscription-group-select"]').setValue('2')
    await wrapper.get('[data-test="subscription-validity-days"]').setValue(14)
    await wrapper.get('[data-test="generate-form"]').trigger('submit')
    await flushPromises()

    expect(generateRedeemCodes).toHaveBeenCalledWith(1, 'subscription', 10, {
      groupId: 2,
      validityDays: 14,
      expiresInDays: undefined
    })
    expect(showError).not.toHaveBeenCalled()
  })
})
