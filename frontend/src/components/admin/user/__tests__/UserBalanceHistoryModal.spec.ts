import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminUser } from '@/types'
import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

const mocks = vi.hoisted(() => ({ getUserBalanceHistory: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: mocks } }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

function result(id: number) {
  return { items: [{ id, type: 'admin_balance', value: id, notes: `History ${id}` }], total: 1, total_recharged: id }
}

function deferred() {
  let resolve!: (value: ReturnType<typeof result>) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<ReturnType<typeof result>>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

async function openDialog(user: AdminUser = { id: 1, email: 'one@example.com', balance: 1 } as AdminUser) {
  const wrapper = mount(UserBalanceHistoryModal, {
    props: { show: false, user },
    global: {
      stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
        Icon: true,
        Select: true,
      },
    },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe('UserBalanceHistoryModal', () => {
  it('renders payment-order subscription entries with stable source keys', async () => {
    mocks.getUserBalanceHistory.mockResolvedValue({
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
    const wrapper = await openDialog({
      id: 1,
      email: 'user@example.com',
      username: 'demo',
      balance: 0,
      notes: '',
      created_at: '2026-01-01T00:00:00Z',
    } as AdminUser)
    await flushPromises()

    expect(wrapper.text()).toContain('redeem.subscriptionAssigned')
    expect(wrapper.text()).toContain('30d - OpenAI Pro')
    expect(warnSpy.mock.calls.flat().join('\n')).not.toContain('Duplicate keys')
  })

  it('keeps the new user history when an old response finishes later', async () => {
    const old = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(20))
    const wrapper = await openDialog()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, user: { id: 2, email: 'two@example.com', balance: 2 } as AdminUser })
    await flushPromises()
    old.resolve(result(10))
    await flushPromises()
    expect(wrapper.text()).toContain('History 20')
    expect(wrapper.text()).not.toContain('History 10')
    expect(wrapper.text()).toContain('$20.00')
  })

  it('does not end the current filter loading state when an old request finishes', async () => {
    const old = deferred()
    const current = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openDialog()
    wrapper.findComponent({ name: 'Select' }).vm.$emit('change', 'balance')
    await flushPromises()
    old.resolve(result(10))
    await flushPromises()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('History 10')
    current.resolve(result(20))
    await flushPromises()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(false)
    expect(wrapper.text()).toContain('History 20')
  })

  it.each(['close', 'unmount'])('ignores failures after %s', async (action) => {
    const pending = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(pending.promise)
    const wrapper = await openDialog()
    if (action === 'close') await wrapper.setProps({ show: false })
    else wrapper.unmount()
    pending.reject(new Error('Stale error'))
    await flushPromises()
    expect(console.error).not.toHaveBeenCalled()
  })

  it('still reports current failures and ends loading', async () => {
    const error = new Error('Current error')
    mocks.getUserBalanceHistory.mockRejectedValueOnce(error)
    const wrapper = await openDialog()
    await flushPromises()
    expect(console.error).toHaveBeenCalledWith('Failed to load balance history:', error)
    expect(wrapper.find('svg.animate-spin').exists()).toBe(false)
  })
})
