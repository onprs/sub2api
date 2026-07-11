import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('PlatformTypeBadge', () => {
  it('renders OpenCode Go as its own platform instead of the Gemini fallback', () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'opencode_go',
        type: 'apikey'
      }
    })

    expect(wrapper.text()).toContain('OpenCode Go')
    expect(wrapper.text()).not.toContain('Gemini')
    expect(wrapper.find('svg[stroke="currentColor"]').exists()).toBe(true)
  })
})
