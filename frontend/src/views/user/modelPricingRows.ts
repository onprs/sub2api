import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserPricingInterval,
  UserPricingTimeBand,
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
}

export interface ModelPricingTimeBandRow {
  code: string
  timeZone: string
  timeRanges: string[]
  pricing: ModelPricingValues
}

export interface ModelPricingRow {
  channelName: string
  description: string
  platform: string
  modelName: string
  contextLength: number | null
  promotionLabel: string
  promotionTerm: string
  promotionExpiresAt: string
  promotionFree: boolean
  groupId: number
  groupName: string
  subscriptionType: string
  isExclusive: boolean
  defaultMultiplier: number
  userMultiplier: number | null
  groupMultiplier: number
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  currentPeakMultiplier: number
  modelSpecificMultiplier: number | null
  effectiveMultiplier: number
  usageOfferCode: string
  usageOfferLabel: string
  usageMultiplier: number
  pricingSource: string
  pricingSourceLabel: string
  pricingSourceDetail: string
  billingMode: BillingMode | null
  pricing: ModelPricingValues
  intervals: ModelPricingIntervalRow[]
  timeBands: ModelPricingTimeBandRow[]
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

export function calculateActualTokenPrice(
  value: number | null | undefined,
  effectiveMultiplier: number,
): number | null {
  const price = normalizePrice(value)
  return price == null ? null : price * clampMultiplier(effectiveMultiplier)
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

function intervalRows(pricing: UserSupportedModelPricing): ModelPricingIntervalRow[] {
  return (pricing.intervals || []).map((interval) => ({
    minTokens: interval.min_tokens,
    maxTokens: interval.max_tokens,
    tierLabel: interval.tier_label || '',
    pricing: intervalValues(interval),
  }))
}

function timeBandValues(timeBand: UserPricingTimeBand): ModelPricingValues {
  return {
    inputPrice: normalizePrice(timeBand.input_price),
    outputPrice: normalizePrice(timeBand.output_price),
    cacheWritePrice: normalizePrice(timeBand.cache_write_price),
    cacheReadPrice: normalizePrice(timeBand.cache_read_price),
    imageOutputPrice: null,
    perRequestPrice: null,
  }
}

function timeBandRows(pricing: UserSupportedModelPricing): ModelPricingTimeBandRow[] {
  return (pricing.time_bands || []).map((timeBand) => ({
    code: timeBand.code,
    timeZone: timeBand.time_zone,
    timeRanges: [...timeBand.time_ranges],
    pricing: timeBandValues(timeBand),
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

function peakRateFields(group: UserAvailableGroup): Pick<
  ModelPricingRow,
  | 'peakRateEnabled'
  | 'peakStart'
  | 'peakEnd'
  | 'peakRateMultiplier'
  | 'currentPeakMultiplier'
> {
  const configuredMultiplier = group.peak_rate_multiplier
  const currentMultiplier = group.current_peak_multiplier
  return {
    peakRateEnabled: Boolean(group.peak_rate_enabled),
    peakStart: group.peak_start || '',
    peakEnd: group.peak_end || '',
    peakRateMultiplier:
      configuredMultiplier != null && Number.isFinite(configuredMultiplier) && configuredMultiplier >= 0
        ? configuredMultiplier
        : 1,
    currentPeakMultiplier:
      currentMultiplier != null && Number.isFinite(currentMultiplier) && currentMultiplier >= 0
        ? currentMultiplier
        : 1,
  }
}

function commandCodeMetadataFields(model: UserSupportedModel): Pick<
  ModelPricingRow,
  'contextLength' | 'promotionLabel' | 'promotionTerm' | 'promotionExpiresAt' | 'promotionFree'
> {
  const contextLength = model.context_length
  const promotion = model.promotion
  return {
    contextLength:
      contextLength != null && Number.isFinite(contextLength) && contextLength > 0
        ? Math.floor(contextLength)
        : null,
    promotionLabel: promotion?.label || '',
    promotionTerm: promotion?.term || '',
    promotionExpiresAt: promotion?.expires_at || '',
    promotionFree: Boolean(promotion?.free),
  }
}

function modelSpecificMultiplierForModel(model: UserSupportedModel): number | null {
  const multiplier = model.model_specific_multiplier
  if (multiplier == null || !Number.isFinite(multiplier) || multiplier <= 0) {
    return null
  }
  return multiplier
}

function usageOfferFields(model: UserSupportedModel): Pick<
  ModelPricingRow,
  'usageOfferCode' | 'usageOfferLabel' | 'usageMultiplier'
> {
  const offer = model.usage_offer
  if (
    !offer ||
    !offer.code ||
    (!offer.label && (!Number.isFinite(offer.usage_multiplier) || offer.usage_multiplier <= 1))
  ) {
    return {
      usageOfferCode: '',
      usageOfferLabel: '',
      usageMultiplier: 1,
    }
  }
  return {
    usageOfferCode: offer.code,
    usageOfferLabel: offer.label || '',
    usageMultiplier: Number.isFinite(offer.usage_multiplier) ? offer.usage_multiplier : 1,
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
  const peakRate = peakRateFields(group)
  const modelSpecificMultiplier = modelSpecificMultiplierForModel(model)
  const effectiveMultiplier =
    groupMultiplier.groupMultiplier *
    peakRate.currentPeakMultiplier *
    (modelSpecificMultiplier ?? 1)
  const usageOffer = usageOfferFields(model)
  const metadata = commandCodeMetadataFields(model)
  const source = pricingSourceFields(model.pricing)

  if (!model.pricing) {
    const empty = emptyPricingValues()
    return {
      channelName: channel.name,
      description: channel.description || '',
      platform,
      modelName: model.name,
      ...metadata,
      groupId: group.id,
      groupName: group.name,
      subscriptionType: group.subscription_type || 'standard',
      isExclusive: group.is_exclusive,
      ...groupMultiplier,
      ...peakRate,
      modelSpecificMultiplier,
      effectiveMultiplier,
      ...usageOffer,
      ...source,
      billingMode: null,
      pricing: empty,
      intervals: [],
      timeBands: [],
    }
  }

  return {
    channelName: channel.name,
    description: channel.description || '',
    platform,
    modelName: model.name,
    ...metadata,
    groupId: group.id,
    groupName: group.name,
    subscriptionType: group.subscription_type || 'standard',
    isExclusive: group.is_exclusive,
    ...groupMultiplier,
    ...peakRate,
    modelSpecificMultiplier,
    effectiveMultiplier,
    ...usageOffer,
    ...source,
    billingMode: model.pricing.billing_mode,
    pricing: pricingValues(model.pricing),
    intervals: intervalRows(model.pricing),
    timeBands: timeBandRows(model.pricing),
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
      row.promotionLabel,
      row.promotionTerm,
      row.groupName,
      row.subscriptionType,
      row.usageOfferCode,
      row.pricingSource,
      row.pricingSourceLabel,
      row.pricingSourceDetail,
      row.billingMode || '',
    ].some((value) => value.toLowerCase().includes(q)),
  )
}
