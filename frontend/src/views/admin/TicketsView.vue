<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.tickets.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.tickets.description') }}</p>
        </div>
        <router-link to="/admin/settings?tab=ticketing" class="btn btn-secondary" :title="t('admin.tickets.settings')">
          <Icon name="cog" size="sm" />
          <span>{{ t('admin.tickets.settings') }}</span>
        </router-link>
      </header>

      <div class="space-y-3 border-y border-gray-200 py-4 dark:border-dark-700">
        <div class="flex overflow-x-auto rounded-md bg-gray-100 p-1 dark:bg-dark-800">
          <button
            v-for="bucket in buckets"
            :key="bucket"
            type="button"
            :class="['h-8 whitespace-nowrap rounded px-3 text-sm font-medium', filters.bucket === bucket ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-dark-300 dark:hover:text-white']"
            @click="setBucket(bucket)"
          >
            {{ t(`admin.tickets.buckets.${bucket}`) }}
            <span class="ml-1 text-xs text-gray-400">{{ bucketCount(bucket) }}</span>
          </button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(240px,1fr)_170px_150px_180px_auto]">
          <div class="relative">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" />
            <input v-model="filters.search" class="input h-9 pl-9" :placeholder="t('admin.tickets.filters.search')" @input="scheduleSearch" />
          </div>
          <Select v-model="filters.category" :options="categoryOptions" @change="applyFilters" />
          <Select v-model="filters.priority" :options="priorityOptions" @change="applyFilters" />
          <Select v-model="assignmentFilter" :options="assignmentOptions" searchable @change="applyAssignmentFilter" />
          <button type="button" class="btn btn-secondary h-9 w-9 px-0" :title="t('common.refresh')" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 sm:max-w-xl">
          <label class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.tickets.filters.createdFrom') }}<input v-model="createdFrom" type="date" class="input mt-1 h-9" @change="applyDateFilters" /></label>
          <label class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.tickets.filters.createdTo') }}<input v-model="createdTo" type="date" class="input mt-1 h-9" @change="applyDateFilters" /></label>
        </div>
      </div>

      <div v-if="loading && tickets.length === 0" class="py-16 text-center"><LoadingSpinner /></div>
      <EmptyState v-else-if="tickets.length === 0" :title="t('tickets.empty')" :description="t('admin.tickets.description')" />
      <template v-else>
        <div class="hidden overflow-hidden border-y border-gray-200 dark:border-dark-700 lg:block">
          <table class="w-full table-fixed">
            <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="w-[31%] px-4 py-3">{{ t('admin.tickets.columns.ticket') }}</th>
                <th class="w-[22%] px-4 py-3">{{ t('admin.tickets.columns.requester') }}</th>
                <th class="w-[12%] px-4 py-3">{{ t('admin.tickets.columns.priority') }}</th>
                <th class="w-[15%] px-4 py-3">{{ t('admin.tickets.columns.assignee') }}</th>
                <th class="w-[20%] px-4 py-3">{{ t('admin.tickets.columns.status') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="ticket in tickets" :key="ticket.ticket_no" class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800/60" @click="openTicket(ticket.ticket_no)">
                <td class="px-4 py-4">
                  <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ ticket.subject }}</div>
                  <div class="mt-1 text-xs text-gray-400">{{ ticket.ticket_no }} · {{ t(`tickets.category.${ticket.category}`) }}</div>
                </td>
                <td class="px-4 py-4"><div class="truncate text-sm text-gray-700 dark:text-gray-200">{{ ticket.requester_username || `#${ticket.user_id}` }}</div><div class="truncate text-xs text-gray-400">{{ ticket.requester_email }}</div></td>
                <td class="px-4 py-4"><span :class="priorityClass(ticket.priority)" class="inline-flex h-6 items-center rounded-md px-2 text-xs font-medium">{{ t(`tickets.priority.${ticket.priority}`) }}</span></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ ticket.assignee_id ? `#${ticket.assignee_id}` : t('admin.tickets.filters.unassigned') }}</td>
                <td class="px-4 py-4"><TicketStatusBadge :status="ticket.status" /><div class="mt-1 text-xs text-gray-400">{{ formatDate(ticket.action_required_since || ticket.last_activity_at) }}</div></td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700 lg:hidden">
          <button v-for="ticket in tickets" :key="ticket.ticket_no" type="button" class="block w-full py-4 text-left" @click="openTicket(ticket.ticket_no)">
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ ticket.subject }}</div><div class="mt-1 truncate text-xs text-gray-400">{{ ticket.ticket_no }} · {{ ticket.requester_email }}</div></div><TicketStatusBadge :status="ticket.status" /></div>
            <div class="mt-2 flex items-center justify-between text-xs text-gray-500"><span>{{ t(`tickets.priority.${ticket.priority}`) }} · {{ ticket.assignee_id ? `#${ticket.assignee_id}` : t('admin.tickets.filters.unassigned') }}</span><span>{{ formatDate(ticket.action_required_since || ticket.last_activity_at) }}</span></div>
          </button>
        </div>

        <Pagination v-if="pagination.total > pagination.page_size" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="changePage" @update:page-size="changePageSize" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketStatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { adminAPI } from '@/api'
import { useTicketNotificationsStore } from '@/stores'
import type { AdminTicket, AdminTicketListParams, TicketPriority } from '@/types/ticket'

const { t, locale } = useI18n()
const router = useRouter()
const notifications = useTicketNotificationsStore()
const buckets = ['action_required', 'in_progress', 'waiting_user', 'ended', 'all'] as const
const filters = reactive<AdminTicketListParams>({ bucket: 'action_required', category: '', priority: '', search: '', unassigned: false, page: 1, page_size: 20 })
const tickets = ref<AdminTicket[]>([])
const pagination = reactive({ total: 0, page: 1, page_size: 20, pages: 0 })
const loading = ref(false)
const assignmentFilter = ref<string | number>('')
const assignmentUsers = ref<SelectOption[]>([])
const createdFrom = ref('')
const createdTo = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null
let controller: AbortController | null = null

const categoryOptions = computed<SelectOption[]>(() => [{ value: '', label: t('admin.tickets.filters.category') }, ...['api_issue', 'subscription', 'payment', 'account', 'feature_request', 'other'].map((value) => ({ value, label: t(`tickets.category.${value}`) }))])
const priorityOptions = computed<SelectOption[]>(() => [{ value: '', label: t('admin.tickets.filters.priority') }, ...['urgent', 'high', 'normal', 'low'].map((value) => ({ value, label: t(`tickets.priority.${value}`) }))])
const assignmentOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.tickets.filters.allAssignees') },
  { value: 'unassigned', label: t('admin.tickets.filters.unassigned') },
  ...assignmentUsers.value,
])

async function load(): Promise<void> {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  try {
    const result = await adminAPI.tickets.list(filters, controller.signal)
    tickets.value = result.items
    Object.assign(pagination, result)
  } finally {
    loading.value = false
  }
}
function setBucket(bucket: typeof buckets[number]): void { filters.bucket = bucket; filters.page = 1; load() }
function applyFilters(): void { filters.page = 1; load() }
function applyAssignmentFilter(): void {
  filters.unassigned = assignmentFilter.value === 'unassigned'
  filters.assignee_id = typeof assignmentFilter.value === 'number' ? assignmentFilter.value : undefined
  applyFilters()
}
function applyDateFilters(): void {
  filters.created_from = createdFrom.value ? new Date(`${createdFrom.value}T00:00:00`).toISOString() : undefined
  filters.created_to = createdTo.value ? new Date(`${createdTo.value}T23:59:59.999`).toISOString() : undefined
  applyFilters()
}
async function loadAssignmentUsers(): Promise<void> {
  try {
    const result = await adminAPI.users.list(1, 100, { role: 'admin', status: 'active' })
    assignmentUsers.value = result.items.map((user) => ({ value: user.id, label: user.username || user.email }))
  } catch { assignmentUsers.value = [] }
}
function scheduleSearch(): void { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(applyFilters, 300) }
function changePage(page: number): void { filters.page = page; load() }
function changePageSize(size: number): void { filters.page_size = size; filters.page = 1; load() }
function openTicket(ticketNo: string): void { router.push(`/admin/tickets/${encodeURIComponent(ticketNo)}`) }
function bucketCount(bucket: typeof buckets[number]): number { return notifications.adminCounts?.[bucket] ?? 0 }
function formatDate(value: string): string { return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) }
function priorityClass(priority: TicketPriority): string { return ({ urgent: 'bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-300', high: 'bg-orange-100 text-orange-700 dark:bg-orange-950/50 dark:text-orange-300', normal: 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300', low: 'bg-sky-100 text-sky-700 dark:bg-sky-950/50 dark:text-sky-300' })[priority] }

onMounted(() => { load(); loadAssignmentUsers() })
onBeforeUnmount(() => { controller?.abort(); if (searchTimer) clearTimeout(searchTimer) })
</script>
