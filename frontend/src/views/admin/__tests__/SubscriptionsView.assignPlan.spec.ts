import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import SubscriptionsView from '../SubscriptionsView.vue'

const {
  listSubscriptions,
  assignSubscription,
  getAllGroups,
  searchUsers,
  getPlans,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  assignSubscription: vi.fn(),
  getAllGroups: vi.fn(),
  searchUsers: vi.fn(),
  getPlans: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: {
      list: listSubscriptions,
      assign: assignSubscription,
      extend: vi.fn(),
      revoke: vi.fn(),
      resetQuota: vi.fn()
    },
    groups: {
      getAll: getAllGroups
    },
    usage: {
      searchUsers
    },
    payment: {
      getPlans
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
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

const mountSubscriptionsView = () =>
  mount(SubscriptionsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: { template: '<div></div>' },
        Pagination: true,
        BaseDialog: { template: '<div v-if="show"><slot /><slot name="footer" /></div>', props: ['show'] },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true,
        Teleport: true,
        RouterLink: true
      }
    }
  })

describe('admin SubscriptionsView plan assignment', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''

    listSubscriptions.mockReset()
    assignSubscription.mockReset()
    getAllGroups.mockReset()
    searchUsers.mockReset()
    getPlans.mockReset()
    showSuccess.mockReset()
    showError.mockReset()

    listSubscriptions.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
    getAllGroups.mockResolvedValue([
      {
        id: 2,
        name: 'Codex Plus Group',
        platform: 'openai',
        subscription_type: 'subscription',
        status: 'active',
        rate_multiplier: 1,
        description: null
      }
    ])
    getPlans.mockResolvedValue({
      data: [
        {
          id: 9,
          group_id: 2,
          group_name: 'Codex Plus Group',
          name: 'Codex Plus 7d',
          description: 'Plan',
          price: 19.9,
          validity_days: 7,
          validity_unit: 'days',
          features: [],
          for_sale: true,
          sort_order: 1,
          five_hour_limit_usd: 5,
          seven_day_limit_usd: 70,
          thirty_day_limit_usd: 300
        }
      ]
    })
    searchUsers.mockResolvedValue([
      { id: 1001, email: 'target@example.com', username: 'target' }
    ])
    assignSubscription.mockResolvedValue({ id: 1 })
  })

  it('assigns a selected subscription plan instead of group validity fields', async () => {
    const wrapper = mountSubscriptionsView()

    await flushPromises()
    await wrapper.get('[data-test="assign-open"]').trigger('click')
    await wrapper.get('[data-test="assign-user-search"]').trigger('focus')
    await wrapper.get('[data-test="assign-user-search"]').setValue('target@example.com')
    await new Promise((resolve) => setTimeout(resolve, 350))
    await flushPromises()
    await wrapper.get('[data-test="assign-user-option"]').trigger('click')
    await wrapper.get('[data-test="assign-plan-select"]').setValue('9')
    await wrapper.get('[data-test="assign-form"]').trigger('submit')
    await flushPromises()

    expect(assignSubscription).toHaveBeenCalledWith({
      user_id: 1001,
      subscription_plan_id: 9
    })
    expect(assignSubscription).not.toHaveBeenCalledWith(
      expect.objectContaining({
        group_id: expect.anything(),
        validity_days: expect.anything()
      })
    )
    expect(showError).not.toHaveBeenCalled()
  })
})
