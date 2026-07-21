import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageTable from '../UsageTable.vue'
import type { AdminUsageLog } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'usage.errors.categories.success': 'Success',
      })[key] ?? key,
    }),
  }
})

describe('UsageTable request metadata', () => {
  it('renders the request ID, success category, and status code', () => {
    const row = {
      request_id: 'req-visible-to-user',
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
    expect(wrapper.text()).toContain('Success')
    expect(wrapper.text()).toContain('200')
  })
})
