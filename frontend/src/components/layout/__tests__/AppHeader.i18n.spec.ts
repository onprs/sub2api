import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { authStore } = vi.hoisted(() => ({
  authStore: {
    user: {
      id: 1,
      username: 'operator',
      email: 'operator@example.com',
      role: 'admin',
      balance: 0,
      frozen_balance: 0
    },
    isAdmin: true,
    isSimpleMode: false,
    logout: vi.fn()
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    ...actual,
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ meta: {}, params: {}, name: 'Dashboard' })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => ({
    contactInfo: '',
    docUrl: '',
    cachedPublicSettings: null,
    toggleMobileSidebar: vi.fn()
  }),
  useOnboardingStore: () => ({ replay: vi.fn() })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

import AppHeader from '../AppHeader.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

describe('AppHeader role label', () => {
  beforeEach(() => {
    testLocale.value = 'en'
  })

  it('renders the signed-in role through locale messages', () => {
    const english = mount(AppHeader, {
      global: {
        stubs: {
          AnnouncementBell: true,
          LocaleSwitcher: true,
          SubscriptionProgressMini: true,
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' }
        }
      }
    })

    expect(english.text()).toContain('Admin')
    expect(english.text()).toContain('¥0.00')

    testLocale.value = 'zh'
    const chinese = mount(AppHeader, {
      global: {
        stubs: {
          AnnouncementBell: true,
          LocaleSwitcher: true,
          SubscriptionProgressMini: true,
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' }
        }
      }
    })

    expect(chinese.text()).toContain('管理员')
    expect(chinese.text()).not.toContain('admin.users.roles')
  })
})
