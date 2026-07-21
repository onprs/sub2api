<template>
  <AppLayout>
    <div v-if="loading && !detail" class="py-20 text-center"><LoadingSpinner /></div>
    <div v-else-if="detail" class="space-y-5">
      <header class="flex flex-wrap items-start justify-between gap-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <div class="min-w-0">
          <button type="button" class="mb-3 inline-flex items-center gap-1 text-sm text-gray-500 hover:text-primary-600" @click="router.push('/tickets')">
            <Icon name="arrowLeft" size="sm" /> {{ t('tickets.detail.back') }}
          </button>
          <div class="flex flex-wrap items-center gap-3">
            <h1 class="break-words text-xl font-semibold text-gray-900 dark:text-white">{{ detail.ticket.subject }}</h1>
            <TicketStatusBadge :status="detail.ticket.status" />
          </div>
          <div class="mt-2 text-sm text-gray-500">{{ detail.ticket.ticket_no }} · {{ t(`tickets.category.${detail.ticket.category}`) }}</div>
        </div>
        <div v-if="detail.ticket.status !== 'closed'" class="flex flex-wrap gap-2">
          <button
            v-if="detail.ticket.status === 'resolved'"
            type="button"
            class="btn btn-secondary"
            :disabled="submitting"
            @click="showReopen = !showReopen"
          >{{ t('tickets.detail.reopen') }}</button>
          <button type="button" class="btn btn-secondary text-red-600" :disabled="submitting" @click="closeTicket">
            {{ detail.ticket.status === 'resolved' ? t('tickets.detail.confirmResolved') : t('tickets.detail.close') }}
          </button>
        </div>
      </header>

      <div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_280px]">
        <main class="min-w-0 space-y-5">
          <h2 class="text-sm font-semibold uppercase text-gray-500 dark:text-dark-400">{{ t('tickets.detail.conversation') }}</h2>
          <TicketConversation :messages="detail.messages" :events="detail.events" @download="download" />

          <form v-if="detail.ticket.status !== 'closed' && detail.ticket.status !== 'resolved'" class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-700" @submit.prevent="sendReply">
            <textarea v-model="draft" class="input min-h-28 resize-y" maxlength="5000" required :placeholder="t('tickets.detail.replyPlaceholder')"></textarea>
            <TicketAttachmentUploader
              v-model="pendingAttachments"
              :capabilities="notifications.capabilities"
              :upload="ticketsAPI.uploadAttachment"
              @error="showError"
            />
            <div class="flex justify-end">
              <button type="submit" class="btn btn-primary" :disabled="submitting || !draft.trim()">{{ t('tickets.detail.send') }}</button>
            </div>
          </form>

          <form v-if="detail.ticket.status === 'resolved' && showReopen" class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-700" @submit.prevent="reopenTicket">
            <textarea v-model="draft" class="input min-h-28 resize-y" maxlength="5000" required :placeholder="t('tickets.detail.reopenPlaceholder')"></textarea>
            <div class="flex justify-end">
              <button type="submit" class="btn btn-primary" :disabled="submitting || !draft.trim()">{{ t('tickets.detail.reopen') }}</button>
            </div>
          </form>

          <div v-if="detail.ticket.status === 'closed'" class="border-t border-gray-200 py-5 text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">
            {{ t('tickets.detail.closedHint') }}
          </div>
        </main>

        <aside class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700 lg:border-l lg:border-t-0 lg:pl-5 lg:pt-0">
          <h2 class="text-sm font-semibold uppercase text-gray-500 dark:text-dark-400">{{ t('tickets.detail.context') }}</h2>
          <dl class="space-y-3 text-sm">
            <div><dt class="text-gray-400">{{ t('tickets.create.impact') }}</dt><dd class="mt-0.5 text-gray-800 dark:text-gray-200">{{ t(`tickets.impact.${detail.ticket.impact}`) }}</dd></div>
            <div v-if="detail.ticket.request_id"><dt class="text-gray-400">Request ID</dt><dd class="mt-0.5 break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ detail.ticket.request_id }}</dd></div>
            <div v-if="detail.ticket.api_key_name"><dt class="text-gray-400">{{ t('tickets.create.apiKey') }}</dt><dd class="mt-0.5 break-words text-gray-800 dark:text-gray-200">{{ detail.ticket.api_key_name }}</dd></div>
            <div v-if="detail.ticket.payment_order_no"><dt class="text-gray-400">{{ t('tickets.create.order') }}</dt><dd class="mt-0.5 break-all text-gray-800 dark:text-gray-200">{{ detail.ticket.payment_order_no }}</dd></div>
            <div v-if="detail.ticket.subscription_name"><dt class="text-gray-400">{{ t('tickets.create.subscription') }}</dt><dd class="mt-0.5 break-words text-gray-800 dark:text-gray-200">{{ detail.ticket.subscription_name }}</dd></div>
          </dl>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketAttachmentUploader from '@/components/tickets/TicketAttachmentUploader.vue'
import TicketConversation from '@/components/tickets/TicketConversation.vue'
import TicketStatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { mergeUserTicketDetail } from '@/components/tickets/detailMerge'
import { ticketsAPI } from '@/api'
import { createTicketIdempotencyKey } from '@/api/tickets'
import { useAppStore, useTicketNotificationsStore } from '@/stores'
import type { PendingTicketAttachment, TicketMessageAttachment, UserTicketDetail } from '@/types/ticket'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const notifications = useTicketNotificationsStore()
const detail = ref<UserTicketDetail | null>(null)
const loading = ref(false)
const submitting = ref(false)
const draft = ref('')
const pendingAttachments = ref<PendingTicketAttachment[]>([])
const showReopen = ref(false)
let pollTimer: ReturnType<typeof setInterval> | null = null
let intentKey: string | null = null
let controller: AbortController | null = null
const objectURLs = new Set<string>()

watch([draft, pendingAttachments], () => {
  intentKey = null
}, { deep: true })

function ticketNo(): string {
  return String(route.params.ticketNo || '')
}

async function load(background = false, force = false): Promise<void> {
  if ((!force && submitting.value) || document.visibilityState === 'hidden') return
  controller?.abort()
  controller = new AbortController()
  if (!background) loading.value = true
  try {
    const incoming = await ticketsAPI.get(ticketNo(), controller.signal)
    detail.value = mergeUserTicketDetail(detail.value, incoming)
    const sequence = incoming.ticket.notification_seq
    if (incoming.ticket.unread && sequence > 0) {
      await ticketsAPI.markRead(ticketNo(), sequence)
      detail.value.ticket.unread = false
      notifications.refresh().catch(() => undefined)
    }
  } finally {
    loading.value = false
  }
}

async function sendReply(): Promise<void> {
  if (!detail.value) return
  submitting.value = true
  intentKey ||= createTicketIdempotencyKey()
  try {
    detail.value = await ticketsAPI.reply(ticketNo(), {
      body: draft.value,
      expected_version: detail.value.ticket.version,
      attachment_tokens: pendingAttachments.value.map((item) => item.upload_token),
    }, intentKey)
    draft.value = ''
    pendingAttachments.value = []
    intentKey = null
    await notifications.refresh()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    submitting.value = false
  }
}

async function closeTicket(): Promise<void> {
  if (!detail.value || !window.confirm(t('tickets.detail.close'))) return
  submitting.value = true
  try {
    await ticketsAPI.close(ticketNo(), detail.value.ticket.version)
    await load(false, true)
    await notifications.refresh()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    submitting.value = false
  }
}

async function reopenTicket(): Promise<void> {
  if (!detail.value) return
  submitting.value = true
  intentKey ||= createTicketIdempotencyKey()
  try {
    await ticketsAPI.reopen(ticketNo(), draft.value, detail.value.ticket.version, intentKey)
    draft.value = ''
    intentKey = null
    showReopen.value = false
    await load(false, true)
    await notifications.refresh()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    submitting.value = false
  }
}

async function handleMutationError(error: unknown): Promise<void> {
  const apiError = error as { code?: string | number; reason?: string; message?: string }
  if (apiError.reason === 'TICKET_VERSION_CONFLICT' || apiError.code === 'TICKET_VERSION_CONFLICT') {
    intentKey = null
    appStore.showWarning(t('tickets.detail.refreshConflict'))
    await load(true, true)
    return
  }
  showError(error)
}

function showError(error: unknown): void {
  appStore.showError((error as { message?: string })?.message || t('tickets.detail.actionFailed'))
}

async function download(attachment: TicketMessageAttachment): Promise<void> {
  try {
    const blob = await ticketsAPI.downloadAttachment(ticketNo(), attachment.id)
    const url = URL.createObjectURL(blob)
    objectURLs.add(url)
    if (attachment.content_type.startsWith('image/')) {
      window.open(url, '_blank', 'noopener,noreferrer')
    } else {
      const link = document.createElement('a')
      link.href = url
      link.download = attachment.original_name
      link.click()
    }
    setTimeout(() => {
      URL.revokeObjectURL(url)
      objectURLs.delete(url)
    }, attachment.content_type.startsWith('image/') ? 60_000 : 1000)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('tickets.detail.downloadFailed'))
  }
}

function schedulePolling(): void {
  if (pollTimer) clearInterval(pollTimer)
  if (document.visibilityState === 'hidden') return
  const seconds = notifications.capabilities?.detail_polling_seconds || 15
  pollTimer = setInterval(() => load(true).catch(() => undefined), seconds * 1000)
}

function visibilityChanged(): void {
  if (document.visibilityState === 'visible') load(true).finally(schedulePolling)
  else schedulePolling()
}

onMounted(() => {
  load().catch(showError).finally(schedulePolling)
  document.addEventListener('visibilitychange', visibilityChanged)
})
onBeforeUnmount(() => {
  controller?.abort()
  if (pollTimer) clearInterval(pollTimer)
  document.removeEventListener('visibilitychange', visibilityChanged)
  objectURLs.forEach((url) => URL.revokeObjectURL(url))
})
</script>
