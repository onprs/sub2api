<template>
  <AppLayout>
    <TablePageLayout class="model-pricing-layout" :show-table="hasSelectedGroup">
      <template #filters>
        <div class="space-y-4">
          <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-end">
            <div
              class="grid w-full min-w-0 gap-3 sm:grid-cols-2 lg:w-auto lg:grid-cols-[14rem_20rem]"
            >
              <div class="min-w-0">
                <label class="input-label">{{ t('modelPricing.selection.platform') }}</label>
                <Select
                  :model-value="selectedPlatform"
                  :options="platformOptions"
                  :placeholder="t('modelPricing.selection.platformPlaceholder')"
                  :searchable="false"
                  data-test="pricing-platform-select"
                  @update:model-value="setSelectedPlatform"
                >
                  <template #selected="{ option }">
                    <span v-if="option" class="flex min-w-0 items-center gap-2">
                      <PlatformIcon
                        :platform="(option as unknown as ModelPricingPlatformOption).value as GroupPlatform"
                        size="sm"
                      />
                      <span class="truncate">
                        {{ (option as unknown as ModelPricingPlatformOption).label }}
                      </span>
                    </span>
                    <span v-else class="text-gray-400 dark:text-gray-500">
                      {{ t('modelPricing.selection.platformPlaceholder') }}
                    </span>
                  </template>
                  <template #option="{ option, selected }">
                    <div class="flex min-w-0 flex-1 items-center gap-2">
                      <PlatformIcon
                        :platform="(option as unknown as ModelPricingPlatformOption).value as GroupPlatform"
                        size="sm"
                      />
                      <span class="min-w-0 flex-1 truncate">
                        {{ (option as unknown as ModelPricingPlatformOption).label }}
                      </span>
                      <Icon
                        v-if="selected"
                        name="check"
                        size="sm"
                        class="shrink-0 text-primary-500"
                      />
                    </div>
                  </template>
                </Select>
              </div>

              <div class="min-w-0">
                <label class="input-label">{{ t('modelPricing.selection.group') }}</label>
                <Select
                  :model-value="selectedGroupId"
                  :options="groupOptions"
                  :placeholder="t('modelPricing.selection.groupPlaceholder')"
                  :disabled="!selectedPlatform"
                  data-test="pricing-group-select"
                  @update:model-value="setSelectedGroup"
                >
                  <template #selected="{ option }">
                    <GroupBadge
                      v-if="option"
                      class="max-w-full"
                      :name="(option as unknown as ModelPricingGroupOption).label"
                      :platform="(option as unknown as ModelPricingGroupOption).platform"
                      :subscription-type="(option as unknown as ModelPricingGroupOption).subscriptionType"
                      :rate-multiplier="(option as unknown as ModelPricingGroupOption).defaultMultiplier"
                      :user-rate-multiplier="(option as unknown as ModelPricingGroupOption).userMultiplier"
                      :peak-rate-enabled="(option as unknown as ModelPricingGroupOption).peakRateEnabled"
                      :peak-start="(option as unknown as ModelPricingGroupOption).peakStart"
                      :peak-end="(option as unknown as ModelPricingGroupOption).peakEnd"
                      :peak-rate-multiplier="(option as unknown as ModelPricingGroupOption).peakRateMultiplier"
                      always-show-rate
                    />
                    <span v-else class="text-gray-400 dark:text-gray-500">
                      {{ t('modelPricing.selection.groupPlaceholder') }}
                    </span>
                  </template>
                  <template #option="{ option, selected }">
                    <div class="flex min-w-0 flex-1 items-center justify-between gap-2">
                      <div class="flex min-w-0 items-center gap-1.5">
                        <GroupBadge
                          class="min-w-0 max-w-full"
                          :name="(option as unknown as ModelPricingGroupOption).label"
                          :platform="(option as unknown as ModelPricingGroupOption).platform"
                          :subscription-type="(option as unknown as ModelPricingGroupOption).subscriptionType"
                          :rate-multiplier="(option as unknown as ModelPricingGroupOption).defaultMultiplier"
                          :user-rate-multiplier="(option as unknown as ModelPricingGroupOption).userMultiplier"
                          :peak-rate-enabled="(option as unknown as ModelPricingGroupOption).peakRateEnabled"
                          :peak-start="(option as unknown as ModelPricingGroupOption).peakStart"
                          :peak-end="(option as unknown as ModelPricingGroupOption).peakEnd"
                          :peak-rate-multiplier="(option as unknown as ModelPricingGroupOption).peakRateMultiplier"
                          always-show-rate
                        />
                        <Icon
                          v-if="(option as unknown as ModelPricingGroupOption).isExclusive"
                          name="shield"
                          size="xs"
                          class="shrink-0 text-purple-500"
                          :title="t('availableChannels.exclusive')"
                        />
                      </div>
                      <Icon
                        v-if="selected"
                        name="check"
                        size="sm"
                        class="shrink-0 text-primary-500"
                      />
                    </div>
                  </template>
                </Select>
              </div>
            </div>

            <div class="flex items-center gap-2 self-end">
              <div
                class="flex h-10 items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 text-gray-700 shadow-sm dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
                data-test="utc-clock"
                role="timer"
              >
                <Icon name="clock" size="sm" class="shrink-0 text-gray-400 dark:text-gray-500" />
                <span class="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">UTC</span>
                <time
                  :datetime="utcDateTime"
                  class="whitespace-nowrap font-mono text-xs tabular-nums"
                >
                  <span data-test="utc-date" class="mr-1 hidden sm:inline">{{ utcDate }}</span>
                  <span data-test="utc-time">{{ utcTime }}</span>
                </time>
              </div>

              <button
                type="button"
                class="btn btn-secondary h-10 w-10 p-0"
                :title="t('modelPricing.help.open')"
                :aria-label="t('modelPricing.help.open')"
                aria-haspopup="dialog"
                :aria-expanded="showPricingHelp"
                data-test="pricing-help-button"
                @click="showPricingHelp = true"
              >
                <Icon name="questionCircle" size="md" />
              </button>

              <button
                type="button"
                class="btn btn-secondary"
                :disabled="loading"
                :title="t('common.refresh', 'Refresh')"
                @click="loadPricing"
              >
                <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
              </button>
            </div>
          </div>

          <div
            v-if="hasSelectedGroup"
            class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
          >
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
                data-test="pricing-search"
              />
            </div>

            <div
              class="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-gray-200 bg-gray-200 dark:border-dark-600 dark:bg-dark-600"
            >
              <button
                v-for="option in pricingModeOptions"
                :key="option.value"
                type="button"
                class="min-h-10 min-w-0 bg-white px-3 py-2 text-sm font-medium transition-colors focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:bg-dark-800"
                :class="
                  pricingMode === option.value
                    ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/25 dark:text-primary-300'
                    : 'text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white'
                "
                :aria-pressed="pricingMode === option.value"
                :data-pricing-mode="option.value"
                @click="pricingMode = option.value"
              >
                {{ option.label }}
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <div class="table-wrapper model-pricing-table-wrapper">
          <table class="min-w-[1360px] border-collapse text-xs">
            <thead
              class="model-pricing-sticky-header sticky top-0 z-20 bg-gray-50/95 text-left font-medium uppercase text-gray-500 shadow-sm backdrop-blur dark:bg-dark-800/95 dark:text-gray-400"
            >
              <tr class="border-b border-gray-100 dark:border-dark-700">
                <th class="model-pricing-sticky-model w-56 px-4 py-3">{{ t('modelPricing.columns.model') }}</th>
                <th class="w-80 px-4 py-3">{{ t('modelPricing.columns.contextTier') }}</th>
                <th class="w-28 px-4 py-3">{{ t('modelPricing.columns.groupMultiplier') }}</th>
                <th class="w-32 px-4 py-3">{{ t('modelPricing.columns.modelSpecificMultiplier') }}</th>
                <th class="w-32 px-4 py-3">{{ t('modelPricing.columns.effectiveMultiplier') }}</th>
                <th class="w-36 px-4 py-3">{{ t('modelPricing.columns.usageOffer') }}</th>
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
                <td colspan="13" class="py-10 text-center">
                  <Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" />
                </td>
              </tr>
            </tbody>

            <tbody v-else-if="filteredRows.length === 0">
              <tr>
                <td colspan="13" class="py-12 text-center">
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
                    v-if="row.modelSpecificMultiplier !== null"
                    class="font-mono text-[12px] font-semibold text-gray-900 dark:text-white"
                  >
                    {{ formatMultiplier(row.modelSpecificMultiplier) }}
                  </span>
                  <span v-else class="text-gray-400 dark:text-gray-500">-</span>
                </td>

                <td class="px-4 py-3 align-top">
                  <span class="font-mono text-[12px] font-semibold text-gray-900 dark:text-white">
                    {{ formatMultiplier(row.effectiveMultiplier) }}
                  </span>
                </td>

                <td class="px-4 py-3 align-top">
                  <span
                    v-if="row.usageOfferCode"
                    class="inline-flex rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] font-medium text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
                    :title="t('modelPricing.usageOffers.detail')"
                  >
                    {{ t('modelPricing.usageOffers.multiplier', { multiplier: formatMultiplier(row.usageMultiplier) }) }}
                  </span>
                  <span v-else class="text-gray-400 dark:text-gray-500">-</span>
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
                    {{ tokenPrice(row, line.pricing, 'inputPrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(row, line.pricing, 'outputPrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(row, line.pricing, 'cacheWritePrice') }}
                  </div>
                </td>
                <td class="px-4 py-3 text-right align-top font-mono text-[12px]">
                  <div
                    v-for="line in pricingLines(row)"
                    :key="line.key"
                    class="model-pricing-tier-line whitespace-nowrap"
                  >
                    {{ tokenPrice(row, line.pricing, 'cacheReadPrice') }}
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

    <ModelPricingHelpDialog :show="showPricingHelp" @close="showPricingHelp = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import ModelPricingHelpDialog from '@/components/user/ModelPricingHelpDialog.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { BILLING_MODE_IMAGE, BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN, type BillingMode } from '@/constants/channel'
import { useAppStore } from '@/stores/app'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformLabel } from '@/utils/platformColors'
import { formatScaled } from '@/utils/pricing'
import { ACTUAL_COST_SYMBOL } from '@/utils/currencyDisplay'
import { useClipboard } from '@/composables/useClipboard'
import {
  buildModelPricingRows,
  calculateActualTokenPrice,
  filterModelPricingRows,
  type ModelPricingIntervalRow,
  type ModelPricingRow,
  type ModelPricingTimeBandRow,
  type ModelPricingValues,
} from './modelPricingRows'

type TokenPriceKey = 'inputPrice' | 'outputPrice' | 'cacheWritePrice' | 'cacheReadPrice'
type PricingMode = 'raw' | 'actual'

interface ModelPricingLine {
  key: string
  label: string
  pricing: ModelPricingValues
}

interface ModelPricingPlatformOption extends Record<string, unknown> {
  value: string
  label: string
}

interface ModelPricingGroupOption extends Record<string, unknown> {
  value: number
  label: string
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  defaultMultiplier: number
  userMultiplier: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  isExclusive: boolean
}

const platformOrder: GroupPlatform[] = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'opencode_go',
  'clinepass',
  'openrouter',
]
const platformOrderIndex = new Map<string, number>(
  platformOrder.map((platform, index) => [platform, index]),
)

const { t, te } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const selectedPlatform = ref<string | null>(null)
const selectedGroupId = ref<number | null>(null)
const searchQuery = ref('')
const pricingMode = ref<PricingMode>('actual')
const showPricingHelp = ref(false)
const perMillionScale = 1_000_000

const { copyToClipboard } = useClipboard()
const copiedModel = ref<string | null>(null)
const utcDate = ref('')
const utcTime = ref('')
const utcDateTime = ref('')
let copyTimeout: ReturnType<typeof setTimeout> | null = null
let utcClockTimer: ReturnType<typeof setInterval> | null = null

function updateUtcClock() {
  const now = new Date()
  const parts = [
    now.getUTCFullYear(),
    String(now.getUTCMonth() + 1).padStart(2, '0'),
    String(now.getUTCDate()).padStart(2, '0'),
    String(now.getUTCHours()).padStart(2, '0'),
    String(now.getUTCMinutes()).padStart(2, '0'),
    String(now.getUTCSeconds()).padStart(2, '0'),
  ]
  utcDate.value = `${parts[0]}-${parts[1]}-${parts[2]}`
  utcTime.value = `${parts[3]}:${parts[4]}:${parts[5]}`
  utcDateTime.value = now.toISOString()
}

async function handleCopyModel(modelName: string) {
  const success = await copyToClipboard(modelName, t('modelPricing.modelCopied'))
  if (success) {
    copiedModel.value = modelName
    if (copyTimeout) clearTimeout(copyTimeout)
    copyTimeout = setTimeout(() => {
      copiedModel.value = null
    }, 1500)
  }
}

const pricingModeOptions = computed<Array<{ value: PricingMode; label: string }>>(() => [
  { value: 'raw', label: t('modelPricing.modes.raw') },
  { value: 'actual', label: t('modelPricing.modes.actual') },
])

const rows = computed(() => buildModelPricingRows(channels.value, userGroupRates.value))

const platformOptions = computed<ModelPricingPlatformOption[]>(() => {
  const platforms = new Set(rows.value.map((row) => row.platform))
  return [...platforms]
    .sort((left, right) => {
      const leftIndex = platformOrderIndex.get(left) ?? Number.MAX_SAFE_INTEGER
      const rightIndex = platformOrderIndex.get(right) ?? Number.MAX_SAFE_INTEGER
      return leftIndex - rightIndex || platformLabel(left).localeCompare(platformLabel(right))
    })
    .map((platform) => ({ value: platform, label: platformLabel(platform) }))
})

const groupOptions = computed<ModelPricingGroupOption[]>(() => {
  if (!selectedPlatform.value) return []

  const groups = new Map<number, ModelPricingGroupOption>()
  for (const row of rows.value) {
    if (row.platform !== selectedPlatform.value || groups.has(row.groupId)) continue
    groups.set(row.groupId, {
      value: row.groupId,
      label: row.groupName,
      platform: row.platform as GroupPlatform,
      subscriptionType: row.subscriptionType as SubscriptionType,
      defaultMultiplier: row.defaultMultiplier,
      userMultiplier: row.userMultiplier,
      peakRateEnabled: row.peakRateEnabled,
      peakStart: row.peakStart,
      peakEnd: row.peakEnd,
      peakRateMultiplier: row.peakRateMultiplier,
      isExclusive: row.isExclusive,
    })
  }
  return [...groups.values()].sort((left, right) => left.label.localeCompare(right.label))
})

const hasSelectedGroup = computed(
  () =>
    selectedGroupId.value !== null &&
    groupOptions.value.some((option) => option.value === selectedGroupId.value),
)

const selectedRows = computed(() => {
  if (!hasSelectedGroup.value) return []
  return rows.value.filter(
    (row) =>
      row.platform === selectedPlatform.value && row.groupId === selectedGroupId.value,
  )
})
const filteredRows = computed(() => filterModelPricingRows(selectedRows.value, searchQuery.value))

function setSelectedPlatform(value: string | number | boolean | null) {
  const nextPlatform =
    typeof value === 'string' && platformOptions.value.some((option) => option.value === value)
      ? value
      : null
  if (nextPlatform === selectedPlatform.value) return

  selectedPlatform.value = nextPlatform
  selectedGroupId.value = null
  searchQuery.value = ''
}

function setSelectedGroup(value: string | number | boolean | null) {
  const nextGroupId =
    typeof value === 'number' && groupOptions.value.some((option) => option.value === value)
      ? value
      : null
  if (nextGroupId === selectedGroupId.value) return

  selectedGroupId.value = nextGroupId
  searchQuery.value = ''
}

function reconcileSelection() {
  if (
    selectedPlatform.value &&
    !platformOptions.value.some((option) => option.value === selectedPlatform.value)
  ) {
    selectedPlatform.value = null
    selectedGroupId.value = null
    searchQuery.value = ''
    return
  }

  if (!selectedPlatform.value) {
    selectedGroupId.value = null
    return
  }

  if (
    selectedGroupId.value !== null &&
    !groupOptions.value.some((option) => option.value === selectedGroupId.value)
  ) {
    selectedGroupId.value = null
    searchQuery.value = ''
  }
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

function timeBandLabel(timeBand: ModelPricingTimeBandRow): string {
  const key = `modelPricing.timeBands.${timeBand.code}`
  const name = te(key) ? t(key) : timeBand.code
  return `${name} · ${timeBand.timeZone} ${timeBand.timeRanges.join(', ')}`
}

function pricingLines(row: ModelPricingRow): ModelPricingLine[] {
  if (row.timeBands.length > 0) {
    return row.timeBands.map((timeBand, index) => ({
      key: `${timeBand.code}:${index}`,
      label: timeBandLabel(timeBand),
      pricing: timeBand.pricing,
    }))
  }
  if (row.intervals.length === 0) {
    return [
      {
        key: 'all',
        label: t('modelPricing.contextTiers.all'),
        pricing: row.pricing,
      },
    ]
  }
  return row.intervals.map((interval, index) => ({
    key: `${interval.minTokens}:${interval.maxTokens ?? 'max'}:${index}`,
    label: contextTierLabel(interval),
    pricing: interval.pricing,
  }))
}

const pricingCurrencySymbol = computed(() =>
  pricingMode.value === 'actual' ? ACTUAL_COST_SYMBOL : '$',
)

function tokenPrice(
  row: ModelPricingRow,
  pricing: ModelPricingValues,
  key: TokenPriceKey,
): string {
  const price =
    pricingMode.value === 'actual'
      ? calculateActualTokenPrice(pricing[key], row.effectiveMultiplier)
      : pricing[key]
  return formatScaled(price, perMillionScale, pricingCurrencySymbol.value)
}

function unitPrice(pricing: ModelPricingValues): string {
  const parts: string[] = []
  if (pricing.perRequestPrice != null) {
    parts.push(`${formatScaled(pricing.perRequestPrice, 1, pricingCurrencySymbol.value)} ${t('modelPricing.units.request')}`)
  }
  if (pricing.imageOutputPrice != null) {
    parts.push(`${formatScaled(pricing.imageOutputPrice, 1, pricingCurrencySymbol.value)} ${t('modelPricing.units.image')}`)
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
    reconcileSelection()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  updateUtcClock()
  utcClockTimer = setInterval(updateUtcClock, 1000)
  void loadPricing()
})

onUnmounted(() => {
  if (utcClockTimer) clearInterval(utcClockTimer)
  if (copyTimeout) clearTimeout(copyTimeout)
})
</script>

<style scoped>
.model-pricing-table-wrapper {
  overflow-x: auto;
  overflow-y: auto;
}

@media (max-width: 1023px) {
  .model-pricing-layout.table-page-layout.mobile-mode
    :deep(.table-scroll-container .model-pricing-table-wrapper) {
    max-width: 100%;
    overflow-x: auto;
    overflow-y: auto;
  }
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
