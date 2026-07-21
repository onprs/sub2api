import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTicketNotificationsStore } from '@/stores/ticketNotifications'
import type { TicketCapabilities } from '@/types/ticket'

const getUserCounts = vi.fn()
const getAdminCounts = vi.fn()
const getCapabilities = vi.fn()

vi.mock('@/api', () => ({
  ticketsAPI: {
    getCounts: (...args: unknown[]) => getUserCounts(...args),
    getCapabilities: (...args: unknown[]) => getCapabilities(...args),
  },
  adminAPI: {
    tickets: { getCounts: (...args: unknown[]) => getAdminCounts(...args) },
  },
}))

const userCounts = { unread: 4, all: 7, active: 3, waiting_user: 1, ended: 3, open: 2, in_progress: 1, resolved: 2, closed: 1 }
const adminCounts = { action_required: 6, open: 4, in_progress: 2, waiting_user: 1, resolved: 2, closed: 1, ended: 3, all: 10 }

function setVisibility(value: DocumentVisibilityState): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('ticketNotifications store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.clearAllMocks()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    getUserCounts.mockResolvedValue(userCounts)
    getAdminCounts.mockResolvedValue(adminCounts)
    getCapabilities.mockResolvedValue({ enabled: true, attachments_enabled: false, max_file_bytes: 5, max_files_per_message: 3, max_ticket_bytes: 30, polling_hint_seconds: 30, detail_polling_seconds: 15 })
  })

  afterEach(() => {
    useTicketNotificationsStore().reset()
    vi.useRealTimers()
  })

  it('loads user and admin badges immediately and polls once per interval', async () => {
    const store = useTicketNotificationsStore()
    store.start(true)
    await vi.runAllTicks()
    await Promise.resolve()

    expect(store.userUnread).toBe(4)
    expect(store.adminActionRequired).toBe(6)
    expect(getUserCounts).toHaveBeenCalledTimes(1)
    expect(getAdminCounts).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(30_000)
    expect(getUserCounts).toHaveBeenCalledTimes(2)
    expect(getAdminCounts).toHaveBeenCalledTimes(2)
  })

  it('skips count endpoints when ticketing is disabled and can force-refresh capabilities', async () => {
    getCapabilities.mockResolvedValueOnce({ enabled: false, attachments_enabled: false, max_file_bytes: 5, max_files_per_message: 3, max_ticket_bytes: 30, polling_hint_seconds: 30, detail_polling_seconds: 15 })
    const store = useTicketNotificationsStore()

    await store.refresh(true)
    expect(store.capabilities?.enabled).toBe(false)
    expect(getUserCounts).not.toHaveBeenCalled()
    expect(getAdminCounts).not.toHaveBeenCalled()

    await store.refresh(true, true)
    expect(store.capabilities?.enabled).toBe(true)
    expect(getCapabilities).toHaveBeenCalledTimes(2)
    expect(getUserCounts).toHaveBeenCalledTimes(1)
    expect(getAdminCounts).toHaveBeenCalledTimes(1)
  })

  it('does not restore stale capability data after reset', async () => {
    let resolveCapabilities: ((value: TicketCapabilities) => void) | undefined
    getCapabilities.mockReturnValueOnce(new Promise<TicketCapabilities>((resolve) => { resolveCapabilities = resolve }))
    const store = useTicketNotificationsStore()
    const refresh = store.refresh()

    store.reset()
    resolveCapabilities?.({ enabled: true, attachments_enabled: false, max_file_bytes: 5, max_files_per_message: 3, max_ticket_bytes: 30, polling_hint_seconds: 30, detail_polling_seconds: 15 })
    await refresh

    expect(store.capabilities).toBeNull()
    expect(store.userCounts).toBeNull()
    expect(getUserCounts).not.toHaveBeenCalled()
  })

  it('pauses while hidden, refreshes when visible, and clears on reset', async () => {
    const store = useTicketNotificationsStore()
    store.start(false)
    await vi.runAllTicks()
    await Promise.resolve()

    setVisibility('hidden')
    await vi.advanceTimersByTimeAsync(90_000)
    expect(getUserCounts).toHaveBeenCalledTimes(1)

    setVisibility('visible')
    await vi.runAllTicks()
    await Promise.resolve()
    expect(getUserCounts).toHaveBeenCalledTimes(2)

    store.reset()
    expect(store.userCounts).toBeNull()
    expect(store.adminCounts).toBeNull()
    expect(store.polling).toBe(false)
  })
})
