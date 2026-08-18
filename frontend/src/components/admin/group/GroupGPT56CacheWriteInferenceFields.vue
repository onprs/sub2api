<template>
  <div class="border-t border-gray-200 pt-4 dark:border-dark-400">
    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.groups.gpt56CacheWriteInference.title') }}
        </label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.groups.gpt56CacheWriteInference.description') }}
        </p>
      </div>
      <Toggle
        :model-value="enabled"
        :aria-label="t('admin.groups.gpt56CacheWriteInference.title')"
        :data-testid="`${testIdPrefix}-gpt56-cache-write-toggle`"
        @update:model-value="emit('update:enabled', $event)"
      />
    </div>

    <div v-if="enabled" class="mt-4 flex items-center justify-between gap-4">
      <label
        class="input-label mb-0"
        :for="`${testIdPrefix}-gpt56-cache-write-min-tokens`"
      >
        {{ t('admin.groups.gpt56CacheWriteInference.minTokens') }}
      </label>
      <input
        :id="`${testIdPrefix}-gpt56-cache-write-min-tokens`"
        :value="minTokens"
        :data-testid="`${testIdPrefix}-gpt56-cache-write-min-tokens`"
        type="number"
        min="1"
        step="1"
        class="input w-32"
        @input="updateMinTokens"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import Toggle from '@/components/common/Toggle.vue'

defineProps<{
  enabled: boolean
  minTokens: number
  testIdPrefix: string
}>()

const emit = defineEmits<{
  (event: 'update:enabled', value: boolean): void
  (event: 'update:minTokens', value: number): void
}>()

const { t } = useI18n()

function updateMinTokens(event: Event) {
  const value = Number((event.target as HTMLInputElement).value)
  const normalized = Number.isFinite(value) && value > 0 ? Math.floor(value) : 1024
  emit('update:minTokens', normalized)
}
</script>
