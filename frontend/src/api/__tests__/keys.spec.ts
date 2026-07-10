import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosInstance } from 'axios'

vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN'
}))

describe('keys API', () => {
  let apiClient: AxiosInstance
  let downloadCliImportScript: typeof import('@/api/keys').downloadCliImportScript

  beforeEach(async () => {
    localStorage.clear()
    vi.resetModules()
    const clientMod = await import('@/api/client')
    const keysMod = await import('@/api/keys')
    apiClient = clientMod.apiClient
    downloadCliImportScript = keysMod.downloadCliImportScript
  })

  it('surfaces model capability metadata from CLI import blob errors', async () => {
    apiClient.defaults.adapter = vi.fn().mockResolvedValue({
      status: 400,
      data: new Blob([
        JSON.stringify({
          code: 400,
          reason: 'CLI_IMPORT_MODEL_CAPABILITY_UNKNOWN',
          message: 'OpenCode model capability metadata is incomplete',
          metadata: {
            models: 'unknown-model'
          }
        })
      ], { type: 'application/json' }),
      headers: {},
      config: {},
      statusText: 'Bad Request'
    })

    await expect(downloadCliImportScript(42, 'windows')).rejects.toThrow(
      'OpenCode model capability metadata is incomplete: unknown-model'
    )
  })
})
