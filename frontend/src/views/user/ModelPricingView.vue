<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-80">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('modelPricing.searchPlaceholder')"
                class="input pl-10"
              />
            </div>

            <div
              class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-800"
            >
              <button
                v-for="option in pricingModeOptions"
                :key="option.value"
                type="button"
                class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
                :class="
                  pricingMode === option.value
                    ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                    : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                "
                @click="pricingMode = option.value"
              >
                {{ option.label }}
              </button>
            </div>
          </div>

          <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
            <button
              @click="loadPricing"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh', 'Refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <div class="table-wrapper model-pricing-table-wrapper">
          <table class="min-w-[1760px] border-collapse text-xs">
            <thead
              class="model-pricing-sticky-header sticky top-0 z-20 bg-gray-50/95 text-left font-medium uppercase text-gray-500 shadow-sm backdrop-blur dark:bg-dark-800/95 dark:text-gray-400"
            >
              <tr class="border-b border-gray-100 dark:border-dark-700">
                <th class="w-40 px-4 py-3">{{ t('modelPricing.columns.channel') }}</th>
                <th class="w-36 px-4 py-3">{{ t('modelPricing.columns.platform') }}</th>
                <th class="model-pricing-sticky-model w-56 px-4 py-3">{{ t('modelPricing.columns.model') }}</th>
                <th class="w-36 px-4 py-3">{{ t('modelPricing.columns.contextTier') }}</th>
                <th class="w-64 px-4 py-3">{{ t('modelPricing.columns.group') }}</th>
                <th class="w-28 px-4 py-3">{{ t('modelPricing.columns.groupMultiplier') }}</th>
                <th class="w-36 px-4 py-3">{{ t('modelPricing.columns.promotion') }}</th>
                <th class="w-28 px-4 py-3">{{ t('modelPricing.columns.finalMultiplier') }}</th>
                <th class="w-40 px-4 py-3">{{ t('modelPricing.columns.source') }}</th>
                <th class="w-32 px-4 py-3">{{ t('modelPricing.columns.billingMode') }}</th>
                <th class="w-28 px-4 py-3 text-right">{{ t('modelPricing.columns.inputPerMillion') }}</th>
                <th class="w-28 px-4 py-3 text-right">{{ t('modelPricing.columns.outputPerMillion') }}</th>
                <th class="w-32 px-4 py-3 text-right">{{ t('modelPricing.columns.cacheWritePerMillion') }}</th>
                <th class="w-32 px-4 py-3 text-right">{{ t('modelPricing.columns.cacheReadPerMillion') }}</th>
                <th class="w-36 px-4 py-3 text-right">{{ t('modelPricing.columns.unitPrice') }}</th>
              </tr>
            </thead>

            <tbody v-if="loading">
              <tr>
                <td colspan="15" class="py-10 text-center">
                  <Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" />
                </td>
              </tr>
            </tbody>

            <tbody v-else-if="filteredRows.length === 0">
              <tr>
                <td colspan="15" class="py-12 text-center">
                  <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
                  <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('modelPricing.empty') }}</p>
                </td>
              </tr>
            </tbody>

            <tbody v-else>
              <tr
                v-for="row in filteredRows"
                :key="rowKey(row)"
                class="border-b border-gray-100 transition-colors last:border-b-0 hover:bg-gray-50/60 dark:border-dark-800 dark:hover:bg-dark-800/50"
              >
                <td class="px-4 py-3 align-top">
                  <div class="font-medium text-gray-900 dark:text-white">{{ row.channelName }}</div>
                  <div v-if="row.description" class="mt-1 max-w-40 truncate text-[11px] text-gray-500 dark:text-gray-400">
                    {{ row.description }}
                  </div>
                </td>

                <td class="px-4 py-3 align-top">
                  <span
                    :class="[
                      'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
                      platformBadgeClass(row.platform),
                    ]"
                  >
                    <PlatformIcon :platform="row.platform as GroupPlatform" size="xs" />
                    {{ row.platform }}
                  </span>
                </td>

                <td class="model-pricing-sticky-model px-4 py-3 align-top font-mono text-[12px] text-gray-900 dark:text-gray-100">
                  <div class="group/model flex items-center justify-between gap-2">
                    <span class="truncate select-all" :title="row.modelName">{{ row.modelName }}</span>
                    <button
                      type="button"
                      class="opacity-0 group-hover/model:opacity-100 focus:opacity-100 rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200 transition-all flex-shrink-0"
                      :class="{ '!opacity-100 text-green-600 dark:text-green-400': copiedModel === row.modelName }"
                      :title="t('modelPricing.copyModelId', '复制模型 ID')"
                      @click.stop="handleCopyModel(row.modelName)"
                    >
                      <Icon :name="copiedModel === row.modelName ? 'check' : 'copy'" size="xs" />
                    </button>
                  </div>
                </td>

                <td class="px-4 py-3 align-top text-gray-600 dark:text-gray-300">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ line.label }}
                  </div>
                </td>

                <td class="px-4 py-3 align-top">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span
                      v-if="row.isExclusive"
                      class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase text-purple-600 dark:text-purple-400"
                    >
                      <Icon name="shield" size="xs" class="h-3 w-3" />
                      {{ t('availableChannels.exclusive') }}
                    </span>
                    <GroupBadge
                      :name="row.groupName"
                      :platform="row.platform as GroupPlatform"
                      :subscription-type="row.subscriptionType as SubscriptionType"
                      :rate-multiplier="row.defaultMultiplier"
                      :user-rate-multiplier="row.userMultiplier"
                      always-show-rate
                    />
                  </div>
                </td>

                <td class="px-4 py-3 align-top">
                  <div class="inline-flex items-center gap-1 font-mono text-[12px]">
                    <span
                      v-if="row.userMultiplier !== null && row.userMultiplier !== row.defaultMultiplier"
                      class="text-gray-400 line-through"
                    >
                      {{ formatMultiplier(row.defaultMultiplier) }}
                    </span>
                    <span class="font-semibold text-gray-900 dark:text-white">
                      {{ formatMultiplier(row.groupMultiplier) }}
                    </span>
                  </div>
                </td>

                <td class="px-4 py-3 align-top">
                  <span
                    v-if="row.promotionCode"
                    class="inline-flex flex-col gap-0.5 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] font-medium text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
                    :title="promotionTitle(row)"
                  >
                    <span>{{ t('modelPricing.promotions.usageBonus', { multiplier: formatMultiplier(row.promotionUsageMultiplier) }) }}</span>
                    <span class="font-mono">{{ t('modelPricing.promotions.priceMultiplier', { multiplier: formatMultiplier(row.promotionCostMultiplier) }) }}</span>
                  </span>
                  <span v-else class="text-gray-400 dark:text-gray-500">-</span>
                </td>

                <td class="px-4 py-3 align-top font-mono text-[12px] font-semibold text-gray-900 dark:text-white">
                  {{ formatMultiplier(row.finalMultiplier) }}
                </td>

                <td class="px-4 py-3 align-top">
                  <span
                    class="inline-flex max-w-36 items-center rounded-md bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300"
                    :title="row.pricingSourceDetail || row.pricingSource"
                  >
                    <span class="truncate">{{ sourceLabel(row) }}</span>
                  </span>
                </td>

                <td class="px-4 py-3 align-top text-gray-600 dark:text-gray-300">
                  {{ billingModeLabel(row.billingMode) }}
                </td>

                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(line.pricing, 'inputPrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(line.pricing, 'outputPrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(line.pricing, 'cacheWritePrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(line.pricing, 'cacheReadPrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ unitPrice(line.pricing) }}
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { BILLING_MODE_IMAGE, BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN, type BillingMode } from '@/constants/channel'
import { useAppStore } from '@/stores/app'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformBadgeClass } from '@/utils/platformColors'
import { formatScaled } from '@/utils/pricing'
import { useClipboard } from '@/composables/useClipboard'
import {
  buildModelPricingRows,
  filterModelPricingRows,
  type ModelPricingIntervalRow,
  type ModelPricingRow,
  type ModelPricingValues,
} from './modelPricingRows'

type PricingMode = 'raw' | 'actual'
type TokenPriceKey = 'inputPrice' | 'outputPrice' | 'cacheWritePrice' | 'cacheReadPrice'

interface ModelPricingLine {
  key: string
  label: string
  pricing: ModelPricingValues
}

const { t, te } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const pricingMode = ref<PricingMode>('actual')
const perMillionScale = 1_000_000

const { copyToClipboard } = useClipboard()
const copiedModel = ref<string | null>(null)
let copyTimeout: ReturnType<typeof setTimeout> | null = null

async function handleCopyModel(modelName: string) {
  const success = await copyToClipboard(modelName)
  if (success) {
    copiedModel.value = modelName
    if (copyTimeout) clearTimeout(copyTimeout)
    copyTimeout = setTimeout(() => {
      copiedModel.value = null
    }, 1500)
    appStore.showSuccess(t('modelPricing.modelCopied', '已复制模型 ID'))
  }
}

const pricingModeOptions = computed<Array<{ value: PricingMode; label: string }>>(() => [
  { value: 'raw', label: t('modelPricing.modes.raw') },
  { value: 'actual', label: t('modelPricing.modes.actual') },
])

const rows = computed(() => buildModelPricingRows(channels.value, userGroupRates.value))
const filteredRows = computed(() => filterModelPricingRows(rows.value, searchQuery.value))

function selectedPricing(row: ModelPricingRow): ModelPricingValues {
  return pricingMode.value === 'actual' ? row.actualPricing : row.pricing
}

function selectedIntervalPricing(interval: ModelPricingIntervalRow): ModelPricingValues {
  return pricingMode.value === 'actual' ? interval.actualPricing : interval.pricing
}

function formatContextTokenCount(tokens: number): string {
  if (tokens >= 1_000_000 && tokens % 1_000_000 === 0) {
    return `${tokens / 1_000_000}M`
  }
  if (tokens >= 1_000 && tokens % 1_000 === 0) {
    return `${tokens / 1_000}K`
  }
  return tokens.toLocaleString()
}

function contextTierLabel(interval: ModelPricingIntervalRow): string {
  if (interval.tierLabel) {
    return interval.tierLabel
  }
  if (interval.minTokens <= 0 && interval.maxTokens == null) {
    return t('modelPricing.contextTiers.all')
  }
  if (interval.minTokens <= 0 && interval.maxTokens != null) {
    return t('modelPricing.contextTiers.upTo', {
      tokens: formatContextTokenCount(interval.maxTokens),
    })
  }
  if (interval.maxTokens == null) {
    return t('modelPricing.contextTiers.above', {
      tokens: formatContextTokenCount(interval.minTokens),
    })
  }
  return t('modelPricing.contextTiers.range', {
    min: formatContextTokenCount(interval.minTokens),
    max: formatContextTokenCount(interval.maxTokens),
  })
}

function pricingLines(row: ModelPricingRow): ModelPricingLine[] {
  if (row.intervals.length === 0) {
    return [
      {
        key: 'all',
        label: t('modelPricing.contextTiers.all'),
        pricing: selectedPricing(row),
      },
    ]
  }
  return row.intervals.map((interval, index) => ({
    key: `${interval.minTokens}:${interval.maxTokens ?? 'max'}:${index}`,
    label: contextTierLabel(interval),
    pricing: selectedIntervalPricing(interval),
  }))
}

function tokenPrice(pricing: ModelPricingValues, key: TokenPriceKey): string {
  return formatScaled(pricing[key], perMillionScale)
}

function unitPrice(pricing: ModelPricingValues): string {
  const parts: string[] = []
  if (pricing.perRequestPrice != null) {
    parts.push(`${formatScaled(pricing.perRequestPrice, 1)} ${t('modelPricing.units.request')}`)
  }
  if (pricing.imageOutputPrice != null) {
    parts.push(`${formatScaled(pricing.imageOutputPrice, 1)} ${t('modelPricing.units.image')}`)
  }
  return parts.length > 0 ? parts.join(' / ') : '-'
}

function billingModeLabel(mode: BillingMode | null): string {
  switch (mode) {
    case BILLING_MODE_TOKEN:
      return t('modelPricing.billingModes.token')
    case BILLING_MODE_PER_REQUEST:
      return t('modelPricing.billingModes.perRequest')
    case BILLING_MODE_IMAGE:
      return t('modelPricing.billingModes.image')
    default:
      return '-'
  }
}

function sourceLabel(row: ModelPricingRow): string {
  if (row.pricingSourceLabel && te(row.pricingSourceLabel)) {
    return t(row.pricingSourceLabel)
  }
  if (row.pricingSource && te(`modelPricing.sources.${row.pricingSource}`)) {
    return t(`modelPricing.sources.${row.pricingSource}`)
  }
  return row.pricingSourceLabel || row.pricingSource || '-'
}

function formatMultiplier(value: number): string {
  return `${Number(value.toFixed(6))}x`
}

function promotionTitle(row: ModelPricingRow): string {
  return t('modelPricing.promotions.detail', {
    group: formatMultiplier(row.groupMultiplier),
    promotion: formatMultiplier(row.promotionCostMultiplier),
    final: formatMultiplier(row.finalMultiplier),
  })
}

function rowKey(row: ModelPricingRow): string {
  return `${row.channelName}:${row.platform}:${row.modelName}:${row.groupId}`
}

async function loadPricing() {
  loading.value = true
  try {
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable({ purpose: 'model_pricing' }),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(loadPricing)
</script>

<style scoped>
.model-pricing-table-wrapper {
  overflow-x: auto;
  overflow-y: auto;
}

.model-pricing-sticky-header th {
  position: sticky;
  top: 0;
  z-index: 20;
  background-color: rgb(249 250 251 / 0.95);
}

.dark .model-pricing-sticky-header th {
  background-color: rgb(31 41 55 / 0.95);
}

.model-pricing-tier-line {
  min-height: 1.5rem;
  line-height: 1.5rem;
}

.model-pricing-sticky-model {
  position: sticky;
  left: 0;
  z-index: 10;
  min-width: 14rem;
  background-color: rgb(255 255 255);
  box-shadow: 8px 0 12px -12px rgb(15 23 42 / 0.45);
}

.model-pricing-sticky-header .model-pricing-sticky-model {
  z-index: 30;
  background-color: rgb(249 250 251 / 0.98);
}

tbody tr:hover .model-pricing-sticky-model {
  background-color: rgb(249 250 251);
}

.dark .model-pricing-sticky-model {
  background-color: rgb(31 41 55);
  box-shadow: 8px 0 12px -12px rgb(0 0 0 / 0.8);
}

.dark .model-pricing-sticky-header .model-pricing-sticky-model {
  background-color: rgb(31 41 55 / 0.98);
}

.dark tbody tr:hover .model-pricing-sticky-model {
  background-color: rgb(31 41 55);
}
</style>
