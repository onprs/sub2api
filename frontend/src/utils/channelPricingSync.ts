export const CHANNEL_PRICING_UPDATED_EVENT = 'sub2api:channel-pricing-updated'

// 同一浏览器中的渠道编辑保存后，通知已打开的模型计费页重新读取。
export function notifyChannelPricingUpdated() {
  window.dispatchEvent(new Event(CHANNEL_PRICING_UPDATED_EVENT))
  try {
    localStorage.setItem(CHANNEL_PRICING_UPDATED_EVENT, `${Date.now()}:${Math.random()}`)
  } catch {
    // 浏览器禁用存储时，价格页仍通过定时刷新同步。
  }
}
