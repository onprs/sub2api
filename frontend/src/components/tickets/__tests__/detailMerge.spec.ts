import { describe, expect, it } from 'vitest'
import { mergeUserTicketDetail } from '../detailMerge'
import type { UserTicketDetail } from '@/types/ticket'

function detail(messageIds: number[], eventIds: number[], version: number): UserTicketDetail {
  return {
    ticket: {
      ticket_no: 'TK-1', subject: 'subject', category: 'other', impact: 'general', status: 'open',
      action_required: 'ADMIN', last_public_message_at: '2026-07-21T00:00:00Z', notification_seq: 0,
      version, created_at: '2026-07-21T00:00:00Z', updated_at: '2026-07-21T00:00:00Z', unread: false,
    },
    messages: messageIds.map((id) => ({ id, author_role: 'user', author_name: 'user', body: `message-${id}`, attachments: [], created_at: `2026-07-21T00:0${id}:00Z` })),
    events: eventIds.map((id) => ({ id, actor_role: 'system', event_type: 'status_changed', payload: {}, created_at: `2026-07-21T00:0${id}:30Z` })),
  }
}

describe('mergeUserTicketDetail', () => {
  it('merges incremental refreshes without duplicating messages or events', () => {
    const current = detail([1, 2], [1], 2)
    const incoming = detail([2, 3], [1, 2], 3)
    incoming.messages[0].body = 'message-2-updated'

    const merged = mergeUserTicketDetail(current, incoming)

    expect(merged.ticket.version).toBe(3)
    expect(merged.messages.map((item) => item.id)).toEqual([1, 2, 3])
    expect(merged.messages.find((item) => item.id === 2)?.body).toBe('message-2-updated')
    expect(merged.events.map((item) => item.id)).toEqual([1, 2])
  })
})
