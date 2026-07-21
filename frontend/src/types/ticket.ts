export type TicketStatus = 'open' | 'in_progress' | 'waiting_user' | 'resolved' | 'closed'
export type TicketCategory = 'api_issue' | 'subscription' | 'payment' | 'account' | 'feature_request' | 'other'
export type TicketImpact = 'blocked' | 'degraded' | 'general'
export type TicketPriority = 'urgent' | 'high' | 'normal' | 'low'
export type TicketActionRequired = 'ADMIN' | 'USER' | 'NONE'
export type TicketVisibility = 'public' | 'internal'
export type TicketStorageMode = 'disabled' | 'local' | 's3'

export interface TicketCapabilities {
  enabled: boolean
  attachments_enabled: boolean
  max_file_bytes: number
  max_files_per_message: number
  max_ticket_bytes: number
  polling_hint_seconds: number
  detail_polling_seconds: number
}

export interface UserTicketCounts {
  unread: number
  all: number
  active: number
  waiting_user: number
  ended: number
  open: number
  in_progress: number
  resolved: number
  closed: number
}

export interface AdminTicketCounts {
  action_required: number
  open: number
  in_progress: number
  waiting_user: number
  resolved: number
  closed: number
  ended: number
  all: number
}

export interface TicketBase {
  ticket_no: string
  subject: string
  category: TicketCategory
  impact: TicketImpact
  status: TicketStatus
  action_required: TicketActionRequired
  request_id?: string
  usage_log_id?: number
  api_key_id?: number
  api_key_name?: string
  payment_order_id?: number
  payment_order_no?: string
  user_subscription_id?: number
  subscription_name?: string
  last_public_message_at: string
  action_required_since?: string
  notification_seq: number
  resolved_at?: string
  reopen_deadline?: string
  closed_at?: string
  version: number
  created_at: string
  updated_at: string
}

export interface UserTicket extends TicketBase {
  unread: boolean
}

export interface AdminTicket extends TicketBase {
  user_id?: number
  requester_email: string
  requester_username: string
  priority: TicketPriority
  assignee_id?: number
  last_activity_at: string
}

export interface TicketMessageAttachment {
  id: number
  original_name: string
  content_type: string
  byte_size: number
  created_at: string
}

export interface UserTicketMessage {
  id: number
  author_id?: number
  author_role: 'user' | 'admin' | 'system'
  author_name: string
  body: string
  attachments: TicketMessageAttachment[]
  created_at: string
}

export interface AdminTicketMessage extends UserTicketMessage {
  visibility: TicketVisibility
}

export interface UserTicketEvent {
  id: number
  actor_id?: number
  actor_role: 'user' | 'admin' | 'system'
  event_type: string
  from_status?: TicketStatus
  to_status?: TicketStatus
  payload: Record<string, unknown>
  created_at: string
}

export interface AdminTicketEvent extends UserTicketEvent {
  visibility: TicketVisibility
}

export interface UserTicketDetail {
  ticket: UserTicket
  messages: UserTicketMessage[]
  events: UserTicketEvent[]
}

export interface AdminTicketDetail {
  ticket: AdminTicket
  messages: AdminTicketMessage[]
  events: AdminTicketEvent[]
}

export interface TicketPagination<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface UserTicketListParams {
  page?: number
  page_size?: number
  bucket?: 'all' | 'active' | 'waiting_user' | 'ended'
  status?: TicketStatus | ''
  category?: TicketCategory | ''
  search?: string
  sort_by?: 'last_public_message_at' | 'created_at'
  sort_order?: 'asc' | 'desc'
}

export interface AdminTicketListParams {
  page?: number
  page_size?: number
  bucket?: 'action_required' | 'in_progress' | 'waiting_user' | 'ended' | 'all'
  status?: TicketStatus | ''
  category?: TicketCategory | ''
  impact?: TicketImpact | ''
  priority?: TicketPriority | ''
  assignee_id?: number
  unassigned?: boolean
  search?: string
  created_from?: string
  created_to?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface CreateTicketRequest {
  category: TicketCategory
  impact: TicketImpact
  subject: string
  body: string
  usage_log_id?: number | null
  api_key_id?: number | null
  payment_order_id?: number | null
  user_subscription_id?: number | null
  attachment_tokens: string[]
}

export interface PendingTicketAttachment {
  upload_token: string
  original_name: string
  content_type: string
  byte_size: number
  expires_at?: string
}

export interface TicketStorageS3Input {
  endpoint: string
  region: string
  bucket: string
  access_key_id: string
  secret_access_key: string
  prefix: string
  force_path_style: boolean
}

export interface TicketStorageUpdate {
  mode: TicketStorageMode
  s3: TicketStorageS3Input
}

export interface TicketStorageSettings {
  mode: TicketStorageMode
  attachments_enabled: boolean
  local: {
    configured: boolean
    writable: boolean
    display_path: string
    shared_volume: boolean
  }
  s3: {
    endpoint: string
    region: string
    bucket: string
    access_key_id_masked: string
    secret_configured: boolean
    prefix: string
    force_path_style: boolean
  }
  usage: {
    local: { files: number; bytes: number }
    s3: { files: number; bytes: number }
  }
}
