<template>
  <section class="card" aria-labelledby="read-only-api-key-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="read-only-api-key-title" class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.adminReadOnlyApiKey.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.adminReadOnlyApiKey.description') }}
      </p>
    </div>
    <div class="space-y-4 p-6">
      <div v-if="loading" role="status" class="flex items-center gap-2 text-gray-500">
        <span class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"></span>
        {{ t('common.loading') }}
      </div>
      <div v-else-if="loadFailed" class="flex flex-wrap items-center justify-between gap-3">
        <p role="alert" class="text-sm text-red-600 dark:text-red-400">
          {{ t('admin.settings.adminReadOnlyApiKey.loadFailed') }}
        </p>
        <button type="button" class="btn btn-secondary btn-sm" @click="loadStatus">
          {{ t('admin.settings.adminReadOnlyApiKey.retry') }}
        </button>
      </div>
      <template v-else>
        <div v-if="!exists" class="flex flex-wrap items-center justify-between gap-3">
          <span class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.adminReadOnlyApiKey.notConfigured') }}
          </span>
          <button type="button" class="btn btn-primary btn-sm" :disabled="operating" @click="createKey">
            {{ t(operating ? 'admin.settings.adminReadOnlyApiKey.creating' : 'admin.settings.adminReadOnlyApiKey.create') }}
          </button>
        </div>
        <div v-else class="flex flex-wrap items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="mb-1 text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.adminReadOnlyApiKey.currentKey') }}
            </p>
            <code class="break-all rounded bg-gray-100 px-2 py-1 font-mono text-sm text-gray-900 dark:bg-dark-700 dark:text-gray-100">
              {{ maskedKey }}
            </code>
          </div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="operating" @click="regenerateKey">
              {{ t(operating ? 'admin.settings.adminReadOnlyApiKey.regenerating' : 'admin.settings.adminReadOnlyApiKey.regenerate') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400" :disabled="operating" @click="deleteKey">
              {{ t('admin.settings.adminReadOnlyApiKey.delete') }}
            </button>
          </div>
        </div>
        <div v-if="newKey" class="space-y-3 rounded-lg border border-green-200 bg-green-50 p-4 dark:border-green-800 dark:bg-green-900/20">
          <p class="text-sm font-medium text-green-700 dark:text-green-300">
            {{ t('admin.settings.adminReadOnlyApiKey.keyWarning') }}
          </p>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
            <code class="min-w-0 flex-1 select-all break-all rounded border border-green-300 bg-white px-3 py-2 font-mono text-sm dark:border-green-700 dark:bg-dark-800">
              {{ newKey }}
            </code>
            <button type="button" class="btn btn-primary btn-sm flex-shrink-0" @click="copyKey">
              {{ t('admin.settings.adminReadOnlyApiKey.copyKey') }}
            </button>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.adminReadOnlyApiKey.usage') }}
        </p>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const loading = ref(true)
const loadFailed = ref(false)
const exists = ref(false)
const maskedKey = ref('')
const newKey = ref('')
const operating = ref(false)

async function loadStatus() {
  loading.value = true
  loadFailed.value = false
  try {
    const status = await adminAPI.settings.getAdminReadOnlyApiKey()
    exists.value = status.exists
    maskedKey.value = status.masked_key
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

async function createKey() {
  if (operating.value || loading.value || loadFailed.value) return
  operating.value = true
  try {
    const result = await adminAPI.settings.regenerateAdminReadOnlyApiKey()
    newKey.value = result.key
    exists.value = true
    maskedKey.value = result.key.slice(0, 10) + '...' + result.key.slice(-4)
    appStore.showSuccess(t('admin.settings.adminReadOnlyApiKey.keyGenerated'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    operating.value = false
  }
}

async function regenerateKey() {
  if (operating.value || !confirm(t('admin.settings.adminReadOnlyApiKey.regenerateConfirm'))) return
  await createKey()
}

async function deleteKey() {
  if (operating.value || !confirm(t('admin.settings.adminReadOnlyApiKey.deleteConfirm'))) return
  operating.value = true
  try {
    await adminAPI.settings.deleteAdminReadOnlyApiKey()
    exists.value = false
    maskedKey.value = ''
    newKey.value = ''
    appStore.showSuccess(t('admin.settings.adminReadOnlyApiKey.keyDeleted'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    operating.value = false
  }
}

async function copyKey() {
  await copyToClipboard(newKey.value, t('admin.settings.adminReadOnlyApiKey.keyCopied'))
}

onMounted(loadStatus)
onBeforeUnmount(() => { newKey.value = '' })
</script>
