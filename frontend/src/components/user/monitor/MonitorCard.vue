<template>
  <button
    type="button"
    class="group text-left p-5 rounded-2xl min-h-[280px] min-w-0 w-full overflow-hidden bg-white/70 backdrop-blur-xl border border-gray-200/80 shadow-card dark:bg-dark-800/60 dark:border-dark-700/70 hover:-translate-y-1 hover:shadow-card-hover dark:hover:border-primary-500/30 hover:border-gray-300 transition-all duration-300 ease-out flex flex-col"
    @click="emit('click')"
  >
    <!-- 名称与状态 -->
    <div class="flex min-w-0 items-start justify-between gap-3">
      <div class="min-w-0 break-words [overflow-wrap:anywhere] text-base font-semibold text-gray-900 dark:text-gray-100">
        {{ item.name }}
      </div>
      <span
        class="px-2.5 py-1 rounded-full text-xs font-semibold flex-shrink-0"
        :class="statusBadgeClass(item.primary_status)"
      >
        {{ statusLabel(item.primary_status) }}
      </span>
    </div>

    <!-- 分组倍率与模型名称分别占行，长模型名可完整换行 -->
    <div class="mt-2 flex min-w-0 flex-col items-start gap-1.5">
      <span
        v-if="groupRateLabel !== null"
        class="inline-flex max-w-full items-center rounded-md px-2 py-0.5 text-xs font-medium bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300"
      >
        {{ t('monitorCommon.groupRate', { rate: groupRateLabel }) }}
      </span>
      <span class="w-full min-w-0 break-all font-mono text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ formatMonitorModel(item.primary_model) }}
      </span>
    </div>

    <!-- 延迟指标 -->
    <MonitorMetricPair
      primary-icon="bolt"
      :primary-label="t('monitorCommon.dialogLatency')"
      :primary-value="formatLatency(item.primary_latency_ms)"
      primary-unit="ms"
      secondary-icon="globe"
      :secondary-label="t('monitorCommon.endpointPing')"
      :secondary-value="formatLatency(item.primary_ping_latency_ms)"
      secondary-unit="ms"
      :show-secondary="item.target_type === 'external'"
    />

    <!-- 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） -->
    <MonitorQuotaView v-if="quotaVisible" :snapshot="item.latest_quota" class="mt-2" />

    <!-- Divider -->
    <div class="mt-4 border-t border-gray-100 dark:border-dark-700/60"></div>

    <!-- Availability row -->
    <MonitorAvailabilityRow
      :window-label="availabilityLabel"
      :value="availabilityValue"
      :samples-label="extraModelsCountLabel"
    />

    <!-- Timeline -->
    <MonitorTimeline
      :buckets="item.timeline"
      :countdown-seconds="countdownSeconds"
    />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { formatMultiplier } from '@/utils/formatters'
import { isChannelMonitorQuotaVisible } from '@/utils/featureFlags'
import MonitorMetricPair from './MonitorMetricPair.vue'
import MonitorAvailabilityRow from './MonitorAvailabilityRow.vue'
import MonitorTimeline from './MonitorTimeline.vue'
import MonitorQuotaView from '@/components/common/MonitorQuotaView.vue'

const props = defineProps<{
  item: UserMonitorView
  window: '7d' | '15d' | '30d'
  availabilityValue: number | null
  countdownSeconds: number
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()

const { t } = useI18n()
const {
  statusLabel,
  statusBadgeClass,
  formatLatency,
  formatMonitorModel,
} = useChannelMonitorFormat()

const groupRateLabel = computed(() => {
  const min = props.item.group_dynamic_rate_min_multiplier
  const max = props.item.group_dynamic_rate_max_multiplier
  if (min != null && max != null) {
    if (min === max) return `${formatMultiplier(min)}x`
    return `${formatMultiplier(min)}x–${formatMultiplier(max)}x`
  }
  const rate = props.item.group_rate_multiplier
  return rate == null ? null : `${formatMultiplier(rate)}x`
})

const quotaVisible = computed(
  () => isChannelMonitorQuotaVisible() && !!props.item.latest_quota
)

const availabilityLabel = computed(() => {
  const win = t(`channelStatus.windowTab.${props.window}`)
  return `${t('monitorCommon.availabilityPrefix')} · ${win}`
})

const extraModelsCountLabel = computed(() => {
  const count = props.item.extra_models?.length ?? 0
  if (count === 0) return undefined
  return t('monitorCommon.extraModelsCount', { n: count })
})
</script>
