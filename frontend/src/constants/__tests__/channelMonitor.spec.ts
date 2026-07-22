import { describe, expect, it } from 'vitest'

import {
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_MESSAGES,
  API_MODE_RESPONSES,
  monitorSelectableAPIModes,
  monitorCurrentDomainEndpoint,
  monitorPayloadAPIMode,
  PROVIDER_ANTHROPIC,
  PROVIDER_CLINEPASS,
  PROVIDER_OPENCODE_GO,
  PROVIDER_OPENAI,
} from '../channelMonitor'

describe('channel monitor constants', () => {
  it('uses /v1 when monitoring OpenCode Go through the current Sub2API domain', () => {
    expect(monitorCurrentDomainEndpoint(PROVIDER_OPENCODE_GO, 'https://api.onprs.top')).toBe('https://api.onprs.top/v1')
    expect(monitorCurrentDomainEndpoint(PROVIDER_ANTHROPIC, 'https://api.onprs.top')).toBe('https://api.onprs.top')
    expect(monitorCurrentDomainEndpoint(PROVIDER_CLINEPASS, 'https://api.onprs.top/')).toBe('https://api.onprs.top')
  })

  it('preserves OpenCode Go messages mode in monitor payloads', () => {
    expect(monitorPayloadAPIMode(PROVIDER_OPENCODE_GO, API_MODE_MESSAGES)).toBe(API_MODE_MESSAGES)
    expect(monitorPayloadAPIMode(PROVIDER_OPENCODE_GO, API_MODE_RESPONSES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_OPENAI, API_MODE_RESPONSES)).toBe(API_MODE_RESPONSES)
    expect(monitorPayloadAPIMode(PROVIDER_OPENAI, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_ANTHROPIC, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
    expect(monitorPayloadAPIMode(PROVIDER_CLINEPASS, API_MODE_MESSAGES)).toBe(API_MODE_CHAT_COMPLETIONS)
  })

  it('exposes provider-specific selectable api modes', () => {
    expect(monitorSelectableAPIModes(PROVIDER_OPENCODE_GO)).toEqual([API_MODE_CHAT_COMPLETIONS, API_MODE_MESSAGES])
    expect(monitorSelectableAPIModes(PROVIDER_OPENAI)).toEqual([API_MODE_CHAT_COMPLETIONS, API_MODE_RESPONSES])
    expect(monitorSelectableAPIModes(PROVIDER_ANTHROPIC)).toEqual([API_MODE_CHAT_COMPLETIONS])
    expect(monitorSelectableAPIModes(PROVIDER_CLINEPASS)).toEqual([API_MODE_CHAT_COMPLETIONS])
  })
})
