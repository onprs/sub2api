import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'
import { testLocale } from '@/i18n/__tests__/testTranslator'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { translateLocaleMessage } = await import('@/i18n/__tests__/testTranslator')
  return {
    ...actual,
    useI18n: () => ({ t: translateLocaleMessage })
  }
})

describe('PlatformTypeBadge', () => {
  it('renders OpenCode Go as its own platform instead of the Gemini fallback', () => {
    testLocale.value = 'en'
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'opencode_go',
        type: 'apikey'
      }
    })

    expect(wrapper.text()).toContain('OpenCode Go')
    expect(wrapper.text()).toContain('API Key')
    expect(wrapper.text()).not.toContain('Gemini')
    expect(wrapper.find('svg[stroke="currentColor"]').exists()).toBe(true)
  })

  it('renders ClinePass as an independent API key platform', () => {
    testLocale.value = 'en'
    const wrapper = mount(PlatformTypeBadge, {
      props: { platform: 'clinepass', type: 'apikey' }
    })

    expect(wrapper.text()).toContain('ClinePass')
    expect(wrapper.text()).toContain('API Key')
    expect(wrapper.text()).not.toContain('OpenCode Go')
  })

  it('uses Chinese account type labels without exposing the backend value', () => {
    testLocale.value = 'zh'
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'gemini',
        type: 'service_account'
      }
    })

    expect(wrapper.text()).toContain('服务账号')
    expect(wrapper.text()).not.toContain('service_account')
  })

  it('uses the localized fallback for an unknown account type', () => {
    testLocale.value = 'en'
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'openai',
        type: 'future_type'
      } as any
    })

    expect(wrapper.text()).toContain('Unknown (future_type)')
    expect(wrapper.text()).not.toContain('admin.accounts.types.future_type')
  })
})
