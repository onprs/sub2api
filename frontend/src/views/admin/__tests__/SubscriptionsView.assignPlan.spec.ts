import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import SubscriptionsView from '../SubscriptionsView.vue'

const {
  listSubscriptions,
  assignSubscription,
  getAllGroups,
  searchUsers,
  getPlans,
  bulkResetQuota,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  assignSubscription: vi.fn(),
  getAllGroups: vi.fn(),
  searchUsers: vi.fn(),
  getPlans: vi.fn(),
  bulkResetQuota: vi.fn(),
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
      resetQuota: vi.fn(),
      bulkResetQuota
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
        DataTable: {
          props: ['columns', 'data'],
          template: `
            <table>
              <thead>
                <tr>
                  <th v-for="col in columns" :key="col.key">
                    <slot :name="'header-' + col.key" />
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in data" :key="row.id">
                  <td v-for="col in columns" :key="col.key">
                    <slot :name="'cell-' + col.key" :row="row" :value="row[col.key]" />
                  </td>
                </tr>
              </tbody>
              <slot v-if="!data || data.length === 0" name="empty" />
            </table>
          `
        },
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
    bulkResetQuota.mockReset()
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
    bulkResetQuota.mockResolvedValue({
      success_count: 1,
      failed_count: 0,
      subscriptions: [],
      errors: []
    })
  })

  it('renders active subscription actions in three fixed-width command columns', async () => {
    listSubscriptions.mockResolvedValue({
      items: [
        {
          id: 55,
          user_id: 1001,
          user_email: 'target@example.com',
          group_id: 2,
          group_name: 'Codex Plus Group',
          status: 'active',
          started_at: '2026-01-01T00:00:00Z',
          expires_at: '2026-02-01T00:00:00Z'
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountSubscriptionsView()
    await flushPromises()

    const actions = wrapper.get('[data-test="subscription-actions"]')
    expect(actions.classes()).toContain('grid-cols-3')
    expect(actions.classes()).toContain('md:w-[15rem]')
    expect(actions.findAll('button')).toHaveLength(3)
    expect(actions.findAll('button').every((button) => button.classes().includes('whitespace-nowrap'))).toBe(true)

    const actionColumn = (wrapper.vm as unknown as { columns: Array<{ key: string; class?: string }> }).columns
      .find((column) => column.key === 'actions')
    expect(actionColumn?.class).toContain('w-[17rem]')
    expect(actionColumn?.class).toContain('min-w-[17rem]')
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

  it('resets all currently filtered subscriptions with selected rolling windows', async () => {
    listSubscriptions.mockResolvedValue({
      items: [
        {
          id: 55,
          user_id: 1001,
          group_id: 2,
          status: 'active',
          starts_at: '2026-01-01T00:00:00Z',
          expires_at: '2026-02-01T00:00:00Z',
          five_hour_limit_usd: 5,
          seven_day_limit_usd: 70,
          thirty_day_limit_usd: 300,
          five_hour_usage_usd: 1,
          seven_day_usage_usd: 2,
          thirty_day_usage_usd: 3,
          five_hour_window_start: '2026-01-01T00:00:00Z',
          seven_day_window_start: '2026-01-01T00:00:00Z',
          thirty_day_window_start: '2026-01-01T00:00:00Z',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          user: { id: 1001, email: 'target@example.com', username: 'target' },
          group: {
            id: 2,
            name: 'Codex Plus Group',
            platform: 'openai',
            subscription_type: 'subscription',
            status: 'active',
            rate_multiplier: 1
          }
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountSubscriptionsView()

    await flushPromises()
    const renderedText = wrapper.text()
    expect(renderedText).toContain('5h')
    expect(renderedText).toContain('7d')
    expect(renderedText).toContain('30d')
    expect(renderedText).not.toContain('payment.quotaWindows')

    await wrapper.get('[data-test="reset-filtered-quota-open"]').trigger('click')
    await wrapper.get('[data-test="reset-window-seven-day"]').setValue(false)
    await wrapper.get('[data-test="reset-quota-confirm"]').trigger('click')
    await flushPromises()

    expect(bulkResetQuota).toHaveBeenCalledWith({
      five_hour: true,
      seven_day: false,
      thirty_day: true,
      all_filtered: true,
      subscription_ids: undefined,
      filter: {
        status: 'active',
        group_id: undefined,
        platform: undefined,
        user_id: undefined,
        sort_by: 'created_at',
        sort_order: 'desc'
      }
    })
  })

  it('sends only the selected five-hour reset window for filtered subscriptions', async () => {
    listSubscriptions.mockResolvedValue({
      items: [
        {
          id: 56,
          user_id: 1001,
          group_id: 2,
          status: 'active',
          starts_at: '2026-01-01T00:00:00Z',
          expires_at: '2026-02-01T00:00:00Z',
          five_hour_limit_usd: 5,
          seven_day_limit_usd: 70,
          thirty_day_limit_usd: 300,
          five_hour_usage_usd: 1,
          seven_day_usage_usd: 2,
          thirty_day_usage_usd: 3,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          user: { id: 1001, email: 'target@example.com', username: 'target' },
          group: {
            id: 2,
            name: 'Codex Plus Group',
            platform: 'openai',
            subscription_type: 'subscription',
            status: 'active',
            rate_multiplier: 1
          }
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountSubscriptionsView()

    await flushPromises()
    await wrapper.get('[data-test="reset-filtered-quota-open"]').trigger('click')
    await wrapper.get('[data-test="reset-window-seven-day"]').setValue(false)
    await wrapper.get('[data-test="reset-window-thirty-day"]').setValue(false)
    await wrapper.get('[data-test="reset-quota-confirm"]').trigger('click')
    await flushPromises()

    expect(bulkResetQuota).toHaveBeenCalledWith(expect.objectContaining({
      five_hour: true,
      seven_day: false,
      thirty_day: false,
      all_filtered: true,
      subscription_ids: undefined
    }))
  })

  it('does not show success toast after partial bulk reset failure', async () => {
    listSubscriptions.mockResolvedValue({
      items: [
        {
          id: 57,
          user_id: 1001,
          group_id: 2,
          status: 'active',
          starts_at: '2026-01-01T00:00:00Z',
          expires_at: '2026-02-01T00:00:00Z',
          five_hour_limit_usd: 5,
          seven_day_limit_usd: 70,
          thirty_day_limit_usd: 300,
          five_hour_usage_usd: 1,
          seven_day_usage_usd: 2,
          thirty_day_usage_usd: 3,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          user: { id: 1001, email: 'target@example.com', username: 'target' },
          group: {
            id: 2,
            name: 'Codex Plus Group',
            platform: 'openai',
            subscription_type: 'subscription',
            status: 'active',
            rate_multiplier: 1
          }
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    bulkResetQuota.mockResolvedValueOnce({
      success_count: 1,
      failed_count: 1,
      subscriptions: [],
      errors: ['subscription 999: not found']
    })
    const wrapper = mountSubscriptionsView()

    await flushPromises()
    await wrapper.get('[data-test="reset-filtered-quota-open"]').trigger('click')
    await wrapper.get('[data-test="reset-quota-confirm"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.subscriptions.quotaResetPartialSuccess')
    expect(showSuccess).not.toHaveBeenCalledWith('admin.subscriptions.quotaResetSuccess')
  })
})
