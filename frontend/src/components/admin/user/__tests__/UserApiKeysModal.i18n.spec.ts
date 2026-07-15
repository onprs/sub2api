import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getUserApiKeys, getAllGroups } = vi.hoisted(() => ({
  getUserApiKeys: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    ...actual,
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { getUserApiKeys },
    groups: { getAll: getAllGroups },
    apiKeys: { updateApiKeyGroup: vi.fn() }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))

import UserApiKeysModal from '../UserApiKeysModal.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

const user = {
  id: 5,
  email: 'user@example.com',
  username: 'user'
}

function apiKey(status: string) {
  return {
    id: 11,
    name: 'User key',
    key: 'sk-123456789012345678901234567890',
    status,
    group_id: null,
    created_at: '2026-01-01T00:00:00Z'
  }
}

async function mountModal(status: string) {
  getUserApiKeys.mockResolvedValue({ items: [apiKey(status)] })
  const wrapper = mount(UserApiKeysModal, {
    props: { show: false, user } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /></div>' },
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true
      }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('UserApiKeysModal status label', () => {
  beforeEach(() => {
    testLocale.value = 'en'
    getUserApiKeys.mockReset()
    getAllGroups.mockResolvedValue([])
  })

  it('renders API key statuses in English and Chinese', async () => {
    const english = await mountModal('quota_exhausted')
    expect(english.text()).toContain('Quota Exhausted')
    expect(english.text()).not.toContain('quota_exhausted')

    testLocale.value = 'zh'
    const chinese = await mountModal('expired')
    expect(chinese.text()).toContain('已过期')
    expect(chinese.text()).not.toContain('keys.status')
  })
})
