import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UserErrorDetailModal from '../UserErrorDetailModal.vue'

const { getMyErrorDetail } = vi.hoisted(() => ({
  getMyErrorDetail: vi.fn(),
}))

vi.mock('@/api/usage', () => ({ getMyErrorDetail }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

describe('UserErrorDetailModal', () => {
  it('hides the internal request ID prefix and wraps long IDs', async () => {
    getMyErrorDetail.mockResolvedValue({
      id: 1,
      created_at: '2026-07-22T00:00:00Z',
      request_id: 'client:req-error-visible-to-user',
      status_code: 502,
      category: 'upstream',
    })

    const wrapper = mount(UserErrorDetailModal, {
      props: { show: false, errorId: null },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
        },
      },
    })

    await wrapper.setProps({ show: true, errorId: 1 })
    await flushPromises()

    expect(wrapper.text()).toContain('req-error-visible-to-user')
    expect(wrapper.text()).not.toContain('client:')
    expect(wrapper.find('p.font-mono').classes()).toEqual(expect.arrayContaining([
      'whitespace-normal',
      'break-all',
    ]))
  })
})
