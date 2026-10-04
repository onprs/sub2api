import type { ZCodePlan, ZCodeProvider } from '@/api/admin/zcode'
import type { Account } from '@/types'

export interface ZCodeForm {
  provider: ZCodeProvider
  plan: ZCodePlan
  autoClaim: boolean
  sessionId: string
  ready: boolean
}
export const isZCodeAccount = (account?: Account | null) =>
  account?.platform === 'zhipu' && account.type === 'oauth' && account.credentials?.auth_mode === 'zcode_oauth'
export function defaultZCodeForm(account?: Account | null): ZCodeForm {
  return {
    provider: account?.credentials?.zcode_provider === 'bigmodel' ? 'bigmodel' : 'zai',
    plan: account?.credentials?.account_mode === 'coding' ? 'coding' : 'start',
    autoClaim: account?.credentials?.zcode_auto_claim === true,
    sessionId: '',
    ready: false
  }
}
// 前端只提交会话 ID 和非敏感设置，服务端消费授权会话并写入密文。
export function zcodeCredentials(form: ZCodeForm): Record<string, unknown> {
  const output: Record<string, unknown> = {
    auth_mode: 'zcode_oauth', account_mode: form.plan, zcode_provider: form.provider,
    zcode_auto_claim: form.autoClaim, api_protocol: 'anthropic'
  }
  if (form.ready && form.sessionId) output.zcode_oauth_session_id = form.sessionId
  return output
}
