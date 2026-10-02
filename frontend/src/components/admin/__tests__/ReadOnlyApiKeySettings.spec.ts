import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zhSettings from '@/i18n/locales/zh/admin/settings'
import enSettings from '@/i18n/locales/en/admin/settings'
import ReadOnlyApiKeySettings from '../ReadOnlyApiKeySettings.vue'

const { getStatus, regenerate, remove, copy, showSuccess, showError, confirm } = vi.hoisted(() => ({
  getStatus: vi.fn(), regenerate: vi.fn(), remove: vi.fn(), copy: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(), confirm: vi.fn()
}))

vi.mock('@/api', () => ({ adminAPI: { settings: {
  getAdminReadOnlyApiKey: getStatus,
  regenerateAdminReadOnlyApiKey: regenerate,
  deleteAdminReadOnlyApiKey: remove
} } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('@/utils/apiError', () => ({ extractApiErrorMessage: () => '请求失败' }))

function mountCard(locale = 'zh-CN') {
  // 仓库测试使用精简国际化运行时，在此按实际文案生成消息函数。
  const i18n = createI18n({ legacy: false, locale, messageCompiler: message => () => String(message), messages: {
    'zh-CN': { admin: zhSettings, common: { loading: '加载中', error: '发生错误' } },
    en: { admin: enSettings, common: { loading: 'Loading', error: 'Error' } }
  } })
  return mount(ReadOnlyApiKeySettings, { global: { plugins: [i18n] } })
}

function button(wrapper: ReturnType<typeof mountCard>, label: string) {
  const found = wrapper.findAll('button').find(item => item.text() === label)
  expect(found, label).toBeDefined()
  return found!
}

describe('只读 API Key 设置', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getStatus.mockResolvedValue({ exists: false, masked_key: '' })
    confirm.mockReturnValue(true)
    copy.mockResolvedValue(true)
    vi.stubGlobal('confirm', confirm)
  })
  afterEach(() => { vi.unstubAllGlobals() })

  it('创建后显示完整密钥，并支持复制、轮换和删除', async () => {
    const first = 's2ro_' + 'a'.repeat(64)
    const second = 's2ro_' + 'b'.repeat(64)
    regenerate.mockResolvedValueOnce({ key: first }).mockResolvedValueOnce({ key: second })
    remove.mockResolvedValue({ message: '已删除' })
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.text()).toContain('尚未配置只读 API Key')
    await button(wrapper, '创建只读密钥').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(first)
    expect(wrapper.text()).toContain('s2ro_aaaaa...aaaa')
    expect(wrapper.text()).toContain('此密钥仅显示一次')
    await button(wrapper, '复制密钥').trigger('click')
    expect(copy).toHaveBeenCalledWith(first, '密钥已复制到剪贴板')
    await button(wrapper, '重新生成').trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('当前只读密钥将立即失效'))
    expect(wrapper.text()).not.toContain(first)
    expect(wrapper.text()).toContain(second)
    await button(wrapper, '删除').trigger('click')
    await flushPromises()
    expect(remove).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain(second)
    expect(wrapper.text()).toContain('尚未配置只读 API Key')
    expect(showSuccess).toHaveBeenCalledWith('只读 API Key 已删除')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('刷新页面只显示脱敏状态，取消确认保留现有密钥', async () => {
    getStatus.mockResolvedValue({ exists: true, masked_key: 's2ro_aaaaa...aaaa' })
    confirm.mockReturnValue(false)
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.text()).toContain('s2ro_aaaaa...aaaa')
    expect(wrapper.text()).not.toContain('此密钥仅显示一次')
    await button(wrapper, '重新生成').trigger('click')
    await button(wrapper, '删除').trigger('click')
    expect(regenerate).not.toHaveBeenCalled()
    expect(remove).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('s2ro_aaaaa...aaaa')
    wrapper.unmount()
  })

  it('状态加载失败时提供重试，查询恢复后再允许创建', async () => {
    getStatus.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({ exists: false, masked_key: '' })
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('读取只读密钥状态失败')
    expect(wrapper.findAll('button').map(item => item.text())).toEqual(['重试'])
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(button(wrapper, '创建只读密钥').exists()).toBe(true)
    expect(getStatus).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('操作失败时保留已有状态，并阻止重复请求', async () => {
    getStatus.mockResolvedValue({ exists: true, masked_key: 's2ro_aaaaa...aaaa' })
    let reject: (error: Error) => void = () => {}
    regenerate.mockImplementation(() => new Promise((_, rejectPromise) => { reject = rejectPromise }))
    const wrapper = mountCard()
    await flushPromises()
    await button(wrapper, '重新生成').trigger('click')
    expect(wrapper.findAll('button').filter(item => item.attributes('disabled') !== undefined)).toHaveLength(2)
    expect(regenerate).toHaveBeenCalledTimes(1)
    reject(new Error('network'))
    await flushPromises()
    expect(wrapper.text()).toContain('s2ro_aaaaa...aaaa')
    expect(showError).toHaveBeenCalledWith('请求失败')
    remove.mockRejectedValueOnce(new Error('network'))
    await button(wrapper, '删除').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('s2ro_aaaaa...aaaa')
    wrapper.unmount()
  })

  it('英文界面显示完整的用户文案，所有按钮独立于全局保存表单', async () => {
    const wrapper = mountCard('en')
    await flushPromises()
    expect(wrapper.text()).toContain('Read-only API Key')
    expect(wrapper.text()).toContain('Create Read-only Key')
    expect(wrapper.text()).not.toContain('admin.settings.')
    for (const item of wrapper.findAll('button')) expect(item.attributes('type')).toBe('button')
    wrapper.unmount()
  })
})
