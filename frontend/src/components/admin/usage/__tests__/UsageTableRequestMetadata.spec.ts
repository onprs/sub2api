import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageTable from '../UsageTable.vue'
import type { AdminUsageLog, UserRequestRecord } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'usage.errors.categories.success': 'Success',
        'usage.errors.categories.upstream': 'Upstream error',
      })[key] ?? key,
    }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn(),
  }),
}))

describe('UsageTable request metadata', () => {
  it('renders the request ID, success category, and status code', () => {
    const row = {
      request_id: 'client:req-visible-to-user',
      status_code: 200,
      category: 'success',
    } as AdminUsageLog

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        columns: [
          { key: 'request_id', label: 'Request ID' },
          { key: 'category', label: 'Category' },
          { key: 'status', label: 'Status' },
        ],
        flat: true,
        formatRequestIds: true,
      },
      global: {
        stubs: {
          DataTable: {
            props: ['data'],
            template: `
              <div>
                <slot name="cell-request_id" :row="data[0]" />
                <slot name="cell-category" :row="data[0]" />
                <slot name="cell-status" :row="data[0]" />
              </div>
            `,
          },
          EmptyState: true,
          Icon: true,
          IpGeoCell: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('req-visible-to-user')
    expect(wrapper.text()).not.toContain('client:')
    const requestIdText = wrapper.find('span')
    expect(requestIdText.classes()).toEqual(expect.arrayContaining([
      'whitespace-normal',
      'break-all',
    ]))
    expect(requestIdText.element.parentElement?.classList.contains('max-w-[220px]')).toBe(true)
    expect(wrapper.text()).toContain('Success')
    expect(wrapper.text()).toContain('200')
  })

  it('renders errors in the same table without fake billing values and opens details', async () => {
    const row = {
      record_type: 'error',
      id: 501,
      request_id: 'client:req-error',
      status_code: 502,
      category: 'upstream',
      message: 'upstream failed',
      input_tokens: 0,
      output_tokens: 0,
      actual_cost: 0,
      total_cost: 0,
      image_count: 0,
    } as UserRequestRecord

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        columns: [
          { key: 'request_id', label: 'Request ID' },
          { key: 'category', label: 'Category' },
          { key: 'status', label: 'Status' },
          { key: 'message', label: 'Message' },
          { key: 'billing_mode', label: 'Billing' },
          { key: 'tokens', label: 'Tokens' },
          { key: 'cost', label: 'Cost' },
          { key: 'latency', label: 'Latency' },
        ],
        flat: true,
        formatRequestIds: true,
      },
      global: {
        stubs: {
          DataTable: {
            props: ['data'],
            template: `
              <div>
                <slot name="cell-request_id" :row="data[0]" />
                <slot name="cell-category" :row="data[0]" />
                <slot name="cell-status" :row="data[0]" />
                <slot name="cell-message" :row="data[0]" />
                <slot name="cell-billing_mode" :row="data[0]" />
                <slot name="cell-tokens" :row="data[0]" />
                <slot name="cell-cost" :row="data[0]" />
                <slot name="cell-latency" :row="data[0]" />
              </div>
            `,
          },
          EmptyState: true,
          Icon: true,
          IpGeoCell: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('req-error')
    expect(wrapper.text()).toContain('Upstream error')
    expect(wrapper.text()).toContain('502')
    expect(wrapper.text()).toContain('upstream failed')
    expect(wrapper.text()).not.toContain('client:')
    expect(wrapper.text()).not.toContain('0.000000')

    await wrapper.get('button[title="upstream failed"]').trigger('click')
    expect(wrapper.emitted('errorClick')).toEqual([[501]])
  })
})
