import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { Account } from '@/types'
import ZCodeOAuthPanel from '../ZCodeOAuthPanel.vue'
import ZCodeQuotaCell from '../ZCodeQuotaCell.vue'
import { defaultZCodeForm, zcodeCredentials } from '../zcodeForm'
import zh from '@/i18n/locales/zh'

const api = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), quota: vi.fn(), preview: vi.fn(), claim: vi.fn() }))
vi.mock('@/api/admin/zcode', () => ({ zcodeAPI: api }))
const makeI18n = () => createI18n({ legacy: false, locale: 'zh', messageCompiler: message => () => String(message), messages: { zh }, missingWarn: false, fallbackWarn: false })
const account = (extra: Record<string, unknown> = {}) => ({ id: 91, platform: 'zhipu', type: 'oauth', credentials: { auth_mode: 'zcode_oauth', account_mode: 'start', zcode_provider: 'zai' }, extra }) as Account

describe('ZCode OAuth 管理界面', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks() })
  afterEach(() => { vi.useRealTimers() })

  it('网页登录完成后仅向表单提交会话 ID', async () => {
    api.start.mockResolvedValue({ session_id: 'synthetic-session', authorize_url: 'https://chat.z.ai/oauth?state=synthetic', status: 'pending', expires_at: Math.floor(Date.now() / 1000) + 60, poll_interval: 1, plan: 'start' })
    api.poll.mockResolvedValue({ session_id: 'synthetic-session', status: 'ready', expires_at: Math.floor(Date.now() / 1000) + 60, poll_interval: 1, plan: 'start' })
    const wrapper = mount(ZCodeOAuthPanel, { props: { modelValue: defaultZCodeForm() }, global: { plugins: [makeI18n()] } })
    expect(wrapper.find('input[type="checkbox"]').element).toHaveProperty('checked', false)
    await wrapper.get('[data-test="zcode-login"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('a').attributes('href')).toContain('https://chat.z.ai/oauth')
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    const value = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ReturnType<typeof defaultZCodeForm>
    expect(value.ready).toBe(true)
    expect(zcodeCredentials(value)).toEqual({ auth_mode: 'zcode_oauth', account_mode: 'start', zcode_provider: 'zai', zcode_auto_claim: false, api_protocol: 'anthropic', zcode_oauth_session_id: 'synthetic-session' })
    expect(wrapper.findAll('input[type="text"]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('关闭面板后取消待定轮询', async () => {
    api.start.mockResolvedValue({ session_id: 'synthetic-session', authorize_url: 'https://chat.z.ai/oauth', status: 'pending', expires_at: Math.floor(Date.now() / 1000) + 60, poll_interval: 1, plan: 'start' })
    const wrapper = mount(ZCodeOAuthPanel, { props: { modelValue: defaultZCodeForm() }, global: { plugins: [makeI18n()] } })
    await wrapper.get('[data-test="zcode-login"]').trigger('click'); await flushPromises(); wrapper.unmount()
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.poll).not.toHaveBeenCalled()
  })

  it('账号级开关及 claim 状态、生效时间正常展示', async () => {
    const wrapper = mount(ZCodeOAuthPanel, { props: { modelValue: defaultZCodeForm(), account: account({ zcode_claim: { result: 'claimed', checked_at: 1700000000, next_attempt: 2000000000, plan: { id: 'synthetic', show_name: '合成体验套餐', starts_at: 1800000000, ends_at: 2000000000 } } }) }, global: { plugins: [makeI18n()], stubs: { ZCodeQuotaCell: true } } })
    expect(wrapper.text()).toContain('领取成功')
    expect(wrapper.text()).toContain('合成体验套餐')
    expect(wrapper.text()).toContain('生效时间')
    await wrapper.get('[data-test="zcode-auto-claim"]').setValue(true)
    const value = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as ReturnType<typeof defaultZCodeForm>
    expect(value.autoClaim).toBe(true)
    wrapper.unmount()
  })

  it('未知 total 保留为空，挂载只读快照', async () => {
    const wrapper = mount(ZCodeQuotaCell, { props: { account: account({ zcode_quota: { plan: 'start', balances: [{ name: '合成积分', remaining: 1250, unit: 'points', expires_at: 2000000000 }], updated_at: 1700000000 } }) }, global: { plugins: [makeI18n()] } })
    expect(api.quota).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="zcode-remaining"]').text()).not.toContain('/')
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('到期时间')
    api.quota.mockResolvedValue({ plan: 'start', balances: [{ name: '合成积分', remaining: 100 }], updated_at: 1700000001 })
    await wrapper.get('[data-test="zcode-quota-refresh"]').trigger('click'); await flushPromises()
    expect(api.quota).toHaveBeenCalledTimes(1)
    expect(api.quota).toHaveBeenCalledWith(91)
    expect(wrapper.text()).toContain('100')
    wrapper.unmount()
  })

  it('有进度条时以进度为主，精确剩余收纳于名字旁的信息提示', async () => {
    const wrapper = mount(ZCodeQuotaCell, { props: { account: account({ zcode_quota: { plan: 'start', balances: [{ name: 'GLM-5.3-Flash', remaining: 99998232, total: 100000000, used_percent: 0, unit: 'token', expires_at: 1791216000 }], updated_at: 1700000000 } }) }, global: { plugins: [makeI18n()] } })
    expect(wrapper.find('[data-test="zcode-remaining"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('99,998,232')
    expect(wrapper.find('svg.cursor-help').exists()).toBe(true)
    const tooltip = document.querySelector('[role="tooltip"]')
    expect(tooltip?.textContent).toContain('剩余: 99,998,232 / 100,000,000 token')
    expect(tooltip?.textContent).toContain('到期时间')
    expect(tooltip?.textContent).toContain('额度更新时间')
    expect(wrapper.text()).toContain('GLM-5.3-Flash')
    wrapper.unmount()
  })
})
