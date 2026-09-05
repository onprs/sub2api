import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { buildModelMappingObject, getModelsByPlatform, getPresetMappingsByPlatform, splitModelMappingObject } from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('openai 模型列表包含 GPT-5.4 官方快照', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
    expect(models).toContain('codex-auto-review')
    expect(models).toContain('gpt-5.6')
    expect(models).toContain('gpt-6')
    expect(models).toContain('gpt-6-astra')
    expect(models).not.toContain('gpt-5.6-cyber')
    expect(models).not.toContain('gpt-5.5-pro')
    expect(models).not.toContain('gpt-5.4-nano')
    expect(models).not.toContain('gpt-5.4-pro')
    expect(new Set(models).size).toBe(models.length)
  })

  it('Command Code 模型列表与最新 GOAT 目录一致', () => {
    const models = getModelsByPlatform('commandcode')

    expect(models).toHaveLength(48)
    expect(models).toEqual(expect.arrayContaining([
      'google/gemini-3.8-flash',
      'meta/muse-spark-1.3',
      'meta/muse-spark-1.3-contributor',
      'deepseek/deepseek-v4-flash-fast',
      'Qwen/Qwen3.8-Max-0902',
      'Qwen/Qwen3.8-Flash',
      'tencent/hy4-preview',
      'meituan/LongCat-2.0:free'
    ]))
    expect(models).not.toContain('minimax/minimax-m3-free')
    expect(models).not.toContain('minimax/minimax-m2.7-free')
  })

  it('openai 预设映射包含 GPT-6 别名和 Astra', () => {
    expect(getPresetMappingsByPlatform('openai')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'GPT-6', from: 'gpt-6', to: 'gpt-6' }),
      expect.objectContaining({ label: 'GPT-6 Astra', from: 'gpt-6-astra', to: 'gpt-6-astra' })
    ]))
  })

  it('openai 模型列表不再暴露已下线的 ChatGPT 登录 Codex 模型', () => {
    const models = getModelsByPlatform('openai')

    expect(models).not.toContain('gpt-5')
    expect(models).not.toContain('gpt-5.1')
    expect(models).not.toContain('gpt-5.1-codex')
    expect(models).not.toContain('gpt-5.1-codex-max')
    expect(models).not.toContain('gpt-5.1-codex-mini')
    expect(models).not.toContain('gpt-5.2-codex')
  })

  it('antigravity 模型列表聚合思考程度并包含 Gemini 3.8', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toEqual([
      'gemini-3.8-flash',
      'gemini-3.7-flash',
      'gemini-3.6-flash',
      'gemini-3.5-flash',
      'gemini-3.1-pro',
      'claude-fable-5-1',
      'claude-sonnet-4-6',
      'claude-opus-4-6-thinking',
      'gpt-oss-120b-medium'
    ])
  })

  it('Claude 模型列表包含新发布的 Claude 模型', () => {
    expect(getModelsByPlatform('claude')).toContain('claude-fable-5-1')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5-1')
    expect(getModelsByPlatform('claude')).toContain('claude-fable-5')
    expect(getModelsByPlatform('claude')).toContain('claude-opus-4-8')
    expect(getModelsByPlatform('antigravity')).toContain('claude-sonnet-4-6')
    expect(getModelsByPlatform('antigravity')).toContain('claude-opus-4-6-thinking')
  })

  it('xAI 模型列表包含 Grok 4.5 官方模型和别名', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toContain('grok-4.6')
    expect(models).toContain('grok-4.6-latest')
    expect(models).toContain('grok-4.5')
    expect(models).toContain('grok-4.5-latest')
    expect(models).toContain('grok-build-latest')
    expect(models).toContain('grok-imagine-image-2.0')
    expect(models).toContain('grok-imagine-video-1.5')
  })

  it('combined 模式支持 Grok 4.5 官方别名映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['grok-4.5'],
      [
        { from: 'grok-latest', to: 'grok-4.5' },
        { from: 'grok-4.5-latest', to: 'grok-4.5' },
        { from: 'grok-build-latest', to: 'grok-4.5' }
      ]
    )

    expect(mapping).toEqual({
      'grok-4.5': 'grok-4.5',
      'grok-latest': 'grok-4.5',
      'grok-4.5-latest': 'grok-4.5',
      'grok-build-latest': 'grok-4.5'
    })
  })

  it('grok 模型列表包含 Composer 默认项和兼容别名', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toContain('grok-composer-2.5-fast')
    expect(models).not.toContain('grok-composer')
    expect(models).toContain('composer-2.5')
  })

  it('gemini 模型列表包含原生生图模型', () => {
    const models = getModelsByPlatform('gemini')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(models.indexOf('gemini-2.0-flash'))
    expect(models.indexOf('gemini-2.5-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash'))
  })

  it('Gemini API Key Free Tier 使用 AI Studio 的实际请求 ID', () => {
    const models = getModelsByPlatform('gemini', {
      accountType: 'apikey',
      tierId: 'aistudio_free'
    })

    expect(models).toEqual([
      'gemini-3-flash-preview',
      'gemini-2.5-flash',
      'gemini-2.5-flash-lite',
      'gemini-3.1-flash-lite',
      'gemini-3.5-flash',
      'gemini-3.5-flash-lite',
      'gemini-3.6-flash',
      'gemini-3.7-flash',
      'gemma-4-26b-a4b-it',
      'gemma-4-31b-it'
    ])
    expect(models).not.toContain('gemma-4-26b-it')
    expect(models).not.toContain('gemini-2.5-pro')
    expect(models).not.toContain('gemini-3.1-flash-image')
  })

  it('Gemini API Key Paid Tier 保留扩展目录', () => {
    const models = getModelsByPlatform('gemini', {
      accountType: 'apikey',
      tierId: 'aistudio_paid'
    })

    expect(models).toContain('gemini-2.5-pro')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).not.toContain('gemma-4-31b-it')
  })

  it('antigravity 模型列表隐藏 raw wire 和辅助模型', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).not.toContain('gemini-3-flash-agent')
    expect(models).not.toContain('gemini-pro-agent')
    expect(models).not.toContain('gemini-3.7-flash-tiered')
    expect(models).not.toContain('tab_flash_lite_preview')
  })

  it('antigravity 模型列表隐藏旧通用别名', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-3.1-pro')
    expect(models).not.toContain('gemini-3.5-flash-extra-low')
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('combined 模式会同时保留白名单身份映射和模型映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('splitModelMappingObject 会把身份映射还原成白名单，其余保留为映射', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }]
    })
  })
})
