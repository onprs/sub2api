<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">
            {{ user.email.charAt(0).toUpperCase() }}
          </span>
        </div>
        <div>
          <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <Icon name="refresh" size="xl" class="animate-spin text-primary-500" />
      </div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center">
        <p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p>
      </div>
      <div v-else class="max-h-96 space-y-3 overflow-y-auto">
        <div
          v-for="key in apiKeys"
          :key="key.id"
          class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="flex items-start justify-between">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span>
                <span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">
                  {{ apiKeyStatusLabel(key.status) }}
                </span>
              </div>
              <p class="truncate font-mono text-sm text-gray-500">
                {{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}
              </p>
            </div>
          </div>

          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                class="-mx-1 -my-0.5 flex min-h-7 cursor-pointer items-center gap-1.5 rounded-md px-1 py-0.5 transition-colors hover:bg-gray-100 disabled:cursor-wait dark:hover:bg-dark-700"
                :disabled="updatingKeyIds.has(key.id)"
                :title="t('keys.routing.edit')"
                @click="openRoutingEditor(key)"
              >
                <GroupBadge
                  v-if="routingPrimaryGroup(key)"
                  :name="routingPrimaryGroup(key)!.name"
                  :platform="routingPrimaryGroup(key)!.platform"
                  :subscription-type="routingPrimaryGroup(key)!.subscription_type"
                  :rate-multiplier="routingPrimaryGroup(key)!.rate_multiplier"
                  :peak-rate-enabled="routingPrimaryGroup(key)!.peak_rate_enabled"
                  :peak-start="routingPrimaryGroup(key)!.peak_start"
                  :peak-end="routingPrimaryGroup(key)!.peak_end"
                  :peak-rate-multiplier="routingPrimaryGroup(key)!.peak_rate_multiplier"
                />
                <span v-else class="italic text-gray-400">{{ t('admin.users.none') }}</span>
                <span
                  v-if="routingGroupCount(key) > 1"
                  class="inline-flex h-5 min-w-5 items-center justify-center rounded bg-gray-100 px-1 text-[11px] font-semibold tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                >
                  +{{ routingGroupCount(key) - 1 }}
                </span>
                <Icon
                  v-if="updatingKeyIds.has(key.id)"
                  name="refresh"
                  size="xs"
                  class="animate-spin text-primary-500"
                />
                <Icon v-else name="cog" size="xs" class="text-gray-400" />
              </button>
            </div>
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>

  <BaseDialog
    :show="routingEditorKey !== null"
    :title="t('keys.routing.title')"
    width="wide"
    :z-index="60"
    @close="closeRoutingEditor"
  >
    <ApiKeyRoutingEditor
      v-model="routingDraft"
      :groups="routingGroupCatalog"
      :health="routingHealth"
      :health-loading="routingHealthLoading"
      :health-window-minutes="routingHealthWindowMinutes"
      @refresh-health="loadRoutingHealth"
    />
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="closeRoutingEditor">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="routingEditorKey ? updatingKeyIds.has(routingEditorKey.id) : false"
          @click="saveRoutingEditor"
        >
          {{ t('common.update') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import {
  createEmptyRoutingDraft,
  createRoutingDraftFromApiKey,
  createRoutingInput,
  getRoutingGroupCount,
  getRoutingPrimaryGroup
} from '@/utils/apiKeyRouting'
import { getApiKeyStatusLabelKey } from '@/utils/i18nLabels'
import type {
  AdminUser,
  AdminGroup,
  ApiKey,
  ApiKeyRoutingDraft,
  ApiKeyRoutingGroupHealth,
  Group
} from '@/types'
import ApiKeyRoutingEditor from '@/components/keys/ApiKeyRoutingEditor.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const updatingKeyIds = ref(new Set<number>())
const routingEditorKey = ref<ApiKey | null>(null)
const routingDraft = ref<ApiKeyRoutingDraft>(createEmptyRoutingDraft())
const routingHealth = ref<Record<number, ApiKeyRoutingGroupHealth>>({})
const routingHealthLoading = ref(false)
const routingHealthWindowMinutes = ref(10080)
let routingHealthRequestID = 0
const routingGroupCatalog = computed<Group[]>(() => {
  const byID = new Map<number, Group>(allGroups.value.map((group) => [group.id, group]))
  for (const apiKey of apiKeys.value) {
    for (const candidate of apiKey.routing?.groups || []) {
      if (candidate.group) byID.set(candidate.group.id, candidate.group)
    }
  }
  return [...byID.values()]
})

const apiKeyStatusLabel = (value: string) => t(getApiKeyStatusLabelKey(value), { value })
const routingPrimaryGroup = getRoutingPrimaryGroup
const routingGroupCount = getRoutingGroupCount

const load = async () => {
  if (!props.user) return
  loading.value = true
  try {
    const response = await adminAPI.users.getUserApiKeys(props.user.id)
    apiKeys.value = response.items || []
  } catch (error) {
    console.error('Failed to load API keys:', error)
  } finally {
    loading.value = false
  }
}

const loadRoutingHealth = async () => {
  const groupIDs = routingGroupCatalog.value.map((group) => group.id)
  const requestID = ++routingHealthRequestID
  if (groupIDs.length === 0) {
    routingHealth.value = {}
    routingHealthLoading.value = false
    return
  }
  routingHealthLoading.value = true
  try {
    const response = await adminAPI.groups.getRoutingHealth(groupIDs)
    if (requestID !== routingHealthRequestID) return
    routingHealthWindowMinutes.value = response.window_minutes
    routingHealth.value = Object.fromEntries(
      response.items.map((item) => [item.group_id, item])
    )
  } catch (error) {
    if (requestID === routingHealthRequestID) {
      console.error('Failed to load routing health:', error)
    }
  } finally {
    if (requestID === routingHealthRequestID) {
      routingHealthLoading.value = false
    }
  }
}

const loadGroups = async () => {
  try {
    allGroups.value = await adminAPI.groups.getAll()
    await loadRoutingHealth()
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const openRoutingEditor = (apiKey: ApiKey) => {
  void loadRoutingHealth()
  routingEditorKey.value = apiKey
  routingDraft.value = createRoutingDraftFromApiKey(apiKey)
}

const closeRoutingEditor = () => {
  routingEditorKey.value = null
  routingDraft.value = createEmptyRoutingDraft()
}

watch(
  () => props.show,
  (visible) => {
    if (visible && props.user) {
      void load()
      void loadGroups()
      return
    }
    closeRoutingEditor()
  },
  { immediate: true }
)

const saveRoutingEditor = async () => {
  const apiKey = routingEditorKey.value
  const routing = createRoutingInput(routingDraft.value)
  if (!apiKey || !routing) {
    appStore.showError(t('keys.groupRequired'))
    return
  }

  updatingKeyIds.value.add(apiKey.id)
  try {
    const result = await adminAPI.apiKeys.updateApiKeyRouting(apiKey.id, routing)
    const index = apiKeys.value.findIndex((item) => item.id === apiKey.id)
    if (index >= 0) apiKeys.value[index] = result.api_key
    closeRoutingEditor()
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('keys.routing.updatedSuccess'))
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('keys.routing.updateFailed'))
  } finally {
    updatingKeyIds.value.delete(apiKey.id)
  }
}

const handleClose = () => {
  closeRoutingEditor()
  emit('close')
}
</script>
