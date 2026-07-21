<template>
  <AppLayout>
    <div class="mx-auto max-w-[760px] space-y-6">
      <header class="flex items-center gap-3">
        <button type="button" class="flex h-9 w-9 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :title="t('tickets.create.cancel')" @click="router.push('/tickets')">
          <Icon name="arrowLeft" size="md" />
        </button>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('tickets.create.title') }}</h1>
      </header>

      <form class="space-y-5" @submit.prevent="submit">
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('tickets.create.category') }}</label>
            <Select v-model="form.category" :options="categoryOptions" />
          </div>
          <div>
            <label class="input-label">{{ t('tickets.create.impact') }}</label>
            <Select v-model="form.impact" :options="impactOptions" />
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('tickets.create.subject') }}</label>
          <input v-model.trim="form.subject" class="input" maxlength="100" required :placeholder="t('tickets.create.subjectPlaceholder')" />
        </div>
        <div>
          <label class="input-label">{{ t('tickets.create.description') }}</label>
          <textarea v-model="form.body" class="input min-h-40 resize-y" maxlength="5000" required :placeholder="t('tickets.create.bodyPlaceholder')"></textarea>
          <div class="mt-1 text-right text-xs text-gray-400">{{ form.body.length }}/5000</div>
        </div>

        <section v-if="resourceOptions.length" class="border-y border-gray-200 py-4 dark:border-dark-700">
          <label class="input-label">{{ resourceLabel }}</label>
          <Select v-model="selectedResourceId" :options="resourceOptions" clearable searchable />
        </section>

        <TicketAttachmentUploader
          v-model="pendingAttachments"
          :capabilities="notifications.capabilities"
          :upload="ticketsAPI.uploadAttachment"
          @error="handleAttachmentError"
        />

        <div class="flex flex-wrap justify-end gap-3 border-t border-gray-200 pt-5 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" @click="router.push('/tickets')">{{ t('tickets.create.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="submitting || !form.subject.trim() || !form.body.trim()">
            {{ submitting ? t('tickets.create.submitting') : t('tickets.create.submit') }}
          </button>
        </div>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketAttachmentUploader from '@/components/tickets/TicketAttachmentUploader.vue'
import { keysAPI, paymentAPI, ticketsAPI, usageAPI } from '@/api'
import subscriptionsAPI from '@/api/subscriptions'
import { createTicketIdempotencyKey } from '@/api/tickets'
import { useAppStore, useTicketNotificationsStore } from '@/stores'
import type { PendingTicketAttachment, TicketCategory, TicketImpact } from '@/types/ticket'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const notifications = useTicketNotificationsStore()
const submitting = ref(false)
const submitted = ref(false)
const pendingAttachments = ref<PendingTicketAttachment[]>([])
const selectedResourceId = ref<string | number | null>(null)
const apiKeyOptions = ref<SelectOption[]>([])
const usageOptions = ref<SelectOption[]>([])
const orderOptions = ref<SelectOption[]>([])
const subscriptionOptions = ref<SelectOption[]>([])
let intentKey: string | null = null

const form = reactive<{ category: TicketCategory; impact: TicketImpact; subject: string; body: string }>({
  category: 'api_issue', impact: 'general', subject: '', body: '',
})

const categoryOptions = computed<SelectOption[]>(() => ['api_issue', 'subscription', 'payment', 'account', 'feature_request', 'other'].map((value) => ({ value, label: t(`tickets.category.${value}`) })))
const impactOptions = computed<SelectOption[]>(() => ['blocked', 'degraded', 'general'].map((value) => ({ value, label: t(`tickets.impact.${value}`) })))
const resourceOptions = computed(() => {
  if (form.category === 'api_issue') return [...usageOptions.value, ...apiKeyOptions.value]
  if (form.category === 'payment') return orderOptions.value
  if (form.category === 'subscription') return subscriptionOptions.value
  return []
})
const resourceLabel = computed(() => {
  if (form.category === 'payment') return t('tickets.create.order')
  if (form.category === 'subscription') return t('tickets.create.subscription')
  return t('tickets.create.relatedResource')
})
const dirty = computed(() => Boolean(form.subject.trim() || form.body.trim() || pendingAttachments.value.length))

watch(() => form.category, () => { selectedResourceId.value = null })
watch([
  () => form.category,
  () => form.impact,
  () => form.subject,
  () => form.body,
  selectedResourceId,
  pendingAttachments,
], () => {
  intentKey = null
}, { deep: true })

async function loadResources(): Promise<void> {
  const [keys, usage, orders, subscriptions] = await Promise.allSettled([
    keysAPI.list(1, 100),
    usageAPI.list(1, 50),
    paymentAPI.getMyOrders({ page: 1, page_size: 50 }),
    subscriptionsAPI.getMySubscriptions(),
  ])
  if (keys.status === 'fulfilled') {
    apiKeyOptions.value = keys.value.items.map((item) => ({ value: `key:${item.id}`, label: `${t('tickets.create.apiKey')}: ${item.name}` }))
  }
  if (usage.status === 'fulfilled') {
    usageOptions.value = usage.value.items.map((item) => ({ value: `usage:${item.id}`, label: `${t('tickets.create.usageLog')}: ${item.request_id || item.id}` }))
  }
  if (orders.status === 'fulfilled') {
    orderOptions.value = orders.value.data.items.map((item) => ({ value: item.id, label: item.out_trade_no || `#${item.id}` }))
  }
  if (subscriptions.status === 'fulfilled') {
    subscriptionOptions.value = subscriptions.value.map((item) => ({ value: item.id, label: item.group?.name || `#${item.id}` }))
  }
}

async function submit(): Promise<void> {
  submitting.value = true
  intentKey ||= createTicketIdempotencyKey()
  try {
    const references: Record<string, number | null> = {}
    const selected = selectedResourceId.value
    if (typeof selected === 'string' && selected.startsWith('key:')) references.api_key_id = Number(selected.slice(4))
    else if (typeof selected === 'string' && selected.startsWith('usage:')) references.usage_log_id = Number(selected.slice(6))
    else if (typeof selected === 'number' && form.category === 'payment') references.payment_order_id = selected
    else if (typeof selected === 'number' && form.category === 'subscription') references.user_subscription_id = selected

    const created = await ticketsAPI.create({
      category: form.category,
      impact: form.impact,
      subject: form.subject,
      body: form.body,
      attachment_tokens: pendingAttachments.value.map((item) => item.upload_token),
      ...references,
    }, intentKey)
    submitted.value = true
    await notifications.refresh()
    await router.replace(`/tickets/${encodeURIComponent(created.ticket.ticket_no)}`)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('tickets.create.failed'))
  } finally {
    submitting.value = false
  }
}

function handleAttachmentError(error: unknown): void {
  appStore.showError((error as { message?: string })?.message || t('tickets.attachments.uploadFailed'))
}

function beforeUnload(event: BeforeUnloadEvent): void {
  if (!submitted.value && dirty.value) event.preventDefault()
}

onBeforeRouteLeave(() => {
  if (submitted.value || !dirty.value) return true
  return window.confirm(t('tickets.create.leaveWarning'))
})
onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
  notifications.refresh().catch(() => undefined)
  loadResources()
})
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
