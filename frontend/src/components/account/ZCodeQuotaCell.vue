<template>
  <div data-test="zcode-quota" class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
    <div class="flex flex-wrap items-center gap-2"><span class="font-medium">{{ planLabel }}</span><button type="button" data-test="zcode-quota-refresh" class="text-primary-600 disabled:opacity-50 dark:text-primary-400" :disabled="loading" @click="refresh">{{ t('admin.accounts.cnProviders.probe') }}</button></div>
    <!-- 计划行只保留名称与状态；权益明细、生效与到期时间收纳到信息提示。 -->
    <div v-for="(plan, planIndex) in quota?.plans || []" :key="plan.id || planIndex" data-test="zcode-plan" class="flex min-w-0 items-center gap-1.5">
      <span class="truncate font-medium">{{ plan.name }}</span>
      <span v-if="planStatusLabel(plan)" class="shrink-0 rounded bg-gray-100 px-1 text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ planStatusLabel(plan) }}</span>
      <HelpTooltip v-if="planDetails(plan).length">
        <div v-for="(line, lineIndex) in planDetails(plan)" :key="lineIndex">{{ line }}</div>
      </HelpTooltip>
    </div>
    <div v-if="!quota || (!quota.balances.length && !hasScheduledPlan)" class="text-gray-400">{{ t('admin.accounts.zcode.quotaUnknown') }}</div>
    <!-- 余额行一行呈现模型名、进度条与百分比；额度数值与各类时间都在信息提示中。 -->
    <div v-for="(balance, index) in quota?.balances || []" :key="index" data-test="zcode-balance" class="flex min-w-0 items-center gap-1.5">
      <span class="truncate font-medium">{{ balance.name }}</span>
      <span v-if="windowLabel(balance)" class="shrink-0 text-[10px] text-gray-400">{{ windowLabel(balance) }}</span>
      <UsageProgressBar v-if="balance.used_percent != null" data-test="zcode-usage" class="shrink-0" label="" :utilization="balance.used_percent" color="indigo" />
      <span v-else-if="balance.remaining != null" data-test="zcode-remaining" class="shrink-0 text-[10px] text-gray-500">{{ remainingCompact(balance) }}</span>
      <HelpTooltip v-if="detailLines(balance).length">
        <div v-for="(line, lineIndex) in detailLines(balance)" :key="lineIndex">{{ line }}</div>
      </HelpTooltip>
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
const planLabel = computed(() => quota.value?.plan === 'coding' || props.account.credentials?.account_mode === 'coding' ? 'Coding Plan' : 'Start Plan')
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
// Coding Plan 的多个窗口共用同一名称，窗口标签用于区分同名的余额行。
function windowLabel(balance: ZCodeBalance) {
  if (balance.window === '5h') return t('admin.accounts.cnProviders.window5h')
  if (balance.window === 'weekly') return t('admin.accounts.cnProviders.windowWeekly')
  return ''
}
// 无百分比时（如剩余积分）用紧凑数值代替，精确额度与时间仍在悬浮提示中。
function remainingCompact(balance: ZCodeBalance) {
  const value = formatCompactNumber(balance.remaining ?? 0)
  return balance.unit ? `${value} ${balance.unit}` : value
}
function remainingLine(balance: ZCodeBalance) {
  let text = `${t('admin.accounts.zcode.remaining')}: ${number(balance.remaining ?? 0)}`
  if (balance.total != null) text += ` / ${number(balance.total)}`
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
