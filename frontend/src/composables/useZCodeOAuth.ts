import { onBeforeUnmount, ref } from 'vue'
import { zcodeAPI, type ZCodeLogin, type ZCodePlan, type ZCodeProvider } from '@/api/admin/zcode'

export function useZCodeOAuth(onReady: (session: ZCodeLogin) => void) {
  const session = ref<ZCodeLogin | null>(null)
  const busy = ref(false)
  const error = ref('')
  let generation = 0
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  const reset = () => {
    generation++
    controller?.abort()
    if (timer) clearTimeout(timer)
    timer = undefined
    session.value = null
    busy.value = false
    error.value = ''
  }
  async function start(input: { provider: ZCodeProvider; plan: ZCodePlan; proxy_id?: number | null; account_id?: number }) {
    reset()
    const current = generation
    controller = new AbortController()
    busy.value = true
    try {
      const result = await zcodeAPI.start(input, controller.signal)
      if (current !== generation) return
      session.value = result
      const poll = async () => {
        if (current !== generation) return
        if (Date.now() >= result.expires_at * 1000) { busy.value = false; error.value = 'expired'; return }
        try {
          const status = await zcodeAPI.poll(result.session_id, controller?.signal)
          if (current !== generation) return
          session.value = { ...status, authorize_url: result.authorize_url }
          if (status.status === 'ready') { busy.value = false; onReady(status); return }
        } catch (e: unknown) {
          if (current !== generation) return
          const value = e as { status?: number; message?: string }
          if (value.status && value.status < 500 && value.status !== 429) { busy.value = false; error.value = value.message || 'failed'; return }
        }
        timer = setTimeout(poll, Math.max(1, result.poll_interval) * 1000)
      }
      timer = setTimeout(poll, Math.max(1, result.poll_interval) * 1000)
    } catch (e: unknown) {
      if (current !== generation) return
      error.value = (e as { message?: string }).message || 'failed'
      busy.value = false
    }
  }
  onBeforeUnmount(reset)
  return { session, busy, error, start, reset }
}
