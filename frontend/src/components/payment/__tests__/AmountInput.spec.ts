import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function mountInput(value: number | null = null, currencySymbol = '$') {
  return mount(AmountInput, { props: { modelValue: value, currencySymbol } })
}

describe('recharge amount input', () => {
  it.each(['10abc', '10.555', '-10', '1e2'])(
    'restores the accepted amount after rejecting %s',
    async (value) => {
      const wrapper = mountInput(10)
      const input = wrapper.get('input')
      await input.setValue(value)
      expect((input.element as HTMLInputElement).value).toBe('10')
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    }
  )

  it('restores the last typed amount rather than a stale prop', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    await input.setValue('12.50')
    await input.setValue('12.500')
    expect((input.element as HTMLInputElement).value).toBe('12.50')
    expect(wrapper.emitted('update:modelValue')).toEqual([[12.5]])
  })

  it('preserves decimal editing and allows clearing the amount', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    for (const value of ['0', '0.', '0.5', '0.50', '']) await input.setValue(value)
    expect(wrapper.emitted('update:modelValue')).toEqual([[null], [null], [0.5], [0.5], [null]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('shows the configured payment currency symbol beside the custom amount', () => {
    const wrapper = mountInput(null, '¥')

    expect(wrapper.get('.relative > span').text()).toBe('¥')
  })
})
