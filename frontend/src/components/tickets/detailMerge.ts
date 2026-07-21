import type { AdminTicketDetail, UserTicketDetail } from '@/types/ticket'

export function mergeUserTicketDetail(current: UserTicketDetail | null, incoming: UserTicketDetail): UserTicketDetail {
  return mergeDetail(current, incoming)
}

export function mergeAdminTicketDetail(current: AdminTicketDetail | null, incoming: AdminTicketDetail): AdminTicketDetail {
  return mergeDetail(current, incoming)
}

function mergeDetail<T extends UserTicketDetail | AdminTicketDetail>(current: T | null, incoming: T): T {
  if (!current) return incoming
  const messages = new Map(current.messages.map((item) => [item.id, item]))
  incoming.messages.forEach((item) => messages.set(item.id, item))
  const events = new Map(current.events.map((item) => [item.id, item]))
  incoming.events.forEach((item) => events.set(item.id, item))
  return {
    ...incoming,
    messages: [...messages.values()].sort((left, right) => left.id - right.id),
    events: [...events.values()].sort((left, right) => left.id - right.id),
  } as T
}
