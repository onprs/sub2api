<template>
  <AppLayout>
    <div v-if="loading && !detail" class="py-20 text-center"><LoadingSpinner /></div>
    <div v-else-if="detail" class="space-y-5">
      <header class="space-y-4 border-b border-gray-200 pb-5 dark:border-dark-700">
        <button type="button" class="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-primary-600" @click="router.push('/admin/tickets')"><Icon name="arrowLeft" size="sm" /> {{ t('admin.tickets.detail.back') }}</button>
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-3"><h1 class="break-words text-xl font-semibold text-gray-900 dark:text-white">{{ detail.ticket.subject }}</h1><TicketStatusBadge :status="detail.ticket.status" /></div>
            <div class="mt-2 text-sm text-gray-500">{{ detail.ticket.ticket_no }} · {{ detail.ticket.requester_email }}</div>
          </div>
          <div class="flex flex-wrap gap-2">
            <button v-if="!detail.ticket.assignee_id && actionAllowed" type="button" class="btn btn-primary" :disabled="submitting" @click="claimTicket">{{ t('admin.tickets.detail.claim') }}</button>
            <button v-if="['open', 'in_progress', 'waiting_user'].includes(detail.ticket.status)" type="button" class="btn btn-secondary" :disabled="submitting" @click="resolveTicket">{{ t('admin.tickets.detail.resolve') }}</button>
            <button v-if="detail.ticket.status === 'resolved'" type="button" class="btn btn-secondary" :disabled="submitting" @click="reopenTicket">{{ t('admin.tickets.detail.reopen') }}</button>
            <button v-if="detail.ticket.status !== 'closed'" type="button" class="btn btn-secondary text-red-600" :disabled="submitting" @click="showClose = !showClose">{{ t('admin.tickets.detail.close') }}</button>
          </div>
        </div>

        <div v-if="detail.ticket.status !== 'closed'" class="grid gap-3 sm:grid-cols-2 lg:max-w-xl">
          <div><label class="input-label">{{ t('admin.tickets.detail.assignment') }}</label><Select v-model="assignee" :options="assigneeOptions" searchable @change="changeAssignee" /></div>
          <div><label class="input-label">{{ t('admin.tickets.detail.priority') }}</label><Select v-model="priority" :options="priorityOptions" @change="changePriority" /></div>
        </div>

        <form v-if="showClose" class="flex flex-col gap-3 border-t border-gray-200 pt-4 dark:border-dark-700 sm:flex-row sm:items-end" @submit.prevent="closeTicket">
          <div class="flex-1"><label class="input-label">{{ t('admin.tickets.detail.closeReason') }}</label><input v-model="closeReason" class="input" maxlength="500" required :placeholder="t('admin.tickets.detail.closeReasonPlaceholder')" /></div>
          <button type="submit" class="btn btn-primary bg-red-600 hover:bg-red-700" :disabled="submitting || !closeReason.trim()">{{ t('admin.tickets.detail.close') }}</button>
        </form>
      </header>

      <div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
        <main class="min-w-0 space-y-5">
          <TicketConversation show-internal :messages="detail.messages" :events="detail.events" @download="download" />

          <form v-if="detail.ticket.status !== 'closed'" class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-700" @submit.prevent="sendMessage">
            <div class="inline-flex rounded-md bg-gray-100 p-1 dark:bg-dark-800">
              <button type="button" :class="segmentClass(visibility === 'public')" @click="visibility = 'public'">{{ t('admin.tickets.detail.replyUser') }}</button>
              <button type="button" :class="segmentClass(visibility === 'internal')" @click="visibility = 'internal'">{{ t('admin.tickets.detail.internalNote') }}</button>
            </div>
            <div :class="visibility === 'internal' ? 'border-l-4 border-amber-400 bg-amber-50 p-3 dark:bg-amber-950/20' : ''">
              <div v-if="visibility === 'internal'" class="mb-2 text-xs font-medium text-amber-800 dark:text-amber-300">{{ t('admin.tickets.detail.internalOnly') }}</div>
              <textarea v-model="draft" class="input min-h-28 resize-y" maxlength="5000" required :placeholder="t('admin.tickets.detail.bodyPlaceholder')"></textarea>
            </div>
            <TicketAttachmentUploader v-model="pendingAttachments" :capabilities="notifications.capabilities" :upload="adminAPI.tickets.uploadAttachment" @error="showError" />
            <div class="flex flex-wrap items-center justify-end gap-3">
              <div v-if="visibility === 'public'" class="w-full sm:w-52"><Select v-model="nextAction" :options="nextActionOptions" /></div>
              <button type="submit" class="btn btn-primary" :disabled="submitting || !draft.trim()">{{ t('admin.tickets.detail.send') }}</button>
            </div>
          </form>
        </main>

        <aside class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700 lg:border-l lg:border-t-0 lg:pl-5 lg:pt-0">
          <h2 class="text-sm font-semibold uppercase text-gray-500 dark:text-dark-400">{{ t('tickets.detail.context') }}</h2>
          <dl class="space-y-3 text-sm">
            <div><dt class="text-gray-400">User</dt><dd class="mt-0.5 break-words text-gray-800 dark:text-gray-200">{{ detail.ticket.requester_username || `#${detail.ticket.user_id}` }}<br><span class="text-xs text-gray-500">{{ detail.ticket.requester_email }}</span></dd></div>
            <div><dt class="text-gray-400">{{ t('tickets.create.category') }}</dt><dd class="mt-0.5 text-gray-800 dark:text-gray-200">{{ t(`tickets.category.${detail.ticket.category}`) }}</dd></div>
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketAttachmentUploader from '@/components/tickets/TicketAttachmentUploader.vue'
import TicketConversation from '@/components/tickets/TicketConversation.vue'
import TicketStatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { mergeAdminTicketDetail } from '@/components/tickets/detailMerge'
import { adminAPI } from '@/api'
import { createTicketIdempotencyKey } from '@/api/tickets'
import { useAppStore, useTicketNotificationsStore } from '@/stores'
import type { AdminTicketDetail, PendingTicketAttachment, TicketMessageAttachment, TicketPriority, TicketVisibility } from '@/types/ticket'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const notifications = useTicketNotificationsStore()
const detail = ref<AdminTicketDetail | null>(null)
const loading = ref(false)
const submitting = ref(false)
const draft = ref('')
const visibility = ref<TicketVisibility>('public')
const nextAction = ref<'wait_user' | 'keep_processing' | 'resolve'>('wait_user')
const pendingAttachments = ref<PendingTicketAttachment[]>([])
const assignee = ref<number | null>(null)
const priority = ref<TicketPriority>('normal')
const adminOptions = ref<SelectOption[]>([])
const showClose = ref(false)
const closeReason = ref('')
let intentKey: string | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let controller: AbortController | null = null
const objectURLs = new Set<string>()

const actionAllowed = computed(() => detail.value ? ['open', 'in_progress'].includes(detail.value.ticket.status) : false)
const assigneeOptions = computed<SelectOption[]>(() => [{ value: null, label: t('admin.tickets.filters.unassigned') }, ...adminOptions.value])
const priorityOptions = computed<SelectOption[]>(() => ['urgent', 'high', 'normal', 'low'].map((value) => ({ value, label: t(`tickets.priority.${value}`) })))
const nextActionOptions = computed<SelectOption[]>(() => [
  { value: 'wait_user', label: t('admin.tickets.detail.waitUser') },
  { value: 'keep_processing', label: t('admin.tickets.detail.keepProcessing') },
  { value: 'resolve', label: t('admin.tickets.detail.replyAndResolve') },
])

watch(detail, (value) => {
  if (!value) return
  assignee.value = value.ticket.assignee_id ?? null
  priority.value = value.ticket.priority
})
watch([draft, visibility, nextAction, pendingAttachments], () => {
  intentKey = null
}, { deep: true })

function ticketNo(): string { return String(route.params.ticketNo || '') }
async function load(background = false, force = false): Promise<void> {
  if ((!force && submitting.value) || document.visibilityState === 'hidden') return
  controller?.abort(); controller = new AbortController()
  if (!background) loading.value = true
  try {
    const incoming = await adminAPI.tickets.get(ticketNo(), controller.signal)
    detail.value = mergeAdminTicketDetail(detail.value, incoming)
  } finally { loading.value = false }
}
async function mutate(action: () => Promise<unknown>, returnsDetail = false): Promise<boolean> {
  submitting.value = true
  try {
    const result = await action()
    if (returnsDetail && result) detail.value = result as AdminTicketDetail
    else await load(false, true)
    await notifications.refresh(true)
    return true
  } catch (error) {
    await handleMutationError(error)
    return false
  } finally {
    submitting.value = false
  }
}
async function claimTicket(): Promise<void> { if (detail.value) await mutate(() => adminAPI.tickets.claim(ticketNo(), detail.value!.ticket.version), true) }
async function resolveTicket(): Promise<void> { if (detail.value) await mutate(() => adminAPI.tickets.resolve(ticketNo(), detail.value!.ticket.version), true) }
async function reopenTicket(): Promise<void> { if (detail.value) await mutate(() => adminAPI.tickets.reopen(ticketNo(), detail.value!.ticket.version)) }
async function changeAssignee(): Promise<void> { if (detail.value && assignee.value !== (detail.value.ticket.assignee_id ?? null)) await mutate(() => adminAPI.tickets.assign(ticketNo(), assignee.value, detail.value!.ticket.version), true) }
async function changePriority(): Promise<void> { if (detail.value && priority.value !== detail.value.ticket.priority) await mutate(() => adminAPI.tickets.changePriority(ticketNo(), priority.value, detail.value!.ticket.version), true) }
async function closeTicket(): Promise<void> {
  if (!detail.value) return
  const success = await mutate(() => adminAPI.tickets.close(ticketNo(), closeReason.value, detail.value!.ticket.version))
  if (success) { showClose.value = false; closeReason.value = '' }
}
async function sendMessage(): Promise<void> {
  if (!detail.value) return
  intentKey ||= createTicketIdempotencyKey()
  const currentIntentKey = intentKey
  const success = await mutate(() => adminAPI.tickets.sendMessage(ticketNo(), {
    visibility: visibility.value, body: draft.value,
    next_action: visibility.value === 'public' ? nextAction.value : undefined,
    expected_version: detail.value!.ticket.version,
    attachment_tokens: pendingAttachments.value.map((item) => item.upload_token),
  }, currentIntentKey), true)
  if (success) { draft.value = ''; pendingAttachments.value = []; intentKey = null }
}

async function handleMutationError(error: unknown): Promise<void> {
  const apiError = error as { code?: string | number; reason?: string; message?: string }
  if (apiError.reason === 'TICKET_VERSION_CONFLICT' || apiError.code === 'TICKET_VERSION_CONFLICT') {
    intentKey = null
    appStore.showWarning(t('admin.tickets.detail.conflict'))
    await load(true, true)
    return
  }
  showError(error)
}
function showError(error: unknown): void { appStore.showError((error as { message?: string })?.message || t('admin.tickets.detail.operationFailed')) }
function segmentClass(active: boolean): string { return `h-8 rounded px-3 text-sm font-medium ${active ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white' : 'text-gray-500 dark:text-dark-300'}` }

async function download(attachment: TicketMessageAttachment): Promise<void> {
  try {
    const blob = await adminAPI.tickets.downloadAttachment(ticketNo(), attachment.id)
    const url = URL.createObjectURL(blob); objectURLs.add(url)
    if (attachment.content_type.startsWith('image/')) {
      window.open(url, '_blank', 'noopener,noreferrer')
    } else {
      const link = document.createElement('a'); link.href = url; link.download = attachment.original_name; link.click()
    }
    setTimeout(() => { URL.revokeObjectURL(url); objectURLs.delete(url) }, attachment.content_type.startsWith('image/') ? 60_000 : 1000)
  } catch (error) { showError(error) }
}
async function loadAdmins(): Promise<void> {
  try {
    const result = await adminAPI.users.list(1, 100, { role: 'admin', status: 'active' })
    adminOptions.value = result.items.map((user) => ({ value: user.id, label: user.username || user.email }))
  } catch { adminOptions.value = [] }
}
function schedulePolling(): void { if (pollTimer) clearInterval(pollTimer); if (document.visibilityState === 'hidden') return; pollTimer = setInterval(() => load(true).catch(() => undefined), (notifications.capabilities?.detail_polling_seconds || 15) * 1000) }
function visibilityChanged(): void { if (document.visibilityState === 'visible') load(true).finally(schedulePolling); else schedulePolling() }

onMounted(() => { load().catch(showError).finally(schedulePolling); loadAdmins(); document.addEventListener('visibilitychange', visibilityChanged) })
onBeforeUnmount(() => { controller?.abort(); if (pollTimer) clearInterval(pollTimer); document.removeEventListener('visibilitychange', visibilityChanged); objectURLs.forEach((url) => URL.revokeObjectURL(url)) })
</script>
