import { describe, expect, it } from 'vitest'

import {
  buildTempUnschedPresets,
  TEMP_UNSCHED_PRESET_DEFINITIONS
} from '../tempUnschedPresets'

describe('tempUnschedPresets', () => {
  it('覆盖常见的可恢复上游错误码', () => {
    expect(TEMP_UNSCHED_PRESET_DEFINITIONS.map((preset) => preset.errorCode)).toEqual([
      408,
      429,
      500,
      502,
      503,
      504,
      524,
      529
    ])
  })

  it('为每项提供唯一标识、有效时长和机器可读关键词', () => {
    const ids = TEMP_UNSCHED_PRESET_DEFINITIONS.map((preset) => preset.id)
    expect(new Set(ids).size).toBe(ids.length)

    for (const preset of TEMP_UNSCHED_PRESET_DEFINITIONS) {
      expect(preset.durationMinutes).toBeGreaterThan(0)
      expect(preset.keywords.split(',').map((keyword) => keyword.trim()).filter(Boolean).length).toBeGreaterThan(0)
    }

    expect(TEMP_UNSCHED_PRESET_DEFINITIONS.find((preset) => preset.errorCode === 429)?.keywords)
      .toContain('rate_limit')
    expect(TEMP_UNSCHED_PRESET_DEFINITIONS.find((preset) => preset.errorCode === 503)?.keywords)
      .toContain('service_unavailable')
  })

  it('生成可直接写入表单的本地化规则', () => {
    const presets = buildTempUnschedPresets((key) => `translated:${key}`)
    const badGateway = presets.find((preset) => preset.id === 'bad-gateway')

    expect(badGateway).toEqual({
      id: 'bad-gateway',
      label: 'translated:admin.accounts.tempUnschedulable.presets.badGatewayLabel',
      rule: {
        error_code: 502,
        keywords: 'bad gateway, bad_gateway, upstream error, upstream_error, upstream connect error, upstream reset, connection reset, upstream service temporarily unavailable',
        duration_minutes: 10,
        description: 'translated:admin.accounts.tempUnschedulable.presets.badGatewayDesc'
      }
    })
  })
})
