import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ProviderIcon from '../ProviderIcon.vue'

describe('ProviderIcon', () => {
  it('renders the official Cline Bot Icon for ClinePass', () => {
    const wrapper = mount(ProviderIcon, {
      props: { provider: 'clinepass', size: 18 }
    })

    const icon = wrapper.get('svg[viewBox="0 0 466.73 487.04"]')
    expect(icon.attributes('width')).toBe('18')
    expect(icon.attributes('height')).toBe('18')
    expect(icon.get('path').attributes('d')).toContain('M463.6,275.08')
  })
})
