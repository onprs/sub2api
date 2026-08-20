<template>
  <div class="space-y-5">
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
      <div class="grid grid-cols-2 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
        <button
          v-for="(option, index) in strategyOptions"
          :key="option.value"
          type="button"
          :class="[
            'min-h-10 px-3 py-2 text-sm font-medium transition-colors focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40',
            index % 2 === 1 ? 'border-l border-gray-200 dark:border-dark-600' : '',
            index >= 2 ? 'border-t border-gray-200 dark:border-dark-600' : '',
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

    <div>
      <div class="mb-2 flex items-center justify-between gap-3">
        <label class="input-label mb-0">{{ t('keys.routing.candidateGroups') }}</label>
        <span class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400">
          {{ t('keys.routing.selectedCount', { count: selectedGroupIDs.length, max: maxCandidates }) }}
        </span>
      </div>

      <div
        v-if="modelValue.platform"
        class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
      >
        <div class="flex items-center gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-800">
          <Icon name="search" size="sm" class="shrink-0 text-gray-400" />
          <input
            v-model="searchText"
            type="search"
            class="min-w-0 flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-white dark:placeholder:text-dark-400"
            :placeholder="t('keys.searchGroup')"
          />
        </div>

        <div class="max-h-64 overflow-y-auto">
          <div
            v-for="group in visibleGroups"
            :key="group.id"
            :class="[
              'flex min-h-12 items-center gap-2 border-b border-gray-100 px-3 py-2 last:border-b-0 dark:border-dark-700',
              isSelected(group.id) ? 'bg-primary-50/60 dark:bg-primary-900/10' : 'bg-white dark:bg-dark-800'
            ]"
          >
            <label
              :class="[
                'flex min-w-0 flex-1 items-center gap-3',
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
                class="inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded bg-gray-100 px-1 text-[11px] font-semibold tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-300"
              >
                {{ selectedIndex(group.id) + 1 }}
              </span>
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
                class="min-w-0"
              />
              <span
                v-if="group.status !== 'active'"
                class="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium text-gray-500 dark:bg-dark-700 dark:text-gray-300"
              >
                {{ t('common.inactive') }}
              </span>
            </label>

            <div v-if="isSelected(group.id)" class="flex h-8 w-16 shrink-0 items-center justify-end gap-1">
              <button
                type="button"
                class="flex h-7 w-7 items-center justify-center rounded text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                :disabled="selectedIndex(group.id) === 0"
                :title="t('keys.routing.moveUp')"
                @click="moveGroup(group.id, -1)"
              >
                <Icon name="chevronUp" size="sm" />
              </button>
              <button
                type="button"
                class="flex h-7 w-7 items-center justify-center rounded text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                :disabled="selectedIndex(group.id) === selectedGroupIDs.length - 1"
                :title="t('keys.routing.moveDown')"
                @click="moveGroup(group.id, 1)"
              >
                <Icon name="chevronDown" size="sm" />
              </button>
            </div>
          </div>

          <div v-if="visibleGroups.length === 0" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
      <div v-else class="rounded-lg border border-dashed border-gray-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
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
  ApiKeyRoutingStrategy,
  Group,
  GroupPlatform
} from '@/types'

const props = withDefaults(defineProps<{
  modelValue: ApiKeyRoutingDraft
  groups: Group[]
  userGroupRates?: Record<number, number>
  maxCandidates?: number
}>(), {
  userGroupRates: () => ({}),
  maxCandidates: 20
})

const emit = defineEmits<{
  'update:modelValue': [value: ApiKeyRoutingDraft]
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
  'openrouter'
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
