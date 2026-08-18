import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupGPT56CacheWriteInferenceFields from '../GroupGPT56CacheWriteInferenceFields.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('GroupGPT56CacheWriteInferenceFields', () => {
  it('仅在启用后显示阈值并提交分组配置', async () => {
    const wrapper = mount(GroupGPT56CacheWriteInferenceFields, {
      props: {
        enabled: false,
        minTokens: 1536,
        testIdPrefix: 'test-group'
      }
    })

    expect(wrapper.find('[data-testid="test-group-gpt56-cache-write-min-tokens"]').exists()).toBe(false)

    await wrapper.get('[data-testid="test-group-gpt56-cache-write-toggle"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])

    await wrapper.setProps({ enabled: true })
    const threshold = wrapper.get('[data-testid="test-group-gpt56-cache-write-min-tokens"]')
    expect((threshold.element as HTMLInputElement).value).toBe('1536')

    await threshold.setValue('2048')
    expect(wrapper.emitted('update:minTokens')?.at(-1)).toEqual([2048])
  })

  it('将无效阈值恢复为默认值', async () => {
    const wrapper = mount(GroupGPT56CacheWriteInferenceFields, {
      props: {
        enabled: true,
        minTokens: 1024,
        testIdPrefix: 'test-group'
      }
    })

    await wrapper.get('[data-testid="test-group-gpt56-cache-write-min-tokens"]').setValue('0')
    expect(wrapper.emitted('update:minTokens')?.at(-1)).toEqual([1024])
  })
})
