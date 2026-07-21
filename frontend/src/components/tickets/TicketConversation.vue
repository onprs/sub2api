<template>
  <div class="space-y-4">
    <div v-if="items.length === 0" class="py-10 text-center text-sm text-gray-500 dark:text-dark-400">
      {{ t('tickets.detail.noConversation') }}
    </div>

    <template v-for="item in items" :key="item.key">
      <div v-if="item.kind === 'event'" class="flex items-center gap-3 py-1">
        <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
        <div class="max-w-[80%] text-center text-xs text-gray-500 dark:text-dark-400">
          <span>{{ eventLabel(item.value.event_type) }}</span>
          <span v-if="eventDetail(item.value)" class="ml-1">{{ eventDetail(item.value) }}</span>
          <span class="ml-2">{{ formatDate(item.value.created_at) }}</span>
        </div>
        <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
      </div>

      <article
        v-else
        :class="[
          'max-w-[92%] rounded-lg border px-4 py-3 sm:max-w-[78%]',
          messageVisibility(item.value) === 'internal'
            ? 'border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30'
            : item.value.author_role === 'user'
              ? 'ml-auto border-primary-200 bg-primary-50 dark:border-primary-900 dark:bg-primary-950/30'
              : 'border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800'
        ]"
      >
        <div class="mb-2 flex min-w-0 items-center justify-between gap-4 text-xs">
          <div class="flex min-w-0 items-center gap-2">
            <span class="truncate font-medium text-gray-800 dark:text-gray-200">{{ item.value.author_name || item.value.author_role }}</span>
            <span v-if="messageVisibility(item.value) === 'internal'" class="rounded bg-amber-200 px-1.5 py-0.5 font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">
              {{ t('admin.tickets.detail.internalOnly') }}
            </span>
          </div>
          <time class="flex-shrink-0 text-gray-400">{{ formatDate(item.value.created_at) }}</time>
        </div>
        <p class="whitespace-pre-wrap break-words text-sm leading-6 text-gray-800 dark:text-gray-100">{{ item.value.body }}</p>
        <div v-if="item.value.attachments?.length" class="mt-3 flex flex-wrap gap-2">
          <button
            v-for="attachment in item.value.attachments"
            :key="attachment.id"
            type="button"
            class="inline-flex max-w-full items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs text-gray-700 hover:border-primary-300 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-200"
            :title="attachment.original_name"
            @click="$emit('download', attachment)"
          >
            <Icon name="download" size="sm" class="flex-shrink-0" />
            <span class="truncate">{{ attachment.original_name }}</span>
            <span class="flex-shrink-0 text-gray-400">{{ formatBytes(attachment.byte_size) }}</span>
          </button>
        </div>
      </article>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type {
  AdminTicketEvent,
  AdminTicketMessage,
  TicketMessageAttachment,
  UserTicketEvent,
  UserTicketMessage,
} from '@/types/ticket'

const props = withDefaults(defineProps<{
  messages: Array<UserTicketMessage | AdminTicketMessage>
  events: Array<UserTicketEvent | AdminTicketEvent>
  showInternal?: boolean
}>(), { showInternal: false })

defineEmits<{ download: [attachment: TicketMessageAttachment] }>()
const { t, locale } = useI18n()

const items = computed(() => [
  ...props.messages
    .filter((value) => props.showInternal || messageVisibility(value) === 'public')
    .map((value) => ({ kind: 'message' as const, key: `m-${value.id}`, value })),
  ...props.events
    .filter((value) => props.showInternal || !('visibility' in value) || value.visibility === 'public')
    .map((value) => ({ kind: 'event' as const, key: `e-${value.id}`, value })),
].sort((left, right) => {
  const time = Date.parse(left.value.created_at) - Date.parse(right.value.created_at)
  if (time !== 0) return time
  if (left.kind !== right.kind) return left.kind === 'message' ? -1 : 1
  return left.value.id - right.value.id
}))

function messageVisibility(message: UserTicketMessage | AdminTicketMessage): 'public' | 'internal' {
  return 'visibility' in message ? message.visibility : 'public'
}

function eventLabel(eventType: string): string {
  const key = `tickets.events.${eventType}`
  const translated = t(key)
  return translated === key ? eventType : translated
}

function eventDetail(event: UserTicketEvent | AdminTicketEvent): string {
  const reason = event.payload?.reason
  return typeof reason === 'string' && reason.trim() ? `: ${reason}` : ''
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
</script>
