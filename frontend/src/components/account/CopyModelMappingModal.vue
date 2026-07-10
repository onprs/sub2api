<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.copyModelMapping.title')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <div class="space-y-4">
      <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-700/40 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('admin.accounts.copyModelMapping.warning') }}
      </div>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700/60">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
            {{ t('admin.accounts.copyModelMapping.targetCount') }}
          </div>
          <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ targetAccountIds.length }}
          </div>
        </div>
        <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700/60">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">
            {{ t('admin.accounts.platform') }}
          </div>
          <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ targetPlatform || '-' }}
          </div>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.copyModelMapping.sourceAccount') }}</label>
        <Select
          v-model="selectedSourceId"
          :options="sourceOptions"
          :placeholder="t('admin.accounts.copyModelMapping.sourcePlaceholder')"
          :empty-text="emptyText"
          :disabled="loading || submitting || sourceOptions.length === 0"
          searchable
        />
      </div>

      <div v-if="loading" class="rounded-lg bg-gray-50 p-4 text-center text-sm text-gray-500 dark:bg-dark-700/60 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="sourceAccounts.length === 0" class="rounded-lg bg-gray-50 p-4 text-center text-sm text-gray-500 dark:bg-dark-700/60 dark:text-dark-400">
        {{ t('admin.accounts.copyModelMapping.noSourceAccounts') }}
      </div>
      <div v-else class="max-h-60 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700" data-test="copy-model-mapping-source-list">
        <button
          v-for="source in sourceAccounts"
          :key="source.id"
          type="button"
          class="flex w-full items-center justify-between gap-3 border-b border-gray-100 px-3 py-2 text-left last:border-b-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60"
          :class="selectedSourceId === source.id ? 'bg-primary-50 dark:bg-primary-900/20' : ''"
          @click="selectedSourceId = source.id"
        >
          <div class="min-w-0">
            <div class="truncate text-sm font-medium text-gray-900 dark:text-white">
              {{ source.name || `#${source.id}` }}
            </div>
            <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
              #{{ source.id }} - {{ source.platform }}
            </div>
          </div>
          <div class="flex flex-shrink-0 items-center gap-2 text-xs text-gray-500 dark:text-dark-300">
            <span>{{ t('admin.accounts.copyModelMapping.mappingCount', { count: source.mappingCount }) }}</span>
            <Icon v-if="selectedSourceId === source.id" name="check" size="sm" class="text-primary-500" />
          </div>
        </button>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          data-test="copy-model-mapping-submit"
          :disabled="loading || submitting || !selectedSourceId || targetAccountIds.length === 0"
          @click="handleSubmit"
        >
          {{ submitting ? t('admin.accounts.copyModelMapping.copying') : t('admin.accounts.copyModelMapping.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { CopyModelMappingResult } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import type { Account, AccountPlatform } from '@/types'

interface Props {
  show: boolean
  targetAccountIds: number[]
  targetPlatform: AccountPlatform | string | null
}

interface SourceAccount extends Account {
  mappingCount: number
}

interface Emits {
  (e: 'close'): void
  (e: 'copied', result: CopyModelMappingResult): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const submitting = ref(false)
const selectedSourceId = ref<number | null>(null)
const sourceAccounts = ref<SourceAccount[]>([])

const targetIDSet = computed(() => new Set(props.targetAccountIds))
const emptyText = computed(() =>
  loading.value
    ? t('common.loading')
    : t('admin.accounts.copyModelMapping.noSourceAccounts')
)

const sourceOptions = computed<SelectOption[]>(() =>
  sourceAccounts.value.map((account) => ({
    value: account.id,
    label: `${account.name || `#${account.id}`} (#${account.id}) - ${t('admin.accounts.copyModelMapping.mappingCount', { count: account.mappingCount })}`
  }))
)

const getModelMappingCount = (account: Account) => {
  const mapping = account.credentials?.model_mapping
  if (!mapping || typeof mapping !== 'object' || Array.isArray(mapping)) {
    return 0
  }
  return Object.entries(mapping as Record<string, unknown>).filter(
    ([key, value]) => key.trim() !== '' && typeof value === 'string' && value.trim() !== ''
  ).length
}

const resetState = () => {
  loading.value = false
  submitting.value = false
  selectedSourceId.value = null
  sourceAccounts.value = []
}

const loadSourceAccounts = async () => {
  resetState()
  if (!props.show || !props.targetPlatform) {
    return
  }

  loading.value = true
  try {
    const response = await adminAPI.accounts.list(1, 500, { platform: props.targetPlatform })
    sourceAccounts.value = response.items
      .filter((account) => account.platform === props.targetPlatform)
      .filter((account) => !targetIDSet.value.has(account.id))
      .map((account) => ({
        ...account,
        mappingCount: getModelMappingCount(account)
      }))
      .filter((account) => account.mappingCount > 0)
      .sort((a, b) => (a.name || '').localeCompare(b.name || '') || a.id - b.id)
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.accounts.copyModelMapping.loadFailed'))
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.show, props.targetPlatform, props.targetAccountIds.join(',')],
  () => {
    if (props.show) {
      loadSourceAccounts()
    } else {
      resetState()
    }
  },
  { immediate: true }
)

const handleClose = () => {
  if (submitting.value) return
  emit('close')
}

const handleSubmit = async () => {
  if (!selectedSourceId.value) {
    appStore.showError(t('admin.accounts.copyModelMapping.emptySourceError'))
    return
  }
  if (props.targetAccountIds.length === 0) {
    appStore.showError(t('admin.accounts.copyModelMapping.noSelection'))
    return
  }

  submitting.value = true
  try {
    const result = await adminAPI.accounts.copyModelMapping({
      source_account_id: selectedSourceId.value,
      target_account_ids: [...props.targetAccountIds]
    })
    emit('copied', result)
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.accounts.copyModelMapping.failed'))
  } finally {
    submitting.value = false
  }
}
</script>
