import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('AmountInput', () => {
  it('shows the payment currency symbol beside the custom amount', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        currencySymbol: '¥',
      },
    })

    expect(wrapper.get('.relative > span').text()).toBe('¥')
  })
})
