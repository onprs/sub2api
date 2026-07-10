import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

const getUserBalanceHistory = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      getUserBalanceHistory,
    },
  },
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => value,
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      admin: {
        users: {
          balanceHistoryTitle: 'Balance History',
          createdAt: 'Created at',
          currentBalance: 'Current Balance',
          totalRecharged: 'Total Recharged',
          notes: 'Notes',
          allTypes: 'All',
          typeBalance: 'Balance',
          typeAffiliateBalance: 'Affiliate Balance',
          typeAdminBalance: 'Admin Balance',
          typeConcurrency: 'Concurrency',
          typeAdminConcurrency: 'Admin Concurrency',
          typeSubscription: 'Subscription',
          noBalanceHistory: 'No history',
        },
      },
      common: {
        unknown: 'Unknown',
      },
      redeem: {
        balanceAddedRedeem: 'Balance added',
        balanceAddedAffiliate: 'Affiliate balance added',
        balanceAddedAdmin: 'Admin balance added',
        balanceDeductedAdmin: 'Admin balance deducted',
        concurrencyAddedRedeem: 'Concurrency added',
        concurrencyAddedAdmin: 'Admin concurrency added',
        concurrencyReducedAdmin: 'Admin concurrency reduced',
        subscriptionAssigned: 'Subscription assigned',
        adminAdjustment: 'Admin adjustment',
      },
      pagination: {
        previous: 'Previous',
        next: 'Next',
      },
    },
  },
})

const user = {
  id: 1,
  email: 'user@example.com',
  username: 'demo',
  balance: 0,
  notes: '',
  created_at: '2026-01-01T00:00:00Z',
}

const stubs = {
  BaseDialog: {
    props: ['show', 'title'],
    template: '<section v-if="show"><slot /></section>',
  },
  Select: {
    props: ['modelValue', 'options'],
    emits: ['update:modelValue', 'change'],
    template: '<select :value="modelValue" @change="$emit(\'change\')"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
  },
  Icon: true,
}

describe('UserBalanceHistoryModal', () => {
  beforeEach(() => {
    getUserBalanceHistory.mockReset().mockResolvedValue({
      items: [],
      total: 0,
      total_recharged: 0,
    })
  })

  it('renders payment-order subscription history entries with stable source keys', async () => {
    getUserBalanceHistory.mockResolvedValue({
      items: [
        {
          id: 42,
          source: 'redeem_code',
          source_id: 42,
          code: 'redeem-42',
          type: 'balance',
          value: 5,
          status: 'used',
          used_by: 1,
          used_at: '2026-01-02T00:00:00Z',
          created_at: '2026-01-01T00:00:00Z',
          group_id: null,
          validity_days: 0,
          notes: '',
        },
        {
          id: 42,
          source: 'payment_order',
          source_id: 9001,
          payment_order_id: 9001,
          code: 'order-9001',
          type: 'subscription',
          value: 30,
          status: 'completed',
          used_by: 1,
          used_at: '2026-01-03T00:00:00Z',
          created_at: '2026-01-03T00:00:00Z',
          group_id: 10,
          validity_days: 30,
          notes: 'Paid subscription',
          group: { id: 10, name: 'OpenAI Pro' },
        },
      ],
      total: 2,
      total_recharged: 5,
    })
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})

    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user,
      },
      global: {
        plugins: [i18n],
        stubs,
      },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.text()).toContain('redeem.subscriptionAssigned')
    expect(wrapper.text()).toContain('30d - OpenAI Pro')
    expect(warnSpy.mock.calls.flat().join('\n')).not.toContain('Duplicate keys')

    warnSpy.mockRestore()
  })
})
