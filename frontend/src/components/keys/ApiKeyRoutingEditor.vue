<template>
  <div class="space-y-4">
    <div class="grid gap-4 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.6fr)]">
      <div>
        <label class="input-label">{{ t('keys.routing.platform') }}</label>
        <Select
          :model-value="modelValue.platform"
          :options="platformOptions"
          :placeholder="t('keys.routing.selectPlatform')"
          @update:model-value="setPlatform"
        />
      </div>

      <div>
        <label class="input-label">{{ t('keys.routing.strategy') }}</label>
        <div
          class="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-gray-200 bg-gray-200 dark:border-dark-600 dark:bg-dark-600 sm:grid-cols-4"
        >
          <button
            v-for="option in strategyOptions"
            :key="option.value"
            type="button"
            :class="[
              'min-h-10 min-w-0 px-2 py-2 text-sm font-medium transition-colors focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40',
              modelValue.strategy === option.value
                ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/25 dark:text-primary-300'
                : 'bg-white text-gray-600 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700'
            ]"
            :aria-pressed="modelValue.strategy === option.value"
            @click="setStrategy(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
      </div>
    </div>

    <div>
      <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
        <label class="input-label mb-0">{{ t('keys.routing.candidateGroups') }}</label>
        <div class="flex items-center gap-2">
          <span
            class="text-xs tabular-nums text-gray-500 dark:text-gray-400"
            :title="healthWindowLabel"
          >
            {{ healthWindowLabel }}
          </span>
          <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
            {{ t('keys.routing.selectedCount', { count: selectedGroupIDs.length, max: maxCandidates }) }}
          </span>
          <button
            type="button"
            class="flex h-8 w-8 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 disabled:cursor-wait disabled:opacity-60 dark:hover:bg-dark-700 dark:hover:text-gray-200"
            :disabled="healthLoading || !modelValue.platform"
            :title="t('keys.routing.refreshHealth')"
            :aria-label="t('keys.routing.refreshHealth')"
            @click="emit('refresh-health')"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': healthLoading }" />
          </button>
        </div>
      </div>

      <div
        v-if="modelValue.platform"
        class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
      >
        <div
          class="flex items-center gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2.5 dark:border-dark-600 dark:bg-dark-800 sm:px-4"
        >
          <Icon name="search" size="sm" class="shrink-0 text-gray-400" />
          <input
            v-model="searchText"
            type="search"
            class="min-w-0 flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-white dark:placeholder:text-dark-400"
            :placeholder="t('keys.searchGroup')"
          />
        </div>

        <div
          class="hidden items-center border-b border-gray-200 bg-gray-50/70 px-4 py-2 text-[11px] font-medium text-gray-500 dark:border-dark-600 dark:bg-dark-800/70 dark:text-gray-400 md:flex"
        >
          <span class="min-w-0 flex-1">{{ t('keys.routing.groupColumn') }}</span>
          <div class="grid w-[21rem] shrink-0 grid-cols-3 text-center">
            <span :title="t('keys.routing.statusHint')">{{ t('keys.routing.recentStatus') }}</span>
            <span :title="t('keys.routing.latencyHint')">{{ t('keys.routing.latency') }}</span>
            <span :title="t('keys.routing.stabilityHint')">{{ t('keys.routing.stability') }}</span>
          </div>
          <span class="w-16 shrink-0" />
        </div>

        <div class="max-h-[min(30rem,58vh)] overflow-y-auto">
          <div
            v-for="group in visibleGroups"
            :key="group.id"
            :class="[
              'flex min-h-[4.75rem] flex-wrap items-center gap-x-3 gap-y-2 border-b border-gray-100 px-3 py-2.5 last:border-b-0 dark:border-dark-700 sm:px-4',
              isSelected(group.id)
                ? 'bg-primary-50/55 dark:bg-primary-900/10'
                : 'bg-white dark:bg-dark-800'
            ]"
          >
            <label
              :class="[
                'flex min-w-0 flex-1 basis-[15rem] items-center gap-3',
                canToggle(group.id) ? 'cursor-pointer' : 'cursor-not-allowed opacity-50'
              ]"
            >
              <input
                type="checkbox"
                class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                :checked="isSelected(group.id)"
                :disabled="!canToggle(group.id)"
                @change="toggleGroup(group.id)"
              />
              <span
                v-if="isSelected(group.id)"
                class="inline-flex h-6 min-w-6 shrink-0 items-center justify-center rounded-md bg-gray-100 px-1 text-[11px] font-semibold tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300"
              >
                {{ selectedIndex(group.id) + 1 }}
              </span>
              <div class="min-w-0">
                <div class="flex min-w-0 flex-wrap items-center gap-1.5">
                  <GroupBadge
                    :name="group.name"
                    :platform="group.platform"
                    :subscription-type="group.subscription_type"
                    :rate-multiplier="group.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[group.id] ?? null"
                    :peak-rate-enabled="group.peak_rate_enabled"
                    :peak-start="group.peak_start"
                    :peak-end="group.peak_end"
                    :peak-rate-multiplier="group.peak_rate_multiplier"
                    class="min-w-0 max-w-full"
                  />
                  <span
                    v-if="group.status !== 'active'"
                    class="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium text-gray-500 dark:bg-dark-700 dark:text-gray-300"
                  >
                    {{ t('common.inactive') }}
                  </span>
                </div>
                <p
                  v-if="group.description"
                  class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400"
                  :title="group.description"
                >
                  {{ group.description }}
                </p>
              </div>
            </label>

            <div
              class="order-3 grid w-full grid-cols-3 border-t border-gray-100 pt-2 text-center dark:border-dark-700 md:order-none md:w-[21rem] md:shrink-0 md:border-t-0 md:pt-0"
            >
              <div class="min-w-0 px-1" :title="healthTitle(group.id)">
                <span class="mb-0.5 block text-[10px] text-gray-400 md:hidden">
                  {{ t('keys.routing.recentStatus') }}
                </span>
                <span class="inline-flex max-w-full items-center justify-center gap-1.5 text-xs font-medium" :class="healthTextClass(group.id)">
                  <span class="h-2 w-2 shrink-0 rounded-full" :class="healthDotClass(group.id)" />
                  <span class="truncate">{{ healthStatusLabel(group.id) }}</span>
                </span>
              </div>
              <div class="min-w-0 border-l border-gray-100 px-1 dark:border-dark-700" :title="t('keys.routing.latencyHint')">
                <span class="mb-0.5 block text-[10px] text-gray-400 md:hidden">
                  {{ t('keys.routing.latency') }}
                </span>
                <span class="block truncate text-xs font-semibold tabular-nums text-gray-700 dark:text-gray-200">
                  {{ latencyLabel(group.id) }}
                </span>
              </div>
              <div class="min-w-0 border-l border-gray-100 px-1 dark:border-dark-700" :title="healthTitle(group.id)">
                <span class="mb-0.5 block text-[10px] text-gray-400 md:hidden">
                  {{ t('keys.routing.stability') }}
                </span>
                <span class="block truncate text-xs font-semibold tabular-nums text-gray-700 dark:text-gray-200">
                  {{ stabilityLabel(group.id) }}
                </span>
                <span
                  v-if="groupHealth(group.id)?.sample_count"
                  class="block truncate text-[10px] tabular-nums text-gray-400"
                >
                  {{ t('keys.routing.sampleCount', { count: groupHealth(group.id)!.sample_count }) }}
                </span>
              </div>
            </div>

            <div v-if="isSelected(group.id)" class="flex h-8 w-16 shrink-0 items-center justify-end gap-1">
              <button
                type="button"
                class="flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                :disabled="selectedIndex(group.id) === 0"
                :title="t('keys.routing.moveUp')"
                @click="moveGroup(group.id, -1)"
              >
                <Icon name="chevronUp" size="sm" />
              </button>
              <button
                type="button"
                class="flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                :disabled="selectedIndex(group.id) === selectedGroupIDs.length - 1"
                :title="t('keys.routing.moveDown')"
                @click="moveGroup(group.id, 1)"
              >
                <Icon name="chevronDown" size="sm" />
              </button>
            </div>
            <div v-else class="h-8 w-16 shrink-0" />
          </div>

          <div
            v-if="visibleGroups.length === 0"
            class="px-4 py-10 text-center text-sm text-gray-500 dark:text-gray-400"
          >
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
      <div
        v-else
        class="rounded-lg border border-dashed border-gray-300 px-4 py-10 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
      >
        {{ t('keys.routing.selectPlatform') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { platformLabel } from '@/utils/platformColors'
import type {
  ApiKeyRoutingDraft,
  ApiKeyRoutingGroupHealth,
  ApiKeyRoutingHealthStatus,
  ApiKeyRoutingStrategy,
  Group,
  GroupPlatform
} from '@/types'

const props = withDefaults(defineProps<{
  modelValue: ApiKeyRoutingDraft
  groups: Group[]
  userGroupRates?: Record<number, number>
  health?: Record<number, ApiKeyRoutingGroupHealth>
  healthLoading?: boolean
  healthWindowMinutes?: number
  maxCandidates?: number
}>(), {
  userGroupRates: () => ({}),
  health: () => ({}),
  healthLoading: false,
  healthWindowMinutes: 10080,
  maxCandidates: 20
})

const emit = defineEmits<{
  'update:modelValue': [value: ApiKeyRoutingDraft]
  'refresh-health': []
}>()

const { t } = useI18n()
const searchText = ref('')
const platformOrder: GroupPlatform[] = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'opencode_go',
  'clinepass',
  'openrouter',
  'commandcode'
]

const groupCatalog = computed(() => {
  const byID = new Map<number, Group>()
  for (const group of props.groups) byID.set(group.id, group)
  for (const candidate of props.modelValue.groups) {
    if (byID.has(candidate.group_id) || !props.modelValue.platform) continue
    byID.set(candidate.group_id, {
      id: candidate.group_id,
      name: t('keys.routing.missingGroup', { id: candidate.group_id }),
      description: null,
      platform: props.modelValue.platform,
      status: 'inactive',
      subscription_type: 'standard',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1
    } as Group)
  }
  return [...byID.values()]
})

const activeGroups = computed(() => groupCatalog.value.filter((group) => group.status === 'active'))

const platformOptions = computed(() => {
  const available = new Set(activeGroups.value.map((group) => group.platform))
  if (props.modelValue.platform) available.add(props.modelValue.platform)
  return platformOrder
    .filter((platform) => available.has(platform))
    .map((platform) => ({ value: platform, label: platformLabel(platform) }))
})

const strategyOptions = computed<Array<{ value: ApiKeyRoutingStrategy; label: string }>>(() => [
  { value: 'balanced', label: t('keys.routing.strategies.balanced') },
  { value: 'stability_first', label: t('keys.routing.strategies.stabilityFirst') },
  { value: 'cost_first', label: t('keys.routing.strategies.costFirst') },
  { value: 'manual', label: t('keys.routing.strategies.manual') }
])

const selectedGroupIDs = computed(() =>
  [...props.modelValue.groups]
    .sort((left, right) => left.priority - right.priority || left.group_id - right.group_id)
    .map((item) => item.group_id)
)

const groupsForPlatform = computed(() => {
  if (!props.modelValue.platform) return []
  const selected = new Set(selectedGroupIDs.value)
  return groupCatalog.value.filter((group) =>
    selected.has(group.id) ||
    (group.platform === props.modelValue.platform && group.status === 'active')
  )
})

const visibleGroups = computed(() => {
  const query = searchText.value.trim().toLowerCase()
  const byID = new Map(groupsForPlatform.value.map((group) => [group.id, group]))
  const selected = selectedGroupIDs.value
    .map((id) => byID.get(id))
    .filter((group): group is Group => Boolean(group))
  const selectedSet = new Set(selectedGroupIDs.value)
  const remaining = groupsForPlatform.value
    .filter((group) => group.status === 'active' && !selectedSet.has(group.id))
    .sort((left, right) => left.name.localeCompare(right.name))
  const ordered = [...selected, ...remaining]
  if (!query) return ordered
  return ordered.filter((group) =>
    group.name.toLowerCase().includes(query) || group.description?.toLowerCase().includes(query)
  )
})

const emitGroups = (groupIDs: number[]) => {
  emit('update:modelValue', {
    ...props.modelValue,
    groups: groupIDs.map((groupID, priority) => ({ group_id: groupID, priority }))
  })
}

const setPlatform = (value: string | number | boolean | null) => {
  const platform = typeof value === 'string' ? value as GroupPlatform : ''
  if (platform === props.modelValue.platform) return
  searchText.value = ''
  emit('update:modelValue', {
    platform,
    strategy: props.modelValue.strategy,
    groups: []
  })
}

const setStrategy = (strategy: ApiKeyRoutingStrategy) => {
  emit('update:modelValue', { ...props.modelValue, strategy })
}

const isSelected = (groupID: number) => selectedGroupIDs.value.includes(groupID)
const selectedIndex = (groupID: number) => selectedGroupIDs.value.indexOf(groupID)
const canToggle = (groupID: number) => isSelected(groupID) || selectedGroupIDs.value.length < props.maxCandidates
const groupHealth = (groupID: number) => props.health[groupID]
const healthWindowLabel = computed(() => {
  if (props.healthWindowMinutes >= 1440 && props.healthWindowMinutes % 1440 === 0) {
    return t('keys.routing.healthWindowDays', { days: props.healthWindowMinutes / 1440 })
  }
  return t('keys.routing.healthWindow', { minutes: props.healthWindowMinutes })
})

const healthStatusLabel = (groupID: number) => {
  if (props.healthLoading && !groupHealth(groupID)) return t('common.loading')
  return t(`keys.routing.healthStatus.${groupHealth(groupID)?.status ?? 'unknown'}`)
}

const healthDotClass = (groupID: number) => {
  const status: ApiKeyRoutingHealthStatus = groupHealth(groupID)?.status ?? 'unknown'
  return {
    operational: 'bg-emerald-500',
    degraded: 'bg-amber-500',
    failed: 'bg-red-500',
    unknown: 'bg-gray-300 dark:bg-dark-500'
  }[status]
}

const healthTextClass = (groupID: number) => {
  const status: ApiKeyRoutingHealthStatus = groupHealth(groupID)?.status ?? 'unknown'
  return {
    operational: 'text-emerald-700 dark:text-emerald-300',
    degraded: 'text-amber-700 dark:text-amber-300',
    failed: 'text-red-700 dark:text-red-300',
    unknown: 'text-gray-500 dark:text-gray-400'
  }[status]
}

const latencyLabel = (groupID: number) => {
  const latency = groupHealth(groupID)?.average_latency_ms
  if (latency == null) return t('keys.routing.noHealthData')
  if (latency < 1000) return `${Math.round(latency)} ms`
  const digits = latency >= 10000 ? 1 : 2
  return `${(latency / 1000).toFixed(digits)} s`
}

const stabilityLabel = (groupID: number) => {
  const rate = groupHealth(groupID)?.success_rate
  if (rate == null) return t('keys.routing.noHealthData')
  const rounded = Math.round(rate * 10) / 10
  return `${rounded.toFixed(Number.isInteger(rounded) ? 0 : 1)}%`
}

const healthTitle = (groupID: number) => {
  const health = groupHealth(groupID)
  if (!health) return t('keys.routing.noHealthData')
  const observedAt = health.last_observed_at
    ? new Date(health.last_observed_at).toLocaleString()
    : t('keys.routing.noHealthData')
  return t('keys.routing.healthDetails', {
    status: healthStatusLabel(groupID),
    samples: health.sample_count,
    observedAt
  })
}

const toggleGroup = (groupID: number) => {
  if (!canToggle(groupID)) return
  if (isSelected(groupID)) {
    emitGroups(selectedGroupIDs.value.filter((id) => id !== groupID))
    return
  }
  emitGroups([...selectedGroupIDs.value, groupID])
}

const moveGroup = (groupID: number, direction: -1 | 1) => {
  const currentIndex = selectedIndex(groupID)
  const targetIndex = currentIndex + direction
  if (currentIndex < 0 || targetIndex < 0 || targetIndex >= selectedGroupIDs.value.length) return
  const reordered = [...selectedGroupIDs.value]
  ;[reordered[currentIndex], reordered[targetIndex]] = [reordered[targetIndex], reordered[currentIndex]]
  emitGroups(reordered)
}
</script>
