import { apiClient } from '../client'

export type ZCodeProvider = 'zai' | 'bigmodel'
export type ZCodePlan = 'start' | 'coding'
export interface ZCodeLogin {
  session_id: string
  authorize_url?: string
  status: 'pending' | 'ready'
  expires_at: number
  poll_interval: number
  plan: ZCodePlan
}
export interface ZCodeBalance {
  name: string
  window?: string
  remaining?: number
  total?: number
  used?: number
  used_percent?: number
  unit?: string
  expires_at?: number
  reset_at?: number
  effective_at?: number
}
export interface ZCodeQuota { plan: ZCodePlan; balances: ZCodeBalance[]; updated_at: number }
export interface ZCodeClaimState {
  checked_at: number
  claimed_at?: number
  result: string
  campaign?: { id: string; name: string; starts_at: number; ends_at: number; min_app_version: string }
  plan?: { id: string; show_name: string; starts_at: number; ends_at: number }
  last_error?: string
  next_attempt: number
}
export const zcodeAPI = {
  async start(input: { provider: ZCodeProvider; plan: ZCodePlan; proxy_id?: number | null; account_id?: number }, signal?: AbortSignal) {
    return (await apiClient.post<ZCodeLogin>('/admin/zhipu/oauth/start', input, { signal })).data
  },
  async poll(session_id: string, signal?: AbortSignal) {
    return (await apiClient.post<ZCodeLogin>('/admin/zhipu/oauth/poll', { session_id }, { signal })).data
  },
  async quota(id: number) { return (await apiClient.get<ZCodeQuota>(`/admin/accounts/${id}/zcode/quota`)).data },
  async preview(id: number) { return (await apiClient.get<ZCodeClaimState>(`/admin/accounts/${id}/zcode/claim/preview`)).data },
  async claim(id: number) { return (await apiClient.post<ZCodeClaimState>(`/admin/accounts/${id}/zcode/claim`)).data }
}
