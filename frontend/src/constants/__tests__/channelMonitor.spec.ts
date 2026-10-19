import { describe, expect, it } from 'vitest'

import {
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_MESSAGES,
  API_MODE_RESPONSES,
  monitorProviderSupportsProbe,
  monitorSelectableAPIModes,
  monitorPayloadAPIMode,
  PROVIDER_ANTHROPIC,
  PROVIDER_ANTIGRAVITY,
  PROVIDER_CLINEPASS,
  PROVIDER_OPENROUTER,
  PROVIDER_OPENCODE_GO,
  PROVIDER_OPENAI,
  PROVIDERS,
  QUOTA_ONLY_PROVIDERS,
} from '../channelMonitor'

describe('channel monitor constants', () => {
  it('preserves OpenCode Go messages mode in monitor payloads', () => {
    expect(monitorPayloadAPIMode(PROVIDER_OPENCODE_GO, API_MODE_MESSAGES)).toBe(API_MODE_MESSAGES)
    expect(monitorPayloadAPIMode(PROVIDER_OPENCODE_GO, API_MODE_RESPONSES)).toBe(API_MODE_RESPONSES)
    expect(monitorPayloadAPIMode(PROVIDER_OPENAI, API_MODE_RESPONSES)).toBe(API_MODE_RESPONSES)
    expect(monitorPayloadAPIMode(PROVIDER_OPENAI, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_ANTHROPIC, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_CLINEPASS, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_OPENROUTER, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
  })

  it('exposes provider-specific selectable api modes', () => {
    expect(monitorSelectableAPIModes(PROVIDER_OPENCODE_GO)).toEqual([
      API_MODE_CHAT_COMPLETIONS,
      API_MODE_RESPONSES,
      API_MODE_MESSAGES,
    ])
    expect(monitorSelectableAPIModes(PROVIDER_OPENAI)).toEqual([API_MODE_CHAT_COMPLETIONS, API_MODE_RESPONSES])
    expect(monitorSelectableAPIModes(PROVIDER_ANTHROPIC)).toEqual([API_MODE_CHAT_COMPLETIONS])
    expect(monitorSelectableAPIModes(PROVIDER_CLINEPASS)).toEqual([API_MODE_CHAT_COMPLETIONS])
    expect(monitorSelectableAPIModes(PROVIDER_OPENROUTER)).toEqual([API_MODE_CHAT_COMPLETIONS])
  })

  // OpenCode Go 有 Chat / Responses / Messages 探活适配器，不能被当成仅配额 provider。
  it('marks only adapter-less providers as quota-only', () => {
    expect(QUOTA_ONLY_PROVIDERS).toEqual([])
    expect(monitorProviderSupportsProbe(PROVIDER_OPENCODE_GO)).toBe(true)
    expect(monitorProviderSupportsProbe(PROVIDER_ANTIGRAVITY)).toBe(true)
    for (const provider of PROVIDERS) {
      expect(monitorProviderSupportsProbe(provider)).toBe(!QUOTA_ONLY_PROVIDERS.includes(provider))
    }
  })
})
