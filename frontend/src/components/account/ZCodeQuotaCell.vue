<template>
  <div data-test="zcode-quota" class="min-w-0 space-y-1.5 text-xs text-gray-600 dark:text-gray-300">
    <div class="flex flex-wrap items-center gap-2"><span class="font-medium">{{ quota?.plan === 'coding' || account.credentials?.account_mode === 'coding' ? 'Coding Plan' : 'Start Plan' }}</span><button type="button" data-test="zcode-quota-refresh" class="text-primary-600 disabled:opacity-50 dark:text-primary-400" :disabled="loading" @click="refresh">{{ t('admin.accounts.cnProviders.probe') }}</button></div>
    <div v-for="(plan, planIndex) in quota?.plans || []" :key="plan.id || planIndex" data-test="zcode-plan" class="space-y-1.5">
      <div class="flex min-w-0 items-center gap-2">
        <span class="truncate font-medium">{{ plan.name }}</span>
        <span v-if="planStatusLabel(plan)" class="shrink-0 rounded bg-gray-100 px-1 text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ planStatusLabel(plan) }}</span>
        <HelpTooltip v-if="planDetails(plan).length">
          <div v-for="(line, lineIndex) in planDetails(plan)" :key="lineIndex">{{ line }}</div>
        </HelpTooltip>
      </div>
      <div v-if="planSummary(plan)" class="text-gray-500">{{ planSummary(plan) }}</div>
      <div v-if="planPending(plan) && planEffectiveAt(plan)" class="text-gray-500">{{ t('admin.accounts.zcode.effectiveAt') }}: {{ date(planEffectiveAt(plan)) }}</div>
    </div>
    <div v-if="!quota || (!quota.balances.length && !hasScheduledPlan)" class="text-gray-400">{{ t('admin.accounts.zcode.quotaUnknown') }}</div>
    <div v-for="(balance, index) in quota?.balances || []" :key="index" class="space-y-1.5">
      <div class="flex min-w-0 items-center">
        <span class="truncate font-medium">{{ balance.name }}</span>
        <HelpTooltip v-if="detailLines(balance).length">
          <div v-for="(line, lineIndex) in detailLines(balance)" :key="lineIndex">{{ line }}</div>
        </HelpTooltip>
      </div>
      <UsageProgressBar v-if="balance.used_percent != null" :label="windowLabel(balance)" :utilization="balance.used_percent" :resets-at="balance.reset_at ? new Date(balance.reset_at * 1000).toISOString() : undefined" color="indigo" />
      <div v-else-if="balance.remaining != null" data-test="zcode-remaining">{{ remainingLine(balance, true) }}</div>
    </div>
    <div v-if="state?.oauth_status === 'login_required'" class="text-red-600 dark:text-red-400">{{ t('admin.accounts.zcode.loginRequired') }}</div>
    <div v-if="error" class="break-words text-red-600 dark:text-red-400" role="alert">{{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { zcodeAPI, type ZCodeQuota, type ZCodeQuotaPlan } from '@/api/admin/zcode'
import { formatCompactNumber } from '@/utils/format'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import UsageProgressBar from './UsageProgressBar.vue'
const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const quota = ref<ZCodeQuota | null>(null)
const loading = ref(false)
const error = ref('')
const now = ref(Math.floor(Date.now() / 1000))
const hasScheduledPlan = computed(() => (quota.value?.plans || []).some(planPending))
type ZCodeBalance = ZCodeQuota['balances'][number]
// 套餐状态只在需要提示时展示；active 不额外占用行内空间。
function planPending(plan: ZCodeQuotaPlan) {
  const entitlements = plan.entitlements || []
  if (entitlements.length) return entitlements.every(item => (item.effective_at || 0) > now.value)
  return (plan.starts_at || 0) > now.value
}
function planStatusLabel(plan: ZCodeQuotaPlan) {
  if (planPending(plan)) return t('admin.accounts.zcode.planScheduled')
  if (plan.status === 'expired' || (plan.ends_at && plan.ends_at <= now.value)) return t('admin.accounts.zcode.planExpired')
  const quotaExhausted = quota.value?.status === 'exhausted' && !(quota.value.balances || []).some(balance => (balance.remaining ?? 0) > 0)
  if (quotaExhausted) return t('admin.accounts.zcode.planExhausted')
  return ''
}
// 计划的生效时间在 entitlement 上；尚未生效时展示最近一次生效时间。
function planEffectiveAt(plan: ZCodeQuotaPlan) {
  const times = (plan.entitlements || []).map(item => item.effective_at || 0).filter(value => value > now.value)
  if (plan.starts_at && plan.starts_at > now.value) times.push(plan.starts_at)
  return times.length ? Math.min(...times) : 0
}
function planSummary(plan: ZCodeQuotaPlan) {
  return (plan.entitlements || []).map(item => {
    const parts: string[] = []
    if (item.name) parts.push(item.name)
    if (item.units != null) parts.push(`${formatCompactNumber(item.units)}${item.unit ? ` ${item.unit}` : ''}`)
    return parts.join(' · ')
  }).filter(Boolean).join(', ')
}
function planDetails(plan: ZCodeQuotaPlan) {
  const lines: string[] = []
  if (plan.starts_at) lines.push(`${t('admin.accounts.zcode.planStartsAt')}: ${date(plan.starts_at)}`)
  if (plan.ends_at) lines.push(`${t('admin.accounts.zcode.expiresAt')}: ${date(plan.ends_at)}`)
  for (const item of plan.entitlements || []) {
    const parts: string[] = []
    if (item.name) parts.push(item.name)
    if (item.units != null) parts.push(`${formatCompactNumber(item.units)}${item.unit ? ` ${item.unit}` : ''}`)
    if (item.effective_at) parts.push(`${t('admin.accounts.zcode.effectiveAt')}: ${date(item.effective_at)}`)
    if (parts.length) lines.push(parts.join(' · '))
  }
  return lines
}
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
// 到期、生效、重置与更新时间统一收纳于名字旁的问号提示，默认不占版面。
function detailLines(balance: ZCodeBalance) {
  const lines: string[] = []
  if (balance.remaining != null) lines.push(remainingLine(balance))
  if (balance.effective_at && balance.effective_at > now.value) lines.push(`${t('admin.accounts.zcode.effectiveAt')}: ${date(balance.effective_at)}`)
  if (balance.expires_at) lines.push(`${t('admin.accounts.zcode.expiresAt')}: ${date(balance.expires_at)}`)
  if (balance.reset_at) lines.push(`${t('admin.accounts.zcode.resetsAt')}: ${date(balance.reset_at)}`)
  if (quota.value?.updated_at) lines.push(`${t('admin.accounts.zcode.lastUpdated')}: ${date(quota.value.updated_at)}`)
  return lines
}
const state = computed(() => props.account.extra?.zcode_state as { oauth_status?: string } | undefined)
watch(() => [props.account.id, props.account.extra?.zcode_quota], () => { quota.value = (props.account.extra?.zcode_quota as ZCodeQuota | undefined) || null; now.value = Math.floor(Date.now() / 1000); error.value = '' }, { immediate: true })
function date(value: number) { return new Date(value * 1000).toLocaleString() }
function number(value: number) { return value.toLocaleString(undefined, { maximumFractionDigits: 2 }) }
// 挂载只读数据库快照；显式查询仍经过服务端缓存与跨实例限频。
async function refresh() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try { quota.value = await zcodeAPI.quota(props.account.id); now.value = Math.floor(Date.now() / 1000) }
  catch (e: unknown) { error.value = (e as { message?: string }).message || t('common.error') }
  finally { loading.value = false }
}
</script>
