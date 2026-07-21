import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { ticketsAPI, adminAPI } from '@/api'
import type { AdminTicketCounts, TicketCapabilities, UserTicketCounts } from '@/types/ticket'

const DEFAULT_POLLING_SECONDS = 30

export const useTicketNotificationsStore = defineStore('ticketNotifications', () => {
  const userCounts = ref<UserTicketCounts | null>(null)
  const adminCounts = ref<AdminTicketCounts | null>(null)
  const capabilities = ref<TicketCapabilities | null>(null)
  const loading = ref(false)
  const polling = ref(false)
  let timer: ReturnType<typeof setInterval> | null = null
  let activeRefresh: Promise<void> | null = null
  let generation = 0
  let adminMode = false

  const userUnread = computed(() => userCounts.value?.unread ?? 0)
  const adminActionRequired = computed(() => adminCounts.value?.action_required ?? 0)

  function refresh(isAdmin = adminMode, forceCapabilities = false): Promise<void> {
    if (document.visibilityState === 'hidden') return Promise.resolve()
    if (activeRefresh) {
      return forceCapabilities
        ? activeRefresh.then(() => refresh(isAdmin, true))
        : activeRefresh
    }

    loading.value = true
    const refreshGeneration = generation
    const task = (async () => {
      if (!capabilities.value || forceCapabilities) {
        const nextCapabilities = await ticketsAPI.getCapabilities()
        if (refreshGeneration !== generation) return
        capabilities.value = nextCapabilities
      }
      if (!capabilities.value.enabled) {
        userCounts.value = null
        adminCounts.value = null
        return
      }
      const requests: Promise<unknown>[] = [
        ticketsAPI.getCounts().then((counts) => {
          if (refreshGeneration === generation) userCounts.value = counts
        }),
      ]
      if (isAdmin) {
        requests.push(adminAPI.tickets.getCounts().then((counts) => {
          if (refreshGeneration === generation) adminCounts.value = counts
        }))
      }
      await Promise.all(requests)
    })().finally(() => {
      if (activeRefresh === task) {
        activeRefresh = null
        loading.value = false
      }
    })
    activeRefresh = task
    return task
  }

  function schedule(): void {
    if (timer) clearInterval(timer)
    if (!polling.value || document.visibilityState === 'hidden') {
      timer = null
      return
    }
    const seconds = capabilities.value?.polling_hint_seconds || DEFAULT_POLLING_SECONDS
    timer = setInterval(() => {
      refresh().catch(() => undefined)
    }, seconds * 1000)
  }

  function handleVisibility(): void {
    if (document.visibilityState === 'visible') {
      refresh().catch(() => undefined).finally(schedule)
    } else {
      schedule()
    }
  }

  function start(isAdmin: boolean): void {
    stop()
    adminMode = isAdmin
    polling.value = true
    document.addEventListener('visibilitychange', handleVisibility)
    refresh(isAdmin).catch(() => undefined).finally(schedule)
  }

  function stop(): void {
    polling.value = false
    if (timer) clearInterval(timer)
    timer = null
    document.removeEventListener('visibilitychange', handleVisibility)
  }

  function reset(): void {
    stop()
    generation++
    activeRefresh = null
    userCounts.value = null
    adminCounts.value = null
    capabilities.value = null
    loading.value = false
    adminMode = false
  }

  return {
    userCounts,
    adminCounts,
    capabilities,
    loading,
    polling,
    userUnread,
    adminActionRequired,
    refresh,
    start,
    stop,
    reset,
  }
})
