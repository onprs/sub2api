<template>
  <div class="space-y-6">
    <div>
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.tickets.storage.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.tickets.storage.description') }}</p>
    </div>

    <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
    <template v-else-if="settings">
      <div class="inline-flex max-w-full overflow-x-auto rounded-md bg-gray-100 p-1 dark:bg-dark-800">
        <button v-for="mode in modes" :key="mode" type="button" :class="segmentClass(form.mode === mode)" @click="form.mode = mode">
          {{ t(`admin.tickets.storage.modes.${mode}`) }}
        </button>
      </div>

      <section v-if="form.mode === 'local'" class="space-y-4 border-y border-gray-200 py-5 dark:border-dark-700">
        <div class="grid gap-4 sm:grid-cols-2">
          <div><div class="text-xs font-medium uppercase text-gray-400">{{ t('admin.tickets.storage.localPath') }}</div><code class="mt-1 block break-all text-sm text-gray-800 dark:text-gray-200">{{ settings.local.display_path }}</code></div>
          <div><div class="text-xs font-medium uppercase text-gray-400">{{ t('admin.tickets.storage.localWritable') }}</div><div :class="['mt-1 text-sm font-medium', settings.local.writable ? 'text-emerald-600' : 'text-red-600']">{{ settings.local.writable ? t('admin.tickets.storage.localWritable') : t('admin.tickets.storage.localUnavailable') }}</div></div>
        </div>
        <div v-if="!settings.local.shared_volume" class="border-l-4 border-amber-400 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950/20 dark:text-amber-300">{{ t('admin.tickets.storage.multiInstanceWarning') }}</div>
      </section>

      <section v-if="form.mode === 's3'" class="grid gap-4 border-y border-gray-200 py-5 dark:border-dark-700 sm:grid-cols-2">
        <div class="sm:col-span-2"><label class="input-label">{{ t('admin.tickets.storage.endpoint') }}</label><input v-model.trim="form.s3.endpoint" class="input" placeholder="https://s3.example.com" /></div>
        <div><label class="input-label">{{ t('admin.tickets.storage.region') }}</label><input v-model.trim="form.s3.region" class="input" placeholder="auto" required /></div>
        <div><label class="input-label">{{ t('admin.tickets.storage.bucket') }}</label><input v-model.trim="form.s3.bucket" class="input" required /></div>
        <div><label class="input-label">{{ t('admin.tickets.storage.accessKey') }}</label><input v-model.trim="form.s3.access_key_id" class="input" :placeholder="settings.s3.access_key_id_masked" autocomplete="off" /></div>
        <div><label class="input-label">{{ t('admin.tickets.storage.secretKey') }}</label><input v-model="form.s3.secret_access_key" type="password" class="input" :placeholder="settings.s3.secret_configured ? t('admin.tickets.storage.secretPlaceholder') : ''" autocomplete="new-password" /><p v-if="settings.s3.secret_configured" class="mt-1 text-xs text-gray-500">{{ t('admin.tickets.storage.secretConfigured') }}</p></div>
        <div><label class="input-label">{{ t('admin.tickets.storage.prefix') }}</label><input v-model.trim="form.s3.prefix" class="input" placeholder="ticket-attachments/" /></div>
        <label class="flex items-center gap-2 self-end pb-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="form.s3.force_path_style" type="checkbox" class="h-4 w-4 rounded border-gray-300" />{{ t('admin.tickets.storage.pathStyle') }}</label>
      </section>

      <section class="space-y-3">
        <h3 class="text-sm font-semibold uppercase text-gray-500 dark:text-dark-400">{{ t('admin.tickets.storage.usage') }}</h3>
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="border-l-2 border-gray-300 pl-3 dark:border-dark-600"><div class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.tickets.storage.modes.local') }}</div><div class="mt-1 text-xs text-gray-500">{{ t('admin.tickets.storage.files', { count: settings.usage.local.files }) }} · {{ formatBytes(settings.usage.local.bytes) }}</div></div>
          <div class="border-l-2 border-gray-300 pl-3 dark:border-dark-600"><div class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.tickets.storage.modes.s3') }}</div><div class="mt-1 text-xs text-gray-500">{{ t('admin.tickets.storage.files', { count: settings.usage.s3.files }) }} · {{ formatBytes(settings.usage.s3.bytes) }}</div></div>
        </div>
      </section>

      <div class="flex flex-wrap justify-end gap-3 border-t border-gray-200 pt-5 dark:border-dark-700">
        <button type="button" class="btn btn-secondary" :disabled="testing || saving" @click="testStorage"><Icon name="beaker" size="sm" />{{ testing ? t('admin.tickets.storage.testing') : t('admin.tickets.storage.test') }}</button>
        <button type="button" class="btn btn-primary" :disabled="testing || saving" @click="saveStorage">{{ saving ? t('admin.tickets.storage.saving') : t('admin.tickets.storage.save') }}</button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api'
import { useAppStore, useTicketNotificationsStore } from '@/stores'
import type { TicketStorageMode, TicketStorageSettings, TicketStorageUpdate } from '@/types/ticket'

const { t } = useI18n()
const appStore = useAppStore()
const notifications = useTicketNotificationsStore()
const modes: TicketStorageMode[] = ['disabled', 'local', 's3']
const loading = ref(true)
const testing = ref(false)
const saving = ref(false)
const settings = ref<TicketStorageSettings | null>(null)
const form = reactive<TicketStorageUpdate>({ mode: 'disabled', s3: { endpoint: '', region: 'auto', bucket: '', access_key_id: '', secret_access_key: '', prefix: 'ticket-attachments/', force_path_style: false } })

async function load(): Promise<void> {
  loading.value = true
  try {
    settings.value = await adminAPI.tickets.getStorageSettings()
    form.mode = settings.value.mode
    Object.assign(form.s3, {
      endpoint: settings.value.s3.endpoint,
      region: settings.value.s3.region || 'auto',
      bucket: settings.value.s3.bucket,
      access_key_id: '',
      secret_access_key: '',
      prefix: settings.value.s3.prefix || 'ticket-attachments/',
      force_path_style: settings.value.s3.force_path_style,
    })
  } catch (error) { showError(error) } finally { loading.value = false }
}
function payload(): TicketStorageUpdate { return { mode: form.mode, s3: { ...form.s3 } } }
async function testStorage(): Promise<void> {
  testing.value = true
  try { await adminAPI.tickets.testStorageSettings(payload()); appStore.showSuccess(t('admin.tickets.storage.testSuccess')) } catch (error) { showError(error) } finally { testing.value = false }
}
async function saveStorage(): Promise<void> {
  saving.value = true
  try {
    settings.value = await adminAPI.tickets.updateStorageSettings(payload())
    form.s3.secret_access_key = ''
    form.s3.access_key_id = ''
    appStore.showSuccess(t('admin.tickets.storage.saveSuccess'))
    await notifications.refresh(true, true)
  } catch (error) { showError(error) } finally { saving.value = false }
}
function showError(error: unknown): void {
  const apiError = error as { code?: string | number; reason?: string; message?: string }
  const destinationInUse = apiError.reason === 'TICKET_STORAGE_DESTINATION_IN_USE' || apiError.code === 'TICKET_STORAGE_DESTINATION_IN_USE'
  appStore.showError(destinationInUse ? t('admin.tickets.storage.destinationInUse') : apiError.message || t('admin.tickets.storage.failed'))
}
function segmentClass(active: boolean): string { return `h-9 whitespace-nowrap rounded px-3 text-sm font-medium ${active ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white' : 'text-gray-500 dark:text-dark-300'}` }
function formatBytes(bytes: number): string { if (bytes < 1024) return `${bytes} B`; if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`; if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`; return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB` }

onMounted(load)
</script>
