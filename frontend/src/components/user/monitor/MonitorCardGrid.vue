<template>
  <div>
    <div
      v-if="loading && items.length === 0"
      class="grid gap-5 grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="p-5 rounded-2xl min-h-[280px] bg-white/70 dark:bg-dark-800/60 border border-gray-200/80 dark:border-dark-700/70 animate-pulse"
      >
        <div class="flex items-start gap-3">
          <div class="w-9 h-9 rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="flex-1 space-y-2">
            <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="h-6 w-16 rounded-full bg-gray-200 dark:bg-dark-700"></div>
        </div>
        <div class="mt-5 grid grid-cols-2 gap-2">
          <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
          <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
        </div>
        <div class="mt-6 h-5 w-full rounded bg-gray-100 dark:bg-dark-900/40"></div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <div v-else class="divide-y divide-gray-200 dark:divide-dark-700">
      <section
        v-for="group in groupedItems"
        :key="group.key"
        :aria-labelledby="`channel-platform-${group.key}`"
        class="py-6 first:pt-0 last:pb-0"
      >
        <h2
          :id="`channel-platform-${group.key}`"
          class="mb-4 text-sm font-semibold tracking-wide text-gray-900 dark:text-gray-100"
        >
          {{ t(group.titleKey) }}
        </h2>
        <div class="grid gap-5 grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          <MonitorCard
            v-for="item in group.items"
            :key="item.id"
            :item="item"
            :window="window"
            :availability-value="resolveAvailability(item)"
            :countdown-seconds="countdownSeconds"
            @click="emit('cardClick', item)"
          />
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView, UserMonitorDetail } from '@/api/channelMonitor'
import type { Provider } from '@/api/admin/channelMonitor'
import {
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GROK,
  PROVIDER_GEMINI,
  PROVIDER_ANTIGRAVITY,
} from '@/constants/channelMonitor'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from './MonitorCard.vue'

const props = defineProps<{
  items: UserMonitorView[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: UserMonitorView): void
}>()

const { t, locale } = useI18n()

const platformGroups = [
  { key: 'openai', titleKey: 'monitorCommon.providers.openai' },
  { key: 'anthropic', titleKey: 'monitorCommon.providers.anthropic' },
  { key: 'grok', titleKey: 'monitorCommon.providers.grok' },
  { key: 'gemini', titleKey: 'channelStatus.platformGroups.gemini' },
  { key: 'other', titleKey: 'channelStatus.platformGroups.other' },
] as const

type PlatformGroup = (typeof platformGroups)[number]['key']

const platformByProvider: Partial<Record<Provider, PlatformGroup>> = {
  [PROVIDER_OPENAI]: 'openai',
  [PROVIDER_ANTHROPIC]: 'anthropic',
  [PROVIDER_GROK]: 'grok',
  [PROVIDER_GEMINI]: 'gemini',
  [PROVIDER_ANTIGRAVITY]: 'gemini',
}

const groupedItems = computed(() => {
  const collator = new Intl.Collator(locale.value, { numeric: true, sensitivity: 'base' })
  const sortedItems = [...props.items].sort((left, right) =>
    collator.compare(left.name, right.name) || left.id - right.id,
  )

  return platformGroups
    .map((group) => ({
      ...group,
      items: sortedItems.filter((item) =>
        (platformByProvider[item.provider] ?? 'other') === group.key,
      ),
    }))
    .filter((group) => group.items.length > 0)
})

function resolveAvailability(item: UserMonitorView): number | null {
  if (props.window === '7d') {
    return item.availability_7d ?? null
  }
  const detail = props.detailCache[item.id]
  if (!detail) return null
  const primary = detail.models.find(m => m.model === item.primary_model)
  if (!primary) return null
  return props.window === '15d' ? primary.availability_15d ?? null : primary.availability_30d ?? null
}
</script>
