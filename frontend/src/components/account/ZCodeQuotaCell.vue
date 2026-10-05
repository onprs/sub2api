<template>
  <div data-test="zcode-quota" class="min-w-0 space-y-1.5 text-xs text-gray-600 dark:text-gray-300">
    <div class="flex flex-wrap items-center gap-2"><span class="font-medium">{{ quota?.plan === 'coding' || account.credentials?.account_mode === 'coding' ? 'Coding Plan' : 'Start Plan' }}</span><button type="button" data-test="zcode-quota-refresh" class="text-primary-600 disabled:opacity-50 dark:text-primary-400" :disabled="loading" @click="refresh">{{ t('admin.accounts.cnProviders.probe') }}</button></div>
    <div v-if="!quota || !quota.balances.length" class="text-gray-400">{{ t('admin.accounts.zcode.quotaUnknown') }}</div>
    <div v-for="(balance, index) in quota?.balances || []" :key="index" class="space-y-1.5" :title="balance.remaining != null ? remainingLine(balance) : undefined">
      <div class="truncate font-medium">{{ balance.name }}</div>
      <UsageProgressBar v-if="balance.used_percent != null" :label="windowLabel(balance)" :utilization="balance.used_percent" :resets-at="balance.reset_at ? new Date(balance.reset_at * 1000).toISOString() : undefined" color="indigo" />
      <div v-else-if="balance.remaining != null" data-test="zcode-remaining">{{ remainingLine(balance, true) }}</div>
      <div v-if="balance.effective_at && balance.effective_at > now">{{ t('admin.accounts.zcode.effectiveAt') }}: {{ date(balance.effective_at) }}</div>
      <div v-if="balance.expires_at">{{ t('admin.accounts.zcode.expiresAt') }}: {{ date(balance.expires_at) }}</div>
      <div v-if="balance.reset_at && balance.used_percent == null">{{ t('admin.accounts.zcode.resetsAt') }}: {{ date(balance.reset_at) }}</div>
    </div>
    <div v-if="quota?.updated_at" class="text-[10px] text-gray-400">{{ t('admin.accounts.zcode.lastUpdated') }}: {{ date(quota.updated_at) }}</div>
    <div v-if="state?.oauth_status === 'login_required'" class="text-red-600 dark:text-red-400">{{ t('admin.accounts.zcode.loginRequired') }}</div>
    <div v-if="error" class="break-words text-red-600 dark:text-red-400" role="alert">{{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { zcodeAPI, type ZCodeQuota } from '@/api/admin/zcode'
import { formatCompactNumber } from '@/utils/format'
import UsageProgressBar from './UsageProgressBar.vue'
const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const quota = ref<ZCodeQuota | null>(null)
const loading = ref(false)
const error = ref('')
const now = Math.floor(Date.now() / 1000)
type ZCodeBalance = ZCodeQuota['balances'][number]
// 进度条徽章只承载短窗口标签；模型名称已在各自行内展示，避免重复与折行错乱。
function windowLabel(balance: ZCodeBalance) {
  if (balance.window === '5h') return t('admin.accounts.cnProviders.window5h')
  if (balance.window === 'weekly') return t('admin.accounts.cnProviders.windowWeekly')
  return ''
}
// 默认只展示进度条与百分比；精确数值仅在悬浮提示中给出，避免长数字撑破窄列。
function remainingLine(balance: ZCodeBalance, compact = false) {
  const fmt = (value: number) => (compact ? formatCompactNumber(value) : number(value))
  let text = `${t('admin.accounts.zcode.remaining')}: ${fmt(balance.remaining ?? 0)}`
  if (balance.total != null) text += ` / ${fmt(balance.total)}`
  if (balance.unit) text += ` ${balance.unit}`
  return text
}
const state = computed(() => props.account.extra?.zcode_state as { oauth_status?: string } | undefined)
watch(() => [props.account.id, props.account.extra?.zcode_quota], () => { quota.value = (props.account.extra?.zcode_quota as ZCodeQuota | undefined) || null; error.value = '' }, { immediate: true })
function date(value: number) { return new Date(value * 1000).toLocaleString() }
function number(value: number) { return value.toLocaleString(undefined, { maximumFractionDigits: 2 }) }
// 挂载只读数据库快照；显式查询仍经过服务端缓存与跨实例限频。
async function refresh() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try { quota.value = await zcodeAPI.quota(props.account.id) }
  catch (e: unknown) { error.value = (e as { message?: string }).message || t('common.error') }
  finally { loading.value = false }
}
</script>
