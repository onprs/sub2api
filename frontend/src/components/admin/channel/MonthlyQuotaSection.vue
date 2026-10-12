<template>
  <div class="mt-3 space-y-2" data-test="monthly-quota-section">
    <label class="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400">
      <input
        type="checkbox"
        :checked="modelValue != null"
        @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked ? { base_credits_usd: 70, periods: [] } : null)"
      />
      {{ t('admin.channels.monthlyQuota.title') }}
    </label>
    <template v-if="modelValue">
      <div class="max-w-xs">
        <label class="text-xs text-gray-400">{{ t('admin.channels.monthlyQuota.baseCredits') }} ($)</label>
        <input
          type="number" min="0" step="any" class="input mt-0.5 text-sm"
          :value="modelValue.base_credits_usd"
          @input="emit('update:modelValue', { ...modelValue, base_credits_usd: Number(($event.target as HTMLInputElement).value) })"
        />
      </div>
      <div v-for="(period, index) in modelValue.periods || []" :key="index" class="grid items-end gap-2 sm:grid-cols-4">
        <div>
          <label class="text-xs text-gray-400">{{ t('admin.channels.monthlyQuota.periodCredits') }} ($)</label>
          <input type="number" min="0.000001" step="any" class="input mt-0.5 text-sm" :value="period.credits_usd"
            @input="updatePeriod(index, { credits_usd: Number(($event.target as HTMLInputElement).value) })" />
        </div>
        <div>
          <label class="text-xs text-gray-400">{{ t('admin.channels.monthlyQuota.startsAt') }} (UTC)</label>
          <input type="datetime-local" step="1" class="input mt-0.5 text-sm" :value="utcInput(period.starts_at)"
            @input="updatePeriod(index, { starts_at: utcValue(($event.target as HTMLInputElement).value) })" />
        </div>
        <div>
          <label class="text-xs text-gray-400">{{ t('admin.channels.monthlyQuota.expiresAt') }} (UTC)</label>
          <input type="datetime-local" step="1" class="input mt-0.5 text-sm" :value="utcInput(period.expires_at)"
            @input="updatePeriod(index, { expires_at: utcValue(($event.target as HTMLInputElement).value) })" />
        </div>
        <button type="button" class="btn btn-secondary text-sm" @click="removePeriod(index)">
          {{ t('common.delete') }}
        </button>
      </div>
      <button type="button" class="text-xs text-primary-600 hover:text-primary-700" @click="addPeriod">
        + {{ t('admin.channels.monthlyQuota.addPeriod') }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ChannelMonthlyQuota, ChannelMonthlyQuotaPeriod } from '@/api/admin/channels'

const props = defineProps<{ modelValue?: ChannelMonthlyQuota | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: ChannelMonthlyQuota | null] }>()
const { t } = useI18n()

function utcInput(value?: string | null): string {
  return value ? new Date(value).toISOString().slice(0, 19) : ''
}

function utcValue(value: string): string | null {
  return value ? new Date(`${value}Z`).toISOString() : null
}

function updatePeriod(index: number, change: Partial<ChannelMonthlyQuotaPeriod>) {
  if (!props.modelValue) return
  emit('update:modelValue', {
    ...props.modelValue,
    periods: (props.modelValue.periods || []).map((period, i) => i === index ? { ...period, ...change } : period),
  })
}

function addPeriod() {
  if (!props.modelValue) return
  emit('update:modelValue', {
    ...props.modelValue,
    periods: [...(props.modelValue.periods || []), { credits_usd: props.modelValue.base_credits_usd }],
  })
}

function removePeriod(index: number) {
  if (!props.modelValue) return
  emit('update:modelValue', {
    ...props.modelValue,
    periods: (props.modelValue.periods || []).filter((_, i) => i !== index),
  })
}
</script>
