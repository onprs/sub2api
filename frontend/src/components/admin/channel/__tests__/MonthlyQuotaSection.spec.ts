import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonthlyQuotaSection from '../MonthlyQuotaSection.vue'
import type { ChannelMonthlyQuota } from '@/api/admin/channels'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('MonthlyQuotaSection', () => {
  it('以 UTC 编辑额度活动时间，并保留规则中的其他字段', async () => {
    const quota: ChannelMonthlyQuota = {
      base_credits_usd: 20,
      periods: [{ credits_usd: 40, starts_at: '2026-10-06T08:00:00+08:00', expires_at: '2026-10-21T00:00:00Z', term: 'through Oct 20th' }],
    }
    const wrapper = mount(MonthlyQuotaSection, { props: { modelValue: quota } })
    const dates = wrapper.findAll<HTMLInputElement>('input[type="datetime-local"]')
    expect(dates[0].element.value).toBe('2026-10-06T00:00')
    await dates[1].setValue('2026-10-22T01:02:03')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({
      base_credits_usd: 20,
      periods: [{ ...quota.periods![0], expires_at: '2026-10-22T01:02:03.000Z' }],
    })
    expect(quota.periods![0].expires_at).toBe('2026-10-21T00:00:00Z')
    wrapper.unmount()
  })

  it('关闭月额度折算后提交空规则', async () => {
    const wrapper = mount(MonthlyQuotaSection, { props: { modelValue: { base_credits_usd: 35 } } })
    await wrapper.get('input[type="checkbox"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBeNull()
    wrapper.unmount()
  })
})
