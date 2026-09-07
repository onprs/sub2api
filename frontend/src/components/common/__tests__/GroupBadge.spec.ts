import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

function mountBadge(userRateMultiplier: number | null = null) {
  return mount(GroupBadge, {
    props: {
      name: 'Dynamic group',
      platform: 'openai',
      rateMultiplier: 1,
      dynamicRateEnabled: true,
      dynamicRateMinMultiplier: 0.13,
      dynamicRateMaxMultiplier: 0.15,
      userRateMultiplier,
      alwaysShowRate: true,
    },
    global: {
      stubs: {
        PlatformIcon: true,
      },
    },
  })
}

describe('GroupBadge dynamic rate', () => {
  it('shows the configured multiplier range', () => {
    const wrapper = mountBadge()

    expect(wrapper.text()).toContain('0.13x-0.15x')
  })

  it('shows a user-specific multiplier as an override', () => {
    const wrapper = mountBadge(0.14)

    expect(wrapper.find('.line-through').text()).toBe('0.13x-0.15x')
    expect(wrapper.text()).toContain('0.14x')
  })
})
