import { apiClient } from '../client'
import { createTicketIdempotencyKey } from '../tickets'
import type {
  AdminTicket,
  AdminTicketCounts,
  AdminTicketDetail,
  AdminTicketListParams,
  PendingTicketAttachment,
  TicketPagination,
  TicketPriority,
  TicketStorageSettings,
  TicketStorageUpdate,
  TicketVisibility,
} from '@/types/ticket'

function writeHeaders(idempotencyKey?: string) {
  return { 'Idempotency-Key': idempotencyKey || createTicketIdempotencyKey() }
}

export async function getCounts(): Promise<AdminTicketCounts> {
  const { data } = await apiClient.get<AdminTicketCounts>('/admin/tickets/counts')
  return data
}

export async function list(params: AdminTicketListParams = {}, signal?: AbortSignal): Promise<TicketPagination<AdminTicket>> {
  const { data } = await apiClient.get<TicketPagination<AdminTicket>>('/admin/tickets', { params, signal })
  return data
}

export async function get(ticketNo: string, signal?: AbortSignal): Promise<AdminTicketDetail> {
  const { data } = await apiClient.get<AdminTicketDetail>(`/admin/tickets/${encodeURIComponent(ticketNo)}`, { signal })
  return data
}

export async function claim(ticketNo: string, expectedVersion: number, idempotencyKey?: string): Promise<AdminTicketDetail> {
  const { data } = await apiClient.post<AdminTicketDetail>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/claim`,
    { expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function assign(
  ticketNo: string,
  assigneeId: number | null,
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<AdminTicketDetail> {
  const { data } = await apiClient.put<AdminTicketDetail>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/assignee`,
    { assignee_id: assigneeId, expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function changePriority(
  ticketNo: string,
  priority: TicketPriority,
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<AdminTicketDetail> {
  const { data } = await apiClient.put<AdminTicketDetail>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/priority`,
    { priority, expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function sendMessage(
  ticketNo: string,
  request: {
    visibility: TicketVisibility
    body: string
    next_action?: 'wait_user' | 'keep_processing' | 'resolve'
    expected_version: number
    attachment_tokens: string[]
  },
  idempotencyKey?: string,
): Promise<AdminTicketDetail> {
  const { data } = await apiClient.post<AdminTicketDetail>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/messages`,
    request,
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function resolve(ticketNo: string, expectedVersion: number, idempotencyKey?: string): Promise<AdminTicketDetail> {
  const { data } = await apiClient.post<AdminTicketDetail>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/resolve`,
    { expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function reopen(ticketNo: string, expectedVersion: number, idempotencyKey?: string): Promise<void> {
  await apiClient.post(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/reopen`,
    { expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
}

export async function close(
  ticketNo: string,
  reason: string,
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<void> {
  await apiClient.post(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/close`,
    { reason, expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
}

export async function uploadAttachment(file: File): Promise<PendingTicketAttachment> {
  const form = new FormData()
  form.append('file', file)
  const { data } = await apiClient.post<PendingTicketAttachment>('/admin/tickets/attachments', form)
  return data
}

export async function downloadAttachment(ticketNo: string, attachmentId: number): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/admin/tickets/${encodeURIComponent(ticketNo)}/attachments/${attachmentId}`,
    { responseType: 'blob' },
  )
  return data
}

export async function getStorageSettings(): Promise<TicketStorageSettings> {
  const { data } = await apiClient.get<TicketStorageSettings>('/admin/tickets/storage-settings')
  return data
}

export async function testStorageSettings(request: TicketStorageUpdate): Promise<void> {
  await apiClient.post('/admin/tickets/storage-settings/test', request)
}

export async function updateStorageSettings(request: TicketStorageUpdate): Promise<TicketStorageSettings> {
  const { data } = await apiClient.put<TicketStorageSettings>('/admin/tickets/storage-settings', request)
  return data
}

const adminTicketsAPI = {
  getCounts,
  list,
  get,
  claim,
  assign,
  changePriority,
  sendMessage,
  resolve,
  reopen,
  close,
  uploadAttachment,
  downloadAttachment,
  getStorageSettings,
  testStorageSettings,
  updateStorageSettings,
}

export default adminTicketsAPI
