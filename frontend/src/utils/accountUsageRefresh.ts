import type { Account } from '@/types'

const normalizeUsageRefreshValue = (value: unknown): string => {
  if (value == null) return ''
  return String(value)
}

export const buildOpenAIAPIKeyBalanceRefreshKey = (account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'credentials' | 'credentials_status' | 'proxy_id'>): string => {
  if (account.platform !== 'openai' || account.type !== 'apikey') return ''

  return [
    account.id,
    account.updated_at,
    account.proxy_id,
    account.credentials?.base_url,
    account.credentials_status?.has_api_key
  ].map(normalizeUsageRefreshValue).join('|')
}

export const buildOpenAIUsageRefreshKey = (account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'last_used_at' | 'rate_limit_reset_at' | 'extra'>): string => {
  if (account.platform !== 'openai' || account.type !== 'oauth') {
    return ''
  }

  const extra = account.extra ?? {}
  return [
    account.id,
    account.updated_at,
    account.last_used_at,
    account.rate_limit_reset_at,
    extra.codex_usage_updated_at,
    extra.codex_5h_used_percent,
    extra.codex_5h_reset_at,
    extra.codex_5h_reset_after_seconds,
    extra.codex_5h_window_minutes,
    extra.codex_7d_used_percent,
    extra.codex_7d_reset_at,
    extra.codex_7d_reset_after_seconds,
    extra.codex_7d_window_minutes
  ].map(normalizeUsageRefreshValue).join('|')
}

export const buildOpenCodeGoUsageRefreshKey = (account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'extra'>): string => {
  if (account.platform !== 'opencode_go' || account.type !== 'apikey') {
    return ''
  }

  const extra = account.extra ?? {}
  return [
    account.id,
    account.updated_at,
    extra.console_workspace_id,
    extra.opencode_go_console_auth_status,
    extra.opencode_go_console_auth_checked_at,
    extra.opencode_go_usage_source,
    extra.opencode_go_usage_updated_at,
    extra.opencode_go_usage_5h_used_percent,
    extra.opencode_go_usage_5h_reset_in_sec,
    extra.opencode_go_usage_5h_resets_at,
    extra.opencode_go_usage_7d_used_percent,
    extra.opencode_go_usage_7d_reset_in_sec,
    extra.opencode_go_usage_7d_resets_at,
    extra.opencode_go_usage_30d_used_percent,
    extra.opencode_go_usage_30d_reset_in_sec,
    extra.opencode_go_usage_30d_resets_at
  ].map(normalizeUsageRefreshValue).join('|')
}

export const buildClinePassUsageRefreshKey = (account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'extra'>): string => {
  if (account.platform !== 'clinepass' || account.type !== 'apikey') return ''

  const extra = account.extra ?? {}
  return [
    account.id,
    account.updated_at,
    extra.clinepass_usage_auth_status,
    extra.clinepass_usage_last_error_at,
    extra.clinepass_usage_source,
    extra.clinepass_usage_updated_at,
    extra.clinepass_usage_5h_used_percent,
    extra.clinepass_usage_5h_resets_at,
    extra.clinepass_usage_7d_used_percent,
    extra.clinepass_usage_7d_resets_at,
    extra.clinepass_usage_30d_used_percent,
    extra.clinepass_usage_30d_resets_at
  ].map(normalizeUsageRefreshValue).join('|')
}

export const buildOpenRouterUsageRefreshKey = (account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'extra'>): string => {
  if (account.platform !== 'openrouter' || account.type !== 'apikey') return ''

  const extra = account.extra ?? {}
  return [
    account.id,
    account.updated_at,
    extra.openrouter_usage_auth_status,
    extra.openrouter_usage_last_error_at,
    extra.openrouter_usage_source,
    extra.openrouter_usage_updated_at,
    extra.openrouter_usage_label,
    extra.openrouter_usage_used_usd,
    extra.openrouter_usage_limit_usd,
    extra.openrouter_usage_remaining_usd,
    extra.openrouter_usage_is_free_tier
  ].map(normalizeUsageRefreshValue).join('|')
}

export const buildAccountUsageRefreshKey = (
  account: Pick<Account, 'id' | 'platform' | 'type' | 'updated_at' | 'last_used_at' | 'rate_limit_reset_at' | 'extra' | 'credentials' | 'credentials_status' | 'proxy_id'>
): string => {
  return buildOpenAIUsageRefreshKey(account) || buildOpenAIAPIKeyBalanceRefreshKey(account) || buildOpenCodeGoUsageRefreshKey(account) || buildClinePassUsageRefreshKey(account) || buildOpenRouterUsageRefreshKey(account)
}
