import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post, put } }))

import ticketsAPI from '@/api/tickets'
import adminTicketsAPI from '@/api/admin/tickets'

beforeEach(() => {
  get.mockReset(); post.mockReset(); put.mockReset()
  get.mockResolvedValue({ data: {} }); post.mockResolvedValue({ data: {} }); put.mockResolvedValue({ data: {} })
})

describe('ticket APIs', () => {
  it('reuses the caller idempotency key for a user reply intent', async () => {
    await ticketsAPI.reply('TK-123', { body: 'details', expected_version: 3, attachment_tokens: ['upload'] }, 'intent-key')
    expect(post).toHaveBeenCalledWith('/tickets/TK-123/messages', {
      body: 'details', expected_version: 3, attachment_tokens: ['upload'],
    }, { headers: { 'Idempotency-Key': 'intent-key' } })
  })

  it('sends internal admin notes without a next action', async () => {
    await adminTicketsAPI.sendMessage('TK-123', {
      visibility: 'internal', body: 'private', expected_version: 4, attachment_tokens: [],
    }, 'note-key')
    expect(post).toHaveBeenCalledWith('/admin/tickets/TK-123/messages', {
      visibility: 'internal', body: 'private', expected_version: 4, attachment_tokens: [],
    }, { headers: { 'Idempotency-Key': 'note-key' } })
  })

  it('lets FormData set multipart boundaries for user and admin uploads', async () => {
    const file = new File(['evidence'], 'evidence.txt', { type: 'text/plain' })

    await ticketsAPI.uploadAttachment(file)
    await adminTicketsAPI.uploadAttachment(file)

    expect(post).toHaveBeenNthCalledWith(1, '/tickets/attachments', expect.any(FormData))
    expect(post).toHaveBeenNthCalledWith(2, '/admin/tickets/attachments', expect.any(FormData))
    expect(post.mock.calls[0]).toHaveLength(2)
    expect(post.mock.calls[1]).toHaveLength(2)
  })

  it('keeps storage test and save as separate endpoints', async () => {
    const request = {
      mode: 'local' as const,
      s3: { endpoint: '', region: 'auto', bucket: '', access_key_id: '', secret_access_key: '', prefix: '', force_path_style: false },
    }
    await adminTicketsAPI.testStorageSettings(request)
    await adminTicketsAPI.updateStorageSettings(request)
    expect(post).toHaveBeenCalledWith('/admin/tickets/storage-settings/test', request)
    expect(put).toHaveBeenCalledWith('/admin/tickets/storage-settings', request)
  })
})
