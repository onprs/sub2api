<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('tickets.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('tickets.description') }}</p>
        </div>
        <router-link to="/tickets/new" class="btn btn-primary">
          <Icon name="plus" size="sm" />
          <span>{{ t('tickets.newTicket') }}</span>
        </router-link>
      </header>

      <div class="flex flex-col gap-3 border-y border-gray-200 py-4 dark:border-dark-700 sm:flex-row sm:items-center">
        <div class="flex overflow-x-auto rounded-md bg-gray-100 p-1 dark:bg-dark-800">
          <button
            v-for="bucket in buckets"
            :key="bucket"
            type="button"
            :class="['h-8 whitespace-nowrap rounded px-3 text-sm font-medium', filters.bucket === bucket ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-dark-300 dark:hover:text-white']"
            @click="setBucket(bucket)"
          >
            {{ t(`tickets.buckets.${bucket}`) }}
            <span class="ml-1 text-xs text-gray-400">{{ bucketCount(bucket) }}</span>
          </button>
        </div>
        <div class="relative min-w-0 flex-1 sm:ml-auto sm:max-w-xs">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" />
          <input v-model="filters.search" class="input h-9 pl-9" :placeholder="t('tickets.searchPlaceholder')" @input="scheduleSearch" />
        </div>
        <button type="button" class="btn btn-secondary h-9 w-9 px-0" :title="t('common.refresh')" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>

      <div v-if="loading && tickets.length === 0" class="py-16 text-center">
        <LoadingSpinner />
      </div>
      <EmptyState
        v-else-if="tickets.length === 0"
        :title="t('tickets.empty')"
        :description="t('tickets.emptyDescription')"
        :action-text="t('tickets.newTicket')"
        @action="router.push('/tickets/new')"
      />
      <template v-else>
        <div class="hidden overflow-hidden border-y border-gray-200 dark:border-dark-700 md:block">
          <table class="w-full table-fixed">
            <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="w-[46%] px-4 py-3">{{ t('tickets.columns.subject') }}</th>
                <th class="w-[18%] px-4 py-3">{{ t('tickets.columns.category') }}</th>
                <th class="w-[16%] px-4 py-3">{{ t('tickets.columns.status') }}</th>
                <th class="w-[20%] px-4 py-3 text-right">{{ t('tickets.columns.updated') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="ticket in tickets" :key="ticket.ticket_no" class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800/60" @click="openTicket(ticket.ticket_no)">
                <td class="px-4 py-4">
                  <div class="flex min-w-0 items-center gap-2">
                    <span v-if="ticket.unread" class="h-2 w-2 flex-shrink-0 rounded-full bg-rose-500" :title="t('tickets.unread')"></span>
                    <span :class="['truncate text-sm text-gray-900 dark:text-white', ticket.unread ? 'font-semibold' : 'font-medium']">{{ ticket.subject }}</span>
                  </div>
                  <div class="mt-1 pl-4 text-xs text-gray-400">{{ ticket.ticket_no }}</div>
                </td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ t(`tickets.category.${ticket.category}`) }}</td>
                <td class="px-4 py-4"><TicketStatusBadge :status="ticket.status" /></td>
                <td class="px-4 py-4 text-right text-sm text-gray-500 dark:text-dark-400">{{ formatDate(ticket.last_public_message_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700 md:hidden">
          <button v-for="ticket in tickets" :key="ticket.ticket_no" type="button" class="block w-full px-1 py-4 text-left" @click="openTicket(ticket.ticket_no)">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span v-if="ticket.unread" class="h-2 w-2 flex-shrink-0 rounded-full bg-rose-500"></span>
                  <span :class="['truncate text-sm text-gray-900 dark:text-white', ticket.unread ? 'font-semibold' : 'font-medium']">{{ ticket.subject }}</span>
                </div>
                <div class="mt-1 text-xs text-gray-400">{{ ticket.ticket_no }} · {{ t(`tickets.category.${ticket.category}`) }}</div>
              </div>
              <TicketStatusBadge :status="ticket.status" />
            </div>
            <div class="mt-2 text-xs text-gray-500">{{ formatDate(ticket.last_public_message_at) }}</div>
          </button>
        </div>

        <Pagination
          v-if="pagination.total > pagination.page_size"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="changePage"
          @update:page-size="changePageSize"
        />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketStatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { ticketsAPI } from '@/api'
import { useTicketNotificationsStore } from '@/stores'
import type { UserTicket, UserTicketListParams } from '@/types/ticket'

const { t, locale } = useI18n()
const router = useRouter()
const notifications = useTicketNotificationsStore()
const buckets = ['all', 'active', 'waiting_user', 'ended'] as const
const filters = reactive<UserTicketListParams>({ bucket: 'all', search: '', page: 1, page_size: 20 })
const tickets = ref<UserTicket[]>([])
const pagination = reactive({ total: 0, page: 1, page_size: 20, pages: 0 })
const loading = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null
let controller: AbortController | null = null

async function load(): Promise<void> {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  try {
    const result = await ticketsAPI.list(filters, controller.signal)
    tickets.value = result.items
    Object.assign(pagination, result)
  } finally {
    loading.value = false
  }
}

function setBucket(bucket: typeof buckets[number]): void {
  filters.bucket = bucket
  filters.page = 1
  load()
}

function bucketCount(bucket: typeof buckets[number]): number {
  return notifications.userCounts?.[bucket] ?? 0
}

function scheduleSearch(): void {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    filters.page = 1
    load()
  }, 300)
}

function changePage(page: number): void {
  filters.page = page
  load()
}

function changePageSize(pageSize: number): void {
  filters.page_size = pageSize
  filters.page = 1
  load()
}

function openTicket(ticketNo: string): void {
  router.push(`/tickets/${encodeURIComponent(ticketNo)}`)
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

onMounted(load)
onBeforeUnmount(() => {
  controller?.abort()
  if (searchTimer) clearTimeout(searchTimer)
})
</script>
