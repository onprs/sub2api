import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ProviderIcon from '../ProviderIcon.vue'

describe('ProviderIcon', () => {
  it('renders the official Command Code Symbol at the requested size', () => {
    const wrapper = mount(ProviderIcon, {
      props: { provider: 'commandcode', size: 18 }
    })

    const icon = wrapper.get('[data-command-code-icon]')
    expect(icon.attributes('style')).toContain('width: 18px')
    expect(icon.attributes('style')).toContain('height: 18px')
    expect(icon.findAll('svg[viewBox="0 0 446 446"]')).toHaveLength(2)
    expect(icon.get('path').attributes('d')).toContain('M226.665 18.1979')
  })

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
