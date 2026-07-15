import type { Account, AccountType, ApiKey, User } from '@/types'

export const UNKNOWN_LABEL_KEY = 'common.unknownValue' as const

export const accountTypeLabelKeys = {
  oauth: 'admin.accounts.types.oauth',
  'setup-token': 'admin.accounts.types.setup-token',
  apikey: 'admin.accounts.types.apikey',
  upstream: 'admin.accounts.types.upstream',
  bedrock: 'admin.accounts.types.bedrock',
  service_account: 'admin.accounts.types.service_account'
} as const satisfies Record<AccountType, string>

export const accountStatusLabelKeys = {
  active: 'admin.accounts.status.active',
  inactive: 'admin.accounts.status.inactive',
  error: 'admin.accounts.status.error'
} as const satisfies Record<Account['status'], string>

export const userRoleLabelKeys = {
  admin: 'admin.users.roles.admin',
  user: 'admin.users.roles.user'
} as const satisfies Record<User['role'], string>

export const apiKeyStatusLabelKeys = {
  active: 'keys.status.active',
  inactive: 'keys.status.inactive',
  quota_exhausted: 'keys.status.quota_exhausted',
  expired: 'keys.status.expired'
} as const satisfies Record<ApiKey['status'], string>

export const paymentAuditActions = [
  'AFFILIATE_REBATE_APPLIED',
  'AFFILIATE_REBATE_FAILED',
  'AFFILIATE_REBATE_SKIPPED',
  'FULFILLMENT_FAILED',
  'ORDER_CANCELLED',
  'ORDER_CREATED',
  'ORDER_EXPIRED',
  'ORDER_PAID',
  'ORDER_RECOVERED',
  'PAYMENT_AFTER_EXPIRY',
  'PAYMENT_AMOUNT_MISMATCH',
  'PAYMENT_INVALID_AMOUNT',
  'PAYMENT_PROVIDER_METADATA_MISMATCH',
  'PAYMENT_PROVIDER_MISMATCH',
  'RECHARGE_RETRY',
  'RECHARGE_SUCCESS',
  'REFUND_FAILED',
  'REFUND_GATEWAY_FAILED',
  'REFUND_NO_TRADE_NO',
  'REFUND_PENDING',
  'REFUND_PROVIDER_METADATA_MISMATCH',
  'REFUND_QUERY_PENDING',
  'REFUND_REQUESTED',
  'REFUND_ROLLBACK_FAILED',
  'REFUND_SUCCESS',
  'SUBSCRIPTION_ASSIGNED',
  'SUBSCRIPTION_SUCCESS'
] as const

export type PaymentAuditAction = (typeof paymentAuditActions)[number]

export const paymentAuditActionLabelKeys = {
  AFFILIATE_REBATE_APPLIED: 'payment.admin.auditActions.AFFILIATE_REBATE_APPLIED',
  AFFILIATE_REBATE_FAILED: 'payment.admin.auditActions.AFFILIATE_REBATE_FAILED',
  AFFILIATE_REBATE_SKIPPED: 'payment.admin.auditActions.AFFILIATE_REBATE_SKIPPED',
  FULFILLMENT_FAILED: 'payment.admin.auditActions.FULFILLMENT_FAILED',
  ORDER_CANCELLED: 'payment.admin.auditActions.ORDER_CANCELLED',
  ORDER_CREATED: 'payment.admin.auditActions.ORDER_CREATED',
  ORDER_EXPIRED: 'payment.admin.auditActions.ORDER_EXPIRED',
  ORDER_PAID: 'payment.admin.auditActions.ORDER_PAID',
  ORDER_RECOVERED: 'payment.admin.auditActions.ORDER_RECOVERED',
  PAYMENT_AFTER_EXPIRY: 'payment.admin.auditActions.PAYMENT_AFTER_EXPIRY',
  PAYMENT_AMOUNT_MISMATCH: 'payment.admin.auditActions.PAYMENT_AMOUNT_MISMATCH',
  PAYMENT_INVALID_AMOUNT: 'payment.admin.auditActions.PAYMENT_INVALID_AMOUNT',
  PAYMENT_PROVIDER_METADATA_MISMATCH: 'payment.admin.auditActions.PAYMENT_PROVIDER_METADATA_MISMATCH',
  PAYMENT_PROVIDER_MISMATCH: 'payment.admin.auditActions.PAYMENT_PROVIDER_MISMATCH',
  RECHARGE_RETRY: 'payment.admin.auditActions.RECHARGE_RETRY',
  RECHARGE_SUCCESS: 'payment.admin.auditActions.RECHARGE_SUCCESS',
  REFUND_FAILED: 'payment.admin.auditActions.REFUND_FAILED',
  REFUND_GATEWAY_FAILED: 'payment.admin.auditActions.REFUND_GATEWAY_FAILED',
  REFUND_NO_TRADE_NO: 'payment.admin.auditActions.REFUND_NO_TRADE_NO',
  REFUND_PENDING: 'payment.admin.auditActions.REFUND_PENDING',
  REFUND_PROVIDER_METADATA_MISMATCH: 'payment.admin.auditActions.REFUND_PROVIDER_METADATA_MISMATCH',
  REFUND_QUERY_PENDING: 'payment.admin.auditActions.REFUND_QUERY_PENDING',
  REFUND_REQUESTED: 'payment.admin.auditActions.REFUND_REQUESTED',
  REFUND_ROLLBACK_FAILED: 'payment.admin.auditActions.REFUND_ROLLBACK_FAILED',
  REFUND_SUCCESS: 'payment.admin.auditActions.REFUND_SUCCESS',
  SUBSCRIPTION_ASSIGNED: 'payment.admin.auditActions.SUBSCRIPTION_ASSIGNED',
  SUBSCRIPTION_SUCCESS: 'payment.admin.auditActions.SUBSCRIPTION_SUCCESS'
} as const satisfies Record<PaymentAuditAction, string>

export const moderationCategories = [
  'harassment',
  'harassment/threatening',
  'hate',
  'hate/threatening',
  'illicit',
  'illicit/violent',
  'self-harm',
  'self-harm/intent',
  'self-harm/instructions',
  'sexual',
  'sexual/minors',
  'violence',
  'violence/graphic'
] as const

export type ModerationCategory = (typeof moderationCategories)[number]

export const moderationCategoryLabelKeys = {
  harassment: 'admin.riskControl.categories.harassment',
  'harassment/threatening': 'admin.riskControl.categories.harassment/threatening',
  hate: 'admin.riskControl.categories.hate',
  'hate/threatening': 'admin.riskControl.categories.hate/threatening',
  illicit: 'admin.riskControl.categories.illicit',
  'illicit/violent': 'admin.riskControl.categories.illicit/violent',
  'self-harm': 'admin.riskControl.categories.self-harm',
  'self-harm/intent': 'admin.riskControl.categories.self-harm/intent',
  'self-harm/instructions': 'admin.riskControl.categories.self-harm/instructions',
  sexual: 'admin.riskControl.categories.sexual',
  'sexual/minors': 'admin.riskControl.categories.sexual/minors',
  violence: 'admin.riskControl.categories.violence',
  'violence/graphic': 'admin.riskControl.categories.violence/graphic'
} as const satisfies Record<ModerationCategory, string>

function getLabelKey(labels: Readonly<Record<string, string>>, value: string): string {
  return Object.prototype.hasOwnProperty.call(labels, value) ? labels[value] : UNKNOWN_LABEL_KEY
}

export function getAccountTypeLabelKey(value: string): string {
  return getLabelKey(accountTypeLabelKeys, value)
}

export function getAccountStatusLabelKey(value: string): string {
  return getLabelKey(accountStatusLabelKeys, value)
}

export function getUserRoleLabelKey(value: string): string {
  return getLabelKey(userRoleLabelKeys, value)
}

export function getApiKeyStatusLabelKey(value: string): string {
  return getLabelKey(apiKeyStatusLabelKeys, value)
}

export function getPaymentAuditActionLabelKey(value: string): string {
  return getLabelKey(paymentAuditActionLabelKeys, value)
}

export function getModerationCategoryLabelKey(value: string): string {
  return getLabelKey(moderationCategoryLabelKeys, value)
}
