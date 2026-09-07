import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'

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

describe('GroupOptionItem 倍率展示', () => {
  it('显示动态区间，并在专属倍率存在时折叠为覆盖值', async () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Dynamic group',
        platform: 'openai',
        rateMultiplier: 0.9,
        dynamicRateEnabled: true,
        dynamicRateMinMultiplier: 0.13,
        dynamicRateMaxMultiplier: 0.15,
      },
      global: { stubs: { GroupBadge: true } },
    })

    expect(wrapper.text()).toContain('0.13x-0.15x')
    await wrapper.setProps({ userRateMultiplier: 0.14 })
    expect(wrapper.find('.line-through').text()).toBe('0.13x-0.15x')
    expect(wrapper.text()).toContain('0.14x')
  })
})

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
})
