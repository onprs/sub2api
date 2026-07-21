import { apiClient } from './client'
import type {
  CreateTicketRequest,
  PendingTicketAttachment,
  TicketCapabilities,
  TicketPagination,
  UserTicket,
  UserTicketCounts,
  UserTicketDetail,
  UserTicketListParams,
} from '@/types/ticket'

export function createTicketIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}-${Math.random().toString(16).slice(2)}`
}

function writeHeaders(idempotencyKey?: string) {
  return { 'Idempotency-Key': idempotencyKey || createTicketIdempotencyKey() }
}

export async function getCapabilities(): Promise<TicketCapabilities> {
  const { data } = await apiClient.get<TicketCapabilities>('/tickets/capabilities')
  return data
}

export async function getCounts(): Promise<UserTicketCounts> {
  const { data } = await apiClient.get<UserTicketCounts>('/tickets/counts')
  return data
}

export async function list(params: UserTicketListParams = {}, signal?: AbortSignal): Promise<TicketPagination<UserTicket>> {
  const { data } = await apiClient.get<TicketPagination<UserTicket>>('/tickets', { params, signal })
  return data
}

export async function get(ticketNo: string, signal?: AbortSignal): Promise<UserTicketDetail> {
  const { data } = await apiClient.get<UserTicketDetail>(`/tickets/${encodeURIComponent(ticketNo)}`, { signal })
  return data
}

export async function create(request: CreateTicketRequest, idempotencyKey?: string): Promise<UserTicketDetail> {
  const { data } = await apiClient.post<UserTicketDetail>('/tickets', request, { headers: writeHeaders(idempotencyKey) })
  return data
}

export async function reply(
  ticketNo: string,
  request: { body: string; expected_version: number; attachment_tokens: string[] },
  idempotencyKey?: string,
): Promise<UserTicketDetail> {
  const { data } = await apiClient.post<UserTicketDetail>(
    `/tickets/${encodeURIComponent(ticketNo)}/messages`,
    request,
    { headers: writeHeaders(idempotencyKey) },
  )
  return data
}

export async function close(ticketNo: string, expectedVersion: number, idempotencyKey?: string): Promise<void> {
  await apiClient.post(
    `/tickets/${encodeURIComponent(ticketNo)}/close`,
    { expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
}

export async function reopen(
  ticketNo: string,
  body: string,
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<void> {
  await apiClient.post(
    `/tickets/${encodeURIComponent(ticketNo)}/reopen`,
    { body, expected_version: expectedVersion },
    { headers: writeHeaders(idempotencyKey) },
  )
}

export async function markRead(ticketNo: string, observedNotificationSeq: number): Promise<void> {
  await apiClient.post(`/tickets/${encodeURIComponent(ticketNo)}/read`, {
    observed_notification_seq: observedNotificationSeq,
  })
}

export async function uploadAttachment(file: File): Promise<PendingTicketAttachment> {
  const form = new FormData()
  form.append('file', file)
  const { data } = await apiClient.post<PendingTicketAttachment>('/tickets/attachments', form)
  return data
}

export async function downloadAttachment(ticketNo: string, attachmentId: number): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/tickets/${encodeURIComponent(ticketNo)}/attachments/${attachmentId}`,
    { responseType: 'blob' },
  )
  return data
}

const ticketsAPI = {
  getCapabilities,
  getCounts,
  list,
  get,
  create,
  reply,
  close,
  reopen,
  markRead,
  uploadAttachment,
  downloadAttachment,
}

export default ticketsAPI
