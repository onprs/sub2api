import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

vi.mock('@/api/keys', () => ({
  downloadCliImportScript: vi.fn().mockResolvedValue(undefined)
}))

import UseKeyModal from '../UseKeyModal.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

const apiKeyRecord = {
  id: 9,
  name: 'Primary key',
  status: 'active',
  expires_at: null,
  quota: 0,
  quota_used: 0,
  group_id: 3,
  group: {
    id: 3,
    name: 'OpenCode Go',
    platform: 'opencode_go',
    default_mapped_model: 'gpt-5.5'
  }
}

function mountModal() {
  return mount(UseKeyModal, {
    props: {
      show: true,
      apiKey: 'sk-test',
      baseUrl: 'https://example.com/v1',
      platform: 'opencode_go',
      apiKeyRecord
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true
      }
    }
  })
}

describe('UseKeyModal locale copy', () => {
  beforeEach(() => {
    testLocale.value = 'en'
  })

  it('renders CLI import fields from real English and Chinese locale messages', () => {
    const english = mountModal()
    expect(english.text()).toContain('Default Model')
    expect(english.text()).toContain('Download for Windows')
    expect(english.text()).toContain('Import this API key into OpenCode')
    expect(english.text()).not.toContain('keys.useKeyModal')

    testLocale.value = 'zh'
    const chinese = mountModal()
    expect(chinese.text()).toContain('默认模型')
    expect(chinese.text()).toContain('下载 Windows 脚本')
    expect(chinese.text()).toContain('将此 API 密钥导入 OpenCode')
    expect(chinese.text()).not.toContain('keys.useKeyModal')
  })
})
