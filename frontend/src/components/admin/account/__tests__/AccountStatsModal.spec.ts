import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

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
    accounts: {
      getStats: vi.fn()
    }
  }
}))

import AccountStatsModal from '../AccountStatsModal.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

function mountModal(status: 'active' | 'inactive' | 'error') {
  return mount(AccountStatsModal, {
    props: {
      show: false,
      account: {
        id: 12,
        name: 'Stats Account',
        platform: 'openai',
        type: 'oauth',
        status
      }
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        LoadingSpinner: true,
        ModelDistributionChart: true,
        EndpointDistributionChart: true,
        Icon: true,
        Line: true
      }
    }
  })
}

describe('AccountStatsModal', () => {
  beforeEach(() => {
    testLocale.value = 'en'
  })

  it('renders localized account status in English and Chinese', () => {
    const english = mountModal('error')
    expect(english.text()).toContain('Error')

    testLocale.value = 'zh'
    const chinese = mountModal('inactive')
    expect(chinese.text()).toContain('停用')
    expect(chinese.text()).not.toContain('inactive')
  })
})
