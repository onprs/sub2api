import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'
import type { UserErrorRequest } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'usage.errors.categories.upstream': 'Upstream error',
      })[key] ?? key,
    }),
  }
})

describe('UserErrorRequestsTable', () => {
  it('renders the user-owned request ID with error category and status', () => {
    const row: UserErrorRequest = {
      id: 1,
      created_at: '2026-07-22T00:00:00Z',
      request_id: 'req-error-visible-to-user',
      model: 'gpt-5.4',
      inbound_endpoint: '/v1/responses',
      status_code: 502,
      category: 'upstream',
      platform: 'openai',
      message: 'upstream failed',
      key_name: 'default',
      key_deleted: false,
    }

    const wrapper = mount(UserErrorRequestsTable, {
      props: {
        rows: [row],
        total: 1,
        loading: false,
        page: 1,
        pageSize: 20,
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
          Pagination: true,
          UserErrorDetailModal: true,
          IpGeoCell: true,
          IpGeoBatchToolbar: true,
        },
      },
    })

    expect(wrapper.text()).toContain('req-error-visible-to-user')
    expect(wrapper.text()).toContain('Upstream error')
    expect(wrapper.text()).toContain('502')
  })
})
