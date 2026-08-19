/**
 * Channel monitor shared constants.
 *
 * Single source of truth for provider/status string values used by both the
 * admin (`views/admin/ChannelMonitorView.vue`) and user-facing
 * (`views/user/ChannelStatusView.vue`) screens, plus the shared composable
 * `useChannelMonitorFormat`.
 */

import type { APIMode, Provider, MonitorStatus } from '@/api/admin/channelMonitor'
import type { GroupPlatform } from '@/types'

export const PROVIDER_OPENAI: Provider = 'openai'
export const PROVIDER_ANTHROPIC: Provider = 'anthropic'
export const PROVIDER_GEMINI: Provider = 'gemini'
export const PROVIDER_ANTIGRAVITY_CLAUDE: Provider = 'antigravity_claude'
export const PROVIDER_ANTIGRAVITY_GEMINI: Provider = 'antigravity_gemini'
export const PROVIDER_OPENCODE_GO: Provider = 'opencode_go'
export const PROVIDER_CLINEPASS: Provider = 'clinepass'
export const PROVIDER_OPENROUTER: Provider = 'openrouter'

export const API_MODE_CHAT_COMPLETIONS: APIMode = 'chat_completions'
export const API_MODE_RESPONSES: APIMode = 'responses'
export const API_MODE_MESSAGES: APIMode = 'messages'

export const PROVIDERS: readonly Provider[] = [
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_ANTIGRAVITY_CLAUDE,
  PROVIDER_ANTIGRAVITY_GEMINI,
  PROVIDER_OPENCODE_GO,
  PROVIDER_CLINEPASS,
  PROVIDER_OPENROUTER,
]

export const API_MODES: readonly APIMode[] = [
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  API_MODE_MESSAGES,
]

export const STATUS_OPERATIONAL: MonitorStatus = 'operational'
export const STATUS_DEGRADED: MonitorStatus = 'degraded'
export const STATUS_FAILED: MonitorStatus = 'failed'
export const STATUS_ERROR: MonitorStatus = 'error'

export const MONITOR_STATUSES: readonly MonitorStatus[] = [
  STATUS_OPERATIONAL,
  STATUS_DEGRADED,
  STATUS_FAILED,
  STATUS_ERROR,
]

/** Default polling interval (seconds) for new monitors. */
export const DEFAULT_INTERVAL_SECONDS = 60

const MONITOR_PROVIDER_KEY_GROUP_PLATFORM: Record<Provider, GroupPlatform> = {
  openai: 'openai',
  anthropic: 'anthropic',
  gemini: 'gemini',
  antigravity_claude: 'antigravity',
  antigravity_gemini: 'antigravity',
  opencode_go: 'opencode_go',
  clinepass: 'clinepass',
  openrouter: 'openrouter',
}

export function monitorProviderKeyGroupPlatform(provider: Provider): GroupPlatform {
  return MONITOR_PROVIDER_KEY_GROUP_PLATFORM[provider]
}

export function monitorCurrentDomainEndpoint(provider: Provider, origin: string): string {
  const normalized = origin.replace(/\/+$/, '')
  if (provider === PROVIDER_OPENCODE_GO) {
    return `${normalized}/v1`
  }
  return normalized
}

export function monitorPayloadAPIMode(provider: Provider, apiMode: APIMode): APIMode {
  if (provider === PROVIDER_OPENAI) {
    return apiMode === API_MODE_RESPONSES ? API_MODE_RESPONSES : API_MODE_CHAT_COMPLETIONS
  }
  if (provider === PROVIDER_OPENCODE_GO) {
    if (apiMode === API_MODE_MESSAGES) return API_MODE_MESSAGES
    if (apiMode === API_MODE_RESPONSES) return API_MODE_RESPONSES
    return API_MODE_CHAT_COMPLETIONS
  }
  return API_MODE_CHAT_COMPLETIONS
}

export function monitorSelectableAPIModes(provider: Provider): readonly APIMode[] {
  if (provider === PROVIDER_OPENAI) {
    return [API_MODE_CHAT_COMPLETIONS, API_MODE_RESPONSES]
  }
  if (provider === PROVIDER_OPENCODE_GO) {
    return [API_MODE_CHAT_COMPLETIONS, API_MODE_RESPONSES, API_MODE_MESSAGES]
  }
  return [API_MODE_CHAT_COMPLETIONS]
}
