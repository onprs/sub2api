<template>
  <span :class="['inline-flex h-6 items-center rounded-md px-2 text-xs font-medium', colorClass]">
    {{ t(`tickets.status.${status}`) }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TicketStatus } from '@/types/ticket'

const props = defineProps<{ status: TicketStatus }>()
const { t } = useI18n()

const colorClass = computed(() => ({
  open: 'bg-blue-50 text-blue-700 dark:bg-blue-950/50 dark:text-blue-300',
  in_progress: 'bg-amber-50 text-amber-700 dark:bg-amber-950/50 dark:text-amber-300',
  waiting_user: 'bg-rose-50 text-rose-700 dark:bg-rose-950/50 dark:text-rose-300',
  resolved: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300',
  closed: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
}[props.status]))
</script>
