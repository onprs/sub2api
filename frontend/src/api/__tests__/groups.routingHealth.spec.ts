import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { InternalAxiosRequestConfig } from 'axios'

vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN'
}))

describe('分组路由健康 API', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
  })

  it('去重并按后端上限分批读取用户分组健康数据', async () => {
    const { apiClient } = await import('@/api/client')
    const { getRoutingHealth } = await import('@/api/groups')
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => {
      const groupIDs = String(config.params.group_ids)
        .split(',')
        .map(Number)
      return {
        status: 200,
        data: {
          code: 0,
          message: 'ok',
          data: {
            window_minutes: 10080,
            window_days: 7,
            items: groupIDs.map((groupID) => ({
              group_id: groupID,
              status: 'unknown',
              success_rate: null,
              average_latency_ms: null,
              sample_count: 0,
              last_observed_at: null
            }))
          }
        },
        headers: {},
        config,
        statusText: 'OK'
      }
    })
    apiClient.defaults.adapter = adapter

    const requested = [...Array.from({ length: 201 }, (_, index) => index + 1), 1, 0, -1]
    const result = await getRoutingHealth(requested)

    expect(adapter).toHaveBeenCalledTimes(2)
    expect(adapter.mock.calls[0][0].url).toBe('/groups/routing-health')
    expect(String(adapter.mock.calls[0][0].params.group_ids).split(',')).toHaveLength(200)
    expect(adapter.mock.calls[1][0].params.group_ids).toBe('201')
    expect(result.items).toHaveLength(201)
    expect(result.window_minutes).toBe(10080)
    expect(result.window_days).toBe(7)
    expect(result.items[200].group_id).toBe(201)
  })

  it('管理员读取使用管理员路由', async () => {
    const { apiClient } = await import('@/api/client')
    const { getRoutingHealth } = await import('@/api/admin/groups')
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => ({
      status: 200,
      data: {
        code: 0,
        message: 'ok',
        data: { window_minutes: 10080, window_days: 7, items: [] }
      },
      headers: {},
      config,
      statusText: 'OK'
    }))
    apiClient.defaults.adapter = adapter

    await getRoutingHealth([7])

    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].url).toBe('/admin/groups/routing-health')
    expect(adapter.mock.calls[0][0].params.group_ids).toBe('7')
  })
})
