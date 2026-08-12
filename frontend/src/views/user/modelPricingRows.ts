import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserPricingInterval,
  UserSupportedModel,
  UserSupportedModelPricing,
} from '@/api/channels'
import type { BillingMode } from '@/constants/channel'

export interface ModelPricingValues {
  inputPrice: number | null
  outputPrice: number | null
  cacheWritePrice: number | null
  cacheReadPrice: number | null
  imageOutputPrice: number | null
  perRequestPrice: number | null
}

export interface ModelPricingIntervalRow {
  minTokens: number
  maxTokens: number | null
  tierLabel: string
  pricing: ModelPricingValues
  actualPricing: ModelPricingValues
}

export interface ModelPricingRow {
  channelName: string
  description: string
  platform: string
  modelName: string
  groupId: number
  groupName: string
  subscriptionType: string
  isExclusive: boolean
  defaultMultiplier: number
  userMultiplier: number | null
  groupMultiplier: number
  promotionCode: string
  promotionCostMultiplier: number
  promotionUsageMultiplier: number
  finalMultiplier: number
  pricingSource: string
  pricingSourceLabel: string
  pricingSourceDetail: string
  billingMode: BillingMode | null
  pricing: ModelPricingValues
  actualPricing: ModelPricingValues
  intervals: ModelPricingIntervalRow[]
}

const MISSING_SOURCE = 'missing'
const MISSING_SOURCE_LABEL = 'modelPricing.sources.missing'

function emptyPricingValues(): ModelPricingValues {
  return {
    inputPrice: null,
    outputPrice: null,
    cacheWritePrice: null,
    cacheReadPrice: null,
    imageOutputPrice: null,
    perRequestPrice: null,
  }
}

function normalizePrice(value: number | null | undefined): number | null {
  return value == null ? null : value
}

function multiplyPrice(value: number | null | undefined, multiplier: number): number | null {
  const normalized = normalizePrice(value)
  return normalized == null ? null : normalized * multiplier
}

function pricingValues(pricing: UserSupportedModelPricing): ModelPricingValues {
  return {
    inputPrice: normalizePrice(pricing.input_price),
    outputPrice: normalizePrice(pricing.output_price),
    cacheWritePrice: normalizePrice(pricing.cache_write_price),
    cacheReadPrice: normalizePrice(pricing.cache_read_price),
    imageOutputPrice: normalizePrice(pricing.image_output_price),
    perRequestPrice: normalizePrice(pricing.per_request_price),
  }
}

function actualPricingValues(pricing: UserSupportedModelPricing, multiplier: number): ModelPricingValues {
  return {
    inputPrice: multiplyPrice(pricing.input_price, multiplier),
    outputPrice: multiplyPrice(pricing.output_price, multiplier),
    cacheWritePrice: multiplyPrice(pricing.cache_write_price, multiplier),
    cacheReadPrice: multiplyPrice(pricing.cache_read_price, multiplier),
    imageOutputPrice: multiplyPrice(pricing.image_output_price, multiplier),
    perRequestPrice: multiplyPrice(pricing.per_request_price, multiplier),
  }
}

function intervalValues(interval: UserPricingInterval): ModelPricingValues {
  return {
    inputPrice: normalizePrice(interval.input_price),
    outputPrice: normalizePrice(interval.output_price),
    cacheWritePrice: normalizePrice(interval.cache_write_price),
    cacheReadPrice: normalizePrice(interval.cache_read_price),
    imageOutputPrice: null,
    perRequestPrice: normalizePrice(interval.per_request_price),
  }
}

function actualIntervalValues(interval: UserPricingInterval, multiplier: number): ModelPricingValues {
  return {
    inputPrice: multiplyPrice(interval.input_price, multiplier),
    outputPrice: multiplyPrice(interval.output_price, multiplier),
    cacheWritePrice: multiplyPrice(interval.cache_write_price, multiplier),
    cacheReadPrice: multiplyPrice(interval.cache_read_price, multiplier),
    imageOutputPrice: null,
    perRequestPrice: multiplyPrice(interval.per_request_price, multiplier),
  }
}

function intervalRows(pricing: UserSupportedModelPricing, multiplier: number): ModelPricingIntervalRow[] {
  return (pricing.intervals || []).map((interval) => ({
    minTokens: interval.min_tokens,
    maxTokens: interval.max_tokens,
    tierLabel: interval.tier_label || '',
    pricing: intervalValues(interval),
    actualPricing: actualIntervalValues(interval, multiplier),
  }))
}

function hasUserMultiplier(userGroupRates: Record<number, number>, groupId: number): boolean {
  return Object.prototype.hasOwnProperty.call(userGroupRates, groupId)
}

function clampMultiplier(value: number | null | undefined): number {
  if (value == null || !Number.isFinite(value)) {
    return 0
  }
  return Math.max(0, value)
}

function groupMultiplierForGroup(
  group: UserAvailableGroup,
  userGroupRates: Record<number, number>,
): Pick<ModelPricingRow, 'defaultMultiplier' | 'userMultiplier' | 'groupMultiplier'> {
  const defaultMultiplier = clampMultiplier(group.rate_multiplier)
  if (!hasUserMultiplier(userGroupRates, group.id)) {
    return {
      defaultMultiplier,
      userMultiplier: null,
      groupMultiplier: defaultMultiplier,
    }
  }

  const userMultiplier = clampMultiplier(userGroupRates[group.id])
  return {
    defaultMultiplier,
    userMultiplier,
    groupMultiplier: userMultiplier,
  }
}

function promotionFields(model: UserSupportedModel): Pick<
  ModelPricingRow,
  'promotionCode' | 'promotionCostMultiplier' | 'promotionUsageMultiplier'
> {
  const promotion = model.promotion
  if (
    !promotion ||
    !promotion.code ||
    !Number.isFinite(promotion.cost_multiplier) ||
    promotion.cost_multiplier <= 0 ||
    promotion.cost_multiplier >= 1 ||
    !Number.isFinite(promotion.usage_multiplier) ||
    promotion.usage_multiplier <= 1
  ) {
    return {
      promotionCode: '',
      promotionCostMultiplier: 1,
      promotionUsageMultiplier: 1,
    }
  }
  return {
    promotionCode: promotion.code,
    promotionCostMultiplier: promotion.cost_multiplier,
    promotionUsageMultiplier: promotion.usage_multiplier,
  }
}

function pricingSourceFields(pricing: UserSupportedModelPricing | null): Pick<
  ModelPricingRow,
  'pricingSource' | 'pricingSourceLabel' | 'pricingSourceDetail'
> {
  if (!pricing) {
    return {
      pricingSource: MISSING_SOURCE,
      pricingSourceLabel: MISSING_SOURCE_LABEL,
      pricingSourceDetail: '',
    }
  }

  const pricingSource = pricing.pricing_source || MISSING_SOURCE
  return {
    pricingSource,
    pricingSourceLabel: pricing.pricing_source_label || `modelPricing.sources.${pricingSource}`,
    pricingSourceDetail: pricing.pricing_source_detail || '',
  }
}

function rowForModelGroup(
  channel: UserAvailableChannel,
  platform: string,
  model: UserSupportedModel,
  group: UserAvailableGroup,
  userGroupRates: Record<number, number>,
): ModelPricingRow {
  const groupMultiplier = groupMultiplierForGroup(group, userGroupRates)
  const promotion = promotionFields(model)
  const finalMultiplier = groupMultiplier.groupMultiplier * promotion.promotionCostMultiplier
  const source = pricingSourceFields(model.pricing)

  if (!model.pricing) {
    const empty = emptyPricingValues()
    return {
      channelName: channel.name,
      description: channel.description || '',
      platform,
      modelName: model.name,
      groupId: group.id,
      groupName: group.name,
      subscriptionType: group.subscription_type || 'standard',
      isExclusive: group.is_exclusive,
      ...groupMultiplier,
      ...promotion,
      finalMultiplier,
      ...source,
      billingMode: null,
      pricing: empty,
      actualPricing: emptyPricingValues(),
      intervals: [],
    }
  }

  return {
    channelName: channel.name,
    description: channel.description || '',
    platform,
    modelName: model.name,
    groupId: group.id,
    groupName: group.name,
    subscriptionType: group.subscription_type || 'standard',
    isExclusive: group.is_exclusive,
    ...groupMultiplier,
    ...promotion,
    finalMultiplier,
    ...source,
    billingMode: model.pricing.billing_mode,
    pricing: pricingValues(model.pricing),
    actualPricing: actualPricingValues(model.pricing, finalMultiplier),
    intervals: intervalRows(model.pricing, finalMultiplier),
  }
}

export function buildModelPricingRows(
  channels: UserAvailableChannel[],
  userGroupRates: Record<number, number>,
): ModelPricingRow[] {
  return channels.flatMap((channel) =>
    channel.platforms.flatMap((section) =>
      section.supported_models.flatMap((model) =>
        section.groups.map((group) =>
          rowForModelGroup(channel, section.platform, model, group, userGroupRates),
        ),
      ),
    ),
  )
}

export function filterModelPricingRows(rows: ModelPricingRow[], query: string): ModelPricingRow[] {
  const q = query.trim().toLowerCase()
  if (!q) return rows

  return rows.filter((row) =>
    [
      row.channelName,
      row.description,
      row.platform,
      row.modelName,
      row.groupName,
      row.subscriptionType,
      row.pricingSource,
      row.pricingSourceLabel,
      row.pricingSourceDetail,
      row.billingMode || '',
    ].some((value) => value.toLowerCase().includes(q)),
  )
}
