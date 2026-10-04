<template>
  <div data-test="zcode-oauth-panel" class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <div class="grid gap-4 sm:grid-cols-2">
      <div><label class="input-label">{{ t('admin.accounts.zcode.provider') }}</label><select :value="modelValue.provider" class="input" @change="changeProvider"><option value="zai">Z.AI</option><option value="bigmodel">BigModel</option></select></div>
      <div><label class="input-label">{{ t('admin.accounts.zcode.plan') }}</label><select :value="modelValue.plan" class="input" @change="changePlan"><option value="start">Start Plan</option><option value="coding">Coding Plan</option></select></div>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <button type="button" data-test="zcode-login" class="btn btn-primary" :disabled="busy" @click="startLogin">{{ account ? t('admin.accounts.zcode.relogin') : t('admin.accounts.zcode.login') }}</button>
      <a v-if="session?.authorize_url && session.status !== 'ready'" :href="session.authorize_url" target="_blank" rel="noopener noreferrer" class="break-words text-sm text-primary-600 dark:text-primary-400">{{ t('admin.accounts.zcode.openAuthorization') }}</a>
      <span class="text-sm text-gray-600 dark:text-gray-300" role="status">{{ oauthStatus }}</span>
    </div>
    <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error === 'expired' ? t('admin.accounts.zcode.expired') : error }}</p>
    <label v-if="modelValue.plan === 'start'" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200"><input type="checkbox" data-test="zcode-auto-claim" :checked="modelValue.autoClaim" @change="emit('update:modelValue', { ...modelValue, autoClaim: ($event.target as HTMLInputElement).checked })" />{{ t('admin.accounts.zcode.autoClaim') }}</label>
    <p v-if="modelValue.plan === 'start'" class="input-hint">{{ t('admin.accounts.zcode.autoClaimHint') }}</p>
    <template v-if="account">
      <ZCodeQuotaCell :account="account" />
      <div class="space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <p>{{ t('admin.accounts.zcode.claimStatus') }}: {{ claimLabel(claimState?.result) }}</p>
        <p v-if="claimState?.campaign">{{ t('admin.accounts.zcode.campaign') }}: {{ claimState.campaign.name }}</p>
        <p v-if="claimState?.plan">{{ t('admin.accounts.zcode.claimedPlan') }}: {{ claimState.plan.show_name }}<span v-if="claimState.plan.starts_at"> · {{ t('admin.accounts.zcode.effectiveAt') }} {{ date(claimState.plan.starts_at) }}</span></p>
        <p v-if="claimState?.checked_at">{{ t('admin.accounts.zcode.lastChecked') }}: {{ date(claimState.checked_at) }}</p>
        <p v-if="claimState?.claimed_at">{{ t('admin.accounts.zcode.lastClaimed') }}: {{ date(claimState.claimed_at) }}</p>
        <p v-if="claimState?.next_attempt">{{ t('admin.accounts.zcode.nextAttempt') }}: {{ date(claimState.next_attempt) }}</p>
        <p v-if="claimState?.last_error" class="text-red-600 dark:text-red-400">{{ t('admin.accounts.zcode.lastError') }}: {{ claimLabel(claimState.last_error) }}</p>
      </div>
      <div v-if="modelValue.plan === 'start'" class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary" :disabled="claimBusy" @click="checkClaim(false)">{{ t('admin.accounts.zcode.preview') }}</button><button type="button" data-test="zcode-claim" class="btn btn-secondary" :disabled="claimBusy" @click="checkClaim(true)">{{ t('admin.accounts.zcode.claim') }}</button></div>
      <p v-if="claimError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ claimError }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { zcodeAPI, type ZCodeClaimState, type ZCodePlan, type ZCodeProvider } from '@/api/admin/zcode'
import { useZCodeOAuth } from '@/composables/useZCodeOAuth'
import type { ZCodeForm } from './zcodeForm'
import ZCodeQuotaCell from './ZCodeQuotaCell.vue'

const props = defineProps<{ modelValue: ZCodeForm; proxyId?: number | null; account?: Account | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: ZCodeForm] }>()
const { t, te } = useI18n()
const { session, busy, error, start, reset } = useZCodeOAuth(result => emit('update:modelValue', { ...props.modelValue, sessionId: result.session_id, ready: true }))
const existingState = computed(() => props.account?.extra?.zcode_state as { oauth_status?: string } | undefined)
const oauthStatus = computed(() => {
  if (props.modelValue.ready) return t('admin.accounts.zcode.authorized')
  if (busy.value) return t('admin.accounts.zcode.waiting')
  if (existingState.value?.oauth_status === 'login_required') return t('admin.accounts.zcode.loginRequired')
  if (props.account) return t('admin.accounts.zcode.authorized')
  return t('admin.accounts.zcode.notAuthorized')
})
function invalidate(value: Partial<ZCodeForm>) { reset(); emit('update:modelValue', { ...props.modelValue, ...value, sessionId: '', ready: false }) }
function changeProvider(event: Event) { invalidate({ provider: (event.target as HTMLSelectElement).value as ZCodeProvider }) }
function changePlan(event: Event) { invalidate({ plan: (event.target as HTMLSelectElement).value as ZCodePlan }) }
function startLogin() { void start({ provider: props.modelValue.provider, plan: props.modelValue.plan, proxy_id: props.proxyId, account_id: props.account?.id }) }
watch(() => props.proxyId, () => invalidate({}))
const claimState = ref<ZCodeClaimState | null>(null)
watch(() => props.account, account => { claimState.value = (account?.extra?.zcode_claim as ZCodeClaimState | undefined) || null }, { immediate: true })
const claimBusy = ref(false)
const claimError = ref('')
function date(value: number) { return new Date(value * 1000).toLocaleString() }
function claimLabel(value?: string) { const key = `admin.accounts.zcode.results.${value || 'unknown'}`; return te(key) ? t(key) : t('admin.accounts.zcode.results.unknown') }
async function checkClaim(execute: boolean) {
  if (!props.account || claimBusy.value) return
  claimBusy.value = true
  claimError.value = ''
  try { claimState.value = await (execute ? zcodeAPI.claim(props.account.id) : zcodeAPI.preview(props.account.id)) }
  catch (e: unknown) { claimError.value = (e as { message?: string }).message || t('common.error') }
  finally { claimBusy.value = false }
}
</script>
