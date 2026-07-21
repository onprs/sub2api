import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
}))

import TicketConversation from '../TicketConversation.vue'

const publicMessage = {
  id: 1,
  author_role: 'admin' as const,
  author_name: 'Admin',
  body: 'Public reply',
  visibility: 'public' as const,
  attachments: [{ id: 8, original_name: 'evidence.txt', content_type: 'text/plain', byte_size: 12, created_at: '2026-07-21T00:00:00Z' }],
  created_at: '2026-07-21T00:00:00Z',
}
const internalMessage = {
  ...publicMessage,
  id: 2,
  body: 'Private note',
  visibility: 'internal' as const,
  attachments: [],
  created_at: '2026-07-21T00:01:00Z',
}

function mountConversation(showInternal = false) {
  return mount(TicketConversation, {
    props: {
      messages: [publicMessage, internalMessage],
      events: [
        { id: 1, actor_role: 'system' as const, event_type: 'ticket_created', payload: {}, visibility: 'public' as const, created_at: '2026-07-20T23:59:00Z' },
        { id: 2, actor_role: 'admin' as const, event_type: 'priority_changed', payload: {}, visibility: 'internal' as const, created_at: '2026-07-21T00:02:00Z' },
      ],
      showInternal,
    },
  })
}

describe('TicketConversation', () => {
  it('hides internal events by default and exposes them to admin mode', () => {
    const user = mountConversation(false)
    expect(user.text()).not.toContain('priority_changed')
    expect(user.text()).not.toContain('Private note')

    const admin = mountConversation(true)
    expect(admin.text()).toContain('priority_changed')
    expect(admin.text()).toContain('Private note')
    expect(admin.text()).toContain('admin.tickets.detail.internalOnly')
  })

  it('keeps a message before its same-timestamp state event', () => {
    const wrapper = mount(TicketConversation, {
      props: {
        messages: [publicMessage],
        events: [{ id: 3, actor_role: 'admin' as const, event_type: 'status_changed', payload: {}, visibility: 'public' as const, created_at: publicMessage.created_at }],
        showInternal: false,
      },
    })

    expect(wrapper.text().indexOf('Public reply')).toBeLessThan(wrapper.text().indexOf('status_changed'))
  })

  it('emits the safe attachment metadata when download is requested', async () => {
    const wrapper = mountConversation()
    await wrapper.get('button[title="evidence.txt"]').trigger('click')
    expect(wrapper.emitted('download')?.[0]?.[0]).toEqual(publicMessage.attachments[0])
  })
})
