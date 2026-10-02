import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, remove } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  remove: vi.fn()
}))

vi.mock('@/api/client', () => ({ apiClient: { get, post, delete: remove } }))

import settingsAPI, {
  deleteAdminReadOnlyApiKey,
  getAdminReadOnlyApiKey,
  regenerateAdminReadOnlyApiKey
} from '@/api/admin/settings'

describe('只读密钥管理 API', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('通过独立接口查询状态、生成和删除密钥', async () => {
    get.mockResolvedValueOnce({ data: { exists: false, masked_key: '' } })
    post.mockResolvedValueOnce({ data: { key: 'synthetic-read-only-key' } })
    remove.mockResolvedValueOnce({ data: { message: '已删除' } })

    await expect(getAdminReadOnlyApiKey()).resolves.toEqual({ exists: false, masked_key: '' })
    await expect(regenerateAdminReadOnlyApiKey()).resolves.toEqual({ key: 'synthetic-read-only-key' })
    await expect(deleteAdminReadOnlyApiKey()).resolves.toEqual({ message: '已删除' })
    expect(get).toHaveBeenCalledWith('/admin/settings/admin-read-only-api-key')
    expect(post).toHaveBeenCalledWith('/admin/settings/admin-read-only-api-key/regenerate')
    expect(remove).toHaveBeenCalledWith('/admin/settings/admin-read-only-api-key')
    expect(settingsAPI.getAdminReadOnlyApiKey).toBe(getAdminReadOnlyApiKey)
    expect(settingsAPI.regenerateAdminReadOnlyApiKey).toBe(regenerateAdminReadOnlyApiKey)
    expect(settingsAPI.deleteAdminReadOnlyApiKey).toBe(deleteAdminReadOnlyApiKey)
  })
})
