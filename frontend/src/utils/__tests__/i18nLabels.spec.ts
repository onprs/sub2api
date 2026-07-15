import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { flattenLocaleKeys } from '@/i18n/localeUtils'
import {
  UNKNOWN_LABEL_KEY,
  accountStatusLabelKeys,
  accountTypeLabelKeys,
  apiKeyStatusLabelKeys,
  getAccountStatusLabelKey,
  getAccountTypeLabelKey,
  getApiKeyStatusLabelKey,
  getModerationCategoryLabelKey,
  getPaymentAuditActionLabelKey,
  getUserRoleLabelKey,
  moderationCategories,
  moderationCategoryLabelKeys,
  paymentAuditActionLabelKeys,
  paymentAuditActions,
  userRoleLabelKeys
} from '../i18nLabels'

const enKeys = new Set(flattenLocaleKeys(en))
const zhKeys = new Set(flattenLocaleKeys(zh))
const labelMaps = [
  accountTypeLabelKeys,
  accountStatusLabelKeys,
  userRoleLabelKeys,
  apiKeyStatusLabelKeys,
  paymentAuditActionLabelKeys,
  moderationCategoryLabelKeys
]

describe('i18n enum label contracts', () => {
  it('defines every mapped label in both locales', () => {
    const missing = labelMaps.flatMap(labels =>
      Object.values(labels).filter(key => !enKeys.has(key) || !zhKeys.has(key))
    )

    expect(missing).toEqual([])
  })

  it('covers every declared payment audit action and moderation category', () => {
    expect(Object.keys(paymentAuditActionLabelKeys).sort()).toEqual([...paymentAuditActions].sort())
    expect(Object.keys(moderationCategoryLabelKeys).sort()).toEqual([...moderationCategories].sort())
  })

  it('returns declared keys for known enum values', () => {
    expect(getAccountTypeLabelKey('setup-token')).toBe('admin.accounts.types.setup-token')
    expect(getAccountStatusLabelKey('error')).toBe('admin.accounts.status.error')
    expect(getUserRoleLabelKey('admin')).toBe('admin.users.roles.admin')
    expect(getApiKeyStatusLabelKey('quota_exhausted')).toBe('keys.status.quota_exhausted')
    expect(getPaymentAuditActionLabelKey('SUBSCRIPTION_SUCCESS')).toBe(
      'payment.admin.auditActions.SUBSCRIPTION_SUCCESS'
    )
    expect(getModerationCategoryLabelKey('harassment/threatening')).toBe(
      'admin.riskControl.categories.harassment/threatening'
    )
  })

  it('uses one localized fallback key for unknown backend values', () => {
    expect([
      getAccountTypeLabelKey('future_type'),
      getAccountStatusLabelKey('future_status'),
      getUserRoleLabelKey('future_role'),
      getApiKeyStatusLabelKey('future_key_status'),
      getPaymentAuditActionLabelKey('FUTURE_ACTION'),
      getModerationCategoryLabelKey('future/category')
    ]).toEqual(Array(6).fill(UNKNOWN_LABEL_KEY))
  })
})
