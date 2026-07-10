import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const checkAuth = vi.hoisted(() => vi.fn())
const fetchPublicSettings = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.login': 'Login',
  'home.dashboard': 'Dashboard',
  'home.goToDashboard': 'Dashboard',
  'home.getStarted': 'Get Started',
  'home.tags.subscriptionToApi': 'Subscription to API',
  'home.tags.stickySession': 'Sticky Session',
  'home.tags.realtimeBilling': 'Realtime Billing',
  'home.features.unifiedGateway': 'Unified Gateway',
  'home.features.unifiedGatewayDesc': 'Unified gateway description',
  'home.features.multiAccount': 'Multi Account',
  'home.features.multiAccountDesc': 'Multi account description',
  'home.features.balanceQuota': 'Balance and Quota',
  'home.features.balanceQuotaDesc': 'Balance and quota description',
  'home.providers.title': 'Supported Providers',
  'home.providers.description': 'Provider description',
  'home.providers.claude': 'Claude',
  'home.providers.gemini': 'Gemini',
  'home.providers.antigravity': 'Antigravity',
  'home.providers.supported': 'Supported',
  'home.providers.more': 'More',
  'home.providers.soon': 'Soon',
  'home.docs': 'Docs',
  'home.footer.allRightsReserved': 'All rights reserved.',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    isAuthenticated: false,
    isAdmin: false,
    user: null,
    checkAuth,
  }),
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    siteSubtitle: 'AI API Gateway Platform',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
  }),
}))

describe('HomeView footer', () => {
  beforeEach(() => {
    checkAuth.mockReset()
    fetchPublicSettings.mockReset()
    localStorage.clear()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the LM Speed verification badge in the default homepage footer', () => {
    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    const badgeLink = wrapper.find(
      'footer a[href="https://lmspeed.net/provider/api-onprs-top"]',
    )
    expect(badgeLink.exists()).toBe(true)
    expect(badgeLink.attributes('target')).toBe('_blank')
    expect(badgeLink.attributes('rel')).toBe('noopener noreferrer')

    const badgeImage = badgeLink.find(
      'img[src="https://lmspeed.net/api/provider/claim-badge/1420?claim=1420-pI3oIdhdh2Iekbg2DuIZuPDUska9-U9f"]',
    )
    expect(badgeImage.exists()).toBe(true)
    expect(badgeImage.attributes('alt')).toBe('Verified on LM Speed')

    wrapper.unmount()
  })
})
