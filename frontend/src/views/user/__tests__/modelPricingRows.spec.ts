import { describe, expect, it } from 'vitest'
import type { UserAvailableChannel } from '@/api/channels'
import { BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN } from '@/constants/channel'
import { buildModelPricingRows, filterModelPricingRows } from '../modelPricingRows'

function makeChannels(): UserAvailableChannel[] {
  return [
    {
      name: 'Gateway A',
      description: 'Primary paid channel',
      platforms: [
        {
          platform: 'openai',
          groups: [
            {
              id: 10,
              name: 'Standard',
              platform: 'openai',
              subscription_type: 'standard',
              rate_multiplier: 1,
              is_exclusive: false,
            },
            {
              id: 20,
              name: 'Enterprise',
              platform: 'openai',
              subscription_type: 'subscription',
              rate_multiplier: 2,
              is_exclusive: true,
            },
          ],
          supported_models: [
            {
              name: 'gpt-4o-mini',
              platform: 'openai',
              pricing: {
                billing_mode: BILLING_MODE_TOKEN,
                input_price: 0.000001,
                output_price: 0.000002,
                cache_write_price: 0.0000004,
                cache_read_price: 0,
                image_output_price: null,
                per_request_price: null,
                pricing_source: 'channel',
                pricing_source_label: 'modelPricing.sources.channel',
                pricing_source_detail: 'channel_model_pricing',
                intervals: [
                  {
                    min_tokens: 0,
                    max_tokens: 1000,
                    tier_label: 'first 1k',
                    input_price: 0.0000005,
                    output_price: 0.000001,
                    cache_write_price: null,
                    cache_read_price: 0,
                    per_request_price: null,
                  },
                ],
              },
            },
            {
              name: 'image-pro',
              platform: 'openai',
              pricing: {
                billing_mode: BILLING_MODE_PER_REQUEST,
                input_price: null,
                output_price: null,
                cache_write_price: null,
                cache_read_price: null,
                image_output_price: 0.07,
                per_request_price: 0.03,
                pricing_source: 'catalog',
                pricing_source_label: 'modelPricing.sources.catalog',
                pricing_source_detail: 'pricing_catalog',
                intervals: [],
              },
            },
          ],
        },
      ],
    },
    {
      name: 'Gateway B',
      description: '',
      platforms: [
        {
          platform: 'gemini',
          groups: [
            {
              id: 30,
              name: 'Free',
              platform: 'gemini',
              subscription_type: 'standard',
              rate_multiplier: 0,
              is_exclusive: false,
            },
          ],
          supported_models: [
            {
              name: 'gemini-free',
              platform: 'gemini',
              pricing: {
                billing_mode: BILLING_MODE_TOKEN,
                input_price: 0.000003,
                output_price: 0.000004,
                cache_write_price: null,
                cache_read_price: 0,
                image_output_price: null,
                per_request_price: null,
                pricing_source: 'catalog',
                pricing_source_label: 'modelPricing.sources.catalog',
                pricing_source_detail: 'pricing_catalog',
                intervals: [],
              },
            },
            {
              name: 'unpriced',
              platform: 'gemini',
              pricing: null,
            },
          ],
        },
      ],
    },
  ]
}

describe('buildModelPricingRows', () => {
  it('expands channels by platform, model, and group', () => {
    const rows = buildModelPricingRows(makeChannels(), {})

    expect(rows).toHaveLength(6)
    expect(rows[0]).toMatchObject({
      channelName: 'Gateway A',
      description: 'Primary paid channel',
      platform: 'openai',
      modelName: 'gpt-4o-mini',
      groupId: 10,
      groupName: 'Standard',
      subscriptionType: 'standard',
      isExclusive: false,
      effectiveMultiplier: 1,
      pricingSource: 'channel',
      pricingSourceLabel: 'modelPricing.sources.channel',
      pricingSourceDetail: 'channel_model_pricing',
      billingMode: BILLING_MODE_TOKEN,
    })
  })

  it('uses user-specific multipliers before group defaults', () => {
    const rows = buildModelPricingRows(makeChannels(), { 20: 0.5 })
    const enterprise = rows.find((row) => row.groupId === 20 && row.modelName === 'gpt-4o-mini')

    expect(enterprise?.defaultMultiplier).toBe(2)
    expect(enterprise?.userMultiplier).toBe(0.5)
    expect(enterprise?.effectiveMultiplier).toBe(0.5)
    expect(enterprise?.actualPricing.inputPrice).toBe(0.0000005)
    expect(enterprise?.actualPricing.outputPrice).toBe(0.000001)
  })

  it('keeps raw pricing and computes actual pricing with the effective multiplier', () => {
    const rows = buildModelPricingRows(makeChannels(), {})
    const enterprise = rows.find((row) => row.groupId === 20 && row.modelName === 'gpt-4o-mini')
    const perRequest = rows.find((row) => row.groupId === 20 && row.modelName === 'image-pro')

    expect(enterprise?.pricing.inputPrice).toBe(0.000001)
    expect(enterprise?.actualPricing.inputPrice).toBe(0.000002)
    expect(enterprise?.actualPricing.cacheWritePrice).toBe(0.0000008)
    expect(enterprise?.intervals[0]).toMatchObject({
      tierLabel: 'first 1k',
      pricing: {
        inputPrice: 0.0000005,
        outputPrice: 0.000001,
      },
      actualPricing: {
        inputPrice: 0.000001,
        outputPrice: 0.000002,
      },
    })
    expect(perRequest?.actualPricing.perRequestPrice).toBe(0.06)
    expect(perRequest?.actualPricing.imageOutputPrice).toBe(0.14)
  })

  it('preserves zero multipliers and computes actual prices as zero', () => {
    const rows = buildModelPricingRows(makeChannels(), {})
    const free = rows.find((row) => row.groupId === 30 && row.modelName === 'gemini-free')

    expect(free?.effectiveMultiplier).toBe(0)
    expect(free?.actualPricing.inputPrice).toBe(0)
    expect(free?.actualPricing.outputPrice).toBe(0)
    expect(free?.actualPricing.cacheReadPrice).toBe(0)
  })

  it('clamps negative multipliers to zero before computing actual prices', () => {
    const rows = buildModelPricingRows(makeChannels(), { 20: -3 })
    const enterprise = rows.find((row) => row.groupId === 20 && row.modelName === 'gpt-4o-mini')

    expect(enterprise?.defaultMultiplier).toBe(2)
    expect(enterprise?.userMultiplier).toBe(0)
    expect(enterprise?.effectiveMultiplier).toBe(0)
    expect(enterprise?.actualPricing.inputPrice).toBe(0)
    expect(enterprise?.actualPricing.outputPrice).toBe(0)
  })

  it('marks rows without pricing as missing source rows', () => {
    const rows = buildModelPricingRows(makeChannels(), {})
    const missing = rows.find((row) => row.modelName === 'unpriced')

    expect(missing).toMatchObject({
      pricingSource: 'missing',
      pricingSourceLabel: 'modelPricing.sources.missing',
      pricingSourceDetail: '',
      billingMode: null,
      pricing: {
        inputPrice: null,
        outputPrice: null,
        cacheWritePrice: null,
        cacheReadPrice: null,
        imageOutputPrice: null,
        perRequestPrice: null,
      },
      actualPricing: {
        inputPrice: null,
        outputPrice: null,
        cacheWritePrice: null,
        cacheReadPrice: null,
        imageOutputPrice: null,
        perRequestPrice: null,
      },
      intervals: [],
    })
  })
})

describe('filterModelPricingRows', () => {
  it('matches channel, platform, model, group, and pricing source text', () => {
    const rows = buildModelPricingRows(makeChannels(), {})

    expect(filterModelPricingRows(rows, 'primary paid')).toHaveLength(4)
    expect(filterModelPricingRows(rows, 'OPENAI')).toHaveLength(4)
    expect(filterModelPricingRows(rows, 'gpt-4o')).toHaveLength(2)
    expect(filterModelPricingRows(rows, 'enterprise')).toHaveLength(2)
    expect(filterModelPricingRows(rows, 'channel_model_pricing')).toHaveLength(2)
    expect(filterModelPricingRows(rows, 'pricing_catalog')).toHaveLength(3)
    expect(filterModelPricingRows(rows, 'missing')).toHaveLength(1)
  })
})
