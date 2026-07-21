import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { queryRequests } from '@/api/usage'

beforeEach(() => {
  get.mockReset()
  get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
})

describe('usage request history API', () => {
  it('queries the unified success and error timeline with shared filters', async () => {
    const controller = new AbortController()
    const params = {
      page: 2,
      page_size: 20,
      category: 'upstream',
      status_code: 502,
      sort_by: 'created_at',
      sort_order: 'desc' as const,
    }

    await queryRequests(params, { signal: controller.signal })

    expect(get).toHaveBeenCalledWith('/usage/requests', {
      signal: controller.signal,
      params,
    })
  })
})
