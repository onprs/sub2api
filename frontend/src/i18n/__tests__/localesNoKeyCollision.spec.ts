import { describe, expect, it } from 'vitest'

import enAdminAccounts from '../locales/en/admin/accounts'
import enAdminChannels from '../locales/en/admin/channels'
import enAdminOps from '../locales/en/admin/ops'
import enAdminOverview from '../locales/en/admin/overview'
import enAdminResources from '../locales/en/admin/resources'
import enAdminSettings from '../locales/en/admin/settings'
import enCommon from '../locales/en/common'
import enDashboard from '../locales/en/dashboard'
import enLanding from '../locales/en/landing'
import enMisc from '../locales/en/misc'
import enLocale from '../locales/en'
import zhAdminAccounts from '../locales/zh/admin/accounts'
import zhAdminChannels from '../locales/zh/admin/channels'
import zhAdminOps from '../locales/zh/admin/ops'
import zhAdminOverview from '../locales/zh/admin/overview'
import zhAdminResources from '../locales/zh/admin/resources'
import zhAdminSettings from '../locales/zh/admin/settings'
import zhCommon from '../locales/zh/common'
import zhDashboard from '../locales/zh/dashboard'
import zhLanding from '../locales/zh/landing'
import zhMisc from '../locales/zh/misc'
import zhLocale from '../locales/zh'

// locales/{zh,en}/index.ts 与 admin/index.ts 使用对象展开聚合各域模块，
// 展开模块之间若出现同名顶层键会静默覆盖。本测试将该风险固化为显式失败。
type Modules = Record<string, Record<string, unknown>>

function collisions(modules: Modules): string[] {
  const seen = new Map<string, string>()
  const out: string[] = []
  for (const [name, mod] of Object.entries(modules)) {
    for (const key of Object.keys(mod)) {
      const prev = seen.get(key)
      if (prev) {
        out.push(`"${key}" in both ${prev} and ${name}`)
      } else {
        seen.set(key, name)
      }
    }
  }
  return out
}

const roots: Record<string, Modules> = {
  zh: { landing: zhLanding, common: zhCommon, dashboard: zhDashboard, misc: zhMisc },
  en: { landing: enLanding, common: enCommon, dashboard: enDashboard, misc: enMisc }
}

const admins: Record<string, Modules> = {
  zh: {
    overview: zhAdminOverview,
    channels: zhAdminChannels,
    accounts: zhAdminAccounts,
    resources: zhAdminResources,
    ops: zhAdminOps,
    settings: zhAdminSettings
  },
  en: {
    overview: enAdminOverview,
    channels: enAdminChannels,
    accounts: enAdminAccounts,
    resources: enAdminResources,
    ops: enAdminOps,
    settings: enAdminSettings
  }
}

describe.each(Object.keys(roots))('locale %s spread assembly', (locale) => {
  it('root modules have no overlapping top-level keys', () => {
    expect(collisions(roots[locale])).toEqual([])
  })

  it('root modules do not shadow the explicit "admin" namespace', () => {
    for (const [name, mod] of Object.entries(roots[locale])) {
      expect(Object.keys(mod), `module ${name} must not define "admin"`).not.toContain('admin')
    }
  })

  it('admin modules have no overlapping top-level keys', () => {
    expect(collisions(admins[locale])).toEqual([])
  })
})

const regressionKeys = [
  'nav.modelPricing',
  'modelPricing.searchPlaceholder',
  'modelPricing.columns.channel',
  'modelPricing.columns.platform',
  'modelPricing.columns.model',
  'modelPricing.columns.group',
  'modelPricing.columns.multiplier',
  'modelPricing.columns.source',
  'modelPricing.columns.billingMode',
  'modelPricing.columns.inputPerMillion',
  'modelPricing.columns.outputPerMillion',
  'modelPricing.columns.cacheWritePerMillion',
  'modelPricing.columns.cacheReadPerMillion',
  'modelPricing.columns.unitPrice',
  'payment.quotaWindows.fiveHour',
  'payment.quotaWindows.fiveHourShort',
  'payment.quotaWindows.sevenDay',
  'payment.quotaWindows.sevenDayShort',
  'payment.quotaWindows.thirtyDay',
  'payment.quotaWindows.thirtyDayShort',
  'payment.renewalDiscount',
  'payment.planSoldOut',
  'payment.stock.label',
  'payment.stock.unlimited',
  'payment.stock.remaining',
  'payment.stock.soldOut',
  'admin.channelMonitor.searchPlaceholder',
  'admin.channelMonitor.allProviders',
  'admin.channelMonitor.enabledFilter',
  'admin.channelMonitor.createButton',
  'admin.channelMonitor.columns.name',
  'admin.channelMonitor.columns.provider',
  'admin.channelMonitor.columns.primaryModel',
  'admin.channelMonitor.columns.availability7d',
  'admin.channelMonitor.columns.latency',
  'admin.channelMonitor.columns.enabled',
  'admin.channelMonitor.columns.actions',
  'admin.channelMonitor.form.apiModeMessages',
  'admin.channelMonitor.form.apiModeMessagesHint',
  'admin.subscriptions.columns.actions',
  'admin.subscriptions.adjust',
  'admin.subscriptions.resetQuota',
  'admin.subscriptions.revoke',
  'admin.subscriptions.restore',
  'admin.groups.platforms.opencode_go',
  'admin.groups.platforms.clinepass',
  'admin.accounts.platforms.opencode_go',
  'admin.accounts.platforms.clinepass',
  'monitorCommon.providers.antigravity_claude',
  'monitorCommon.providers.antigravity_gemini',
  'monitorCommon.providers.opencode_go',
  'monitorCommon.providers.clinepass'
] as const

function resolveMessage(messages: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, part) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[part]
  }, messages)
}

describe.each([
  ['zh', zhLocale],
  ['en', enLocale]
] as const)('locale %s merge-regression contracts', (_locale, messages) => {
  it.each(regressionKeys)('defines %s', (key) => {
    const value = resolveMessage(messages, key)
    expect(value, `${key} must survive locale module assembly`).toBeTypeOf('string')
    expect(value).not.toBe(key)
  })
})
