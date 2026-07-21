<template>
  <div v-if="capabilities?.attachments_enabled" class="space-y-2">
    <div class="flex flex-wrap items-center gap-2">
      <input
        ref="inputRef"
        class="hidden"
        type="file"
        accept="image/png,image/jpeg,image/webp,text/plain,application/json,.txt,.json"
        multiple
        @change="handleFiles"
      />
      <button
        type="button"
        class="btn btn-secondary h-9"
        :disabled="uploading || modelValue.length >= capabilities.max_files_per_message"
        @click="inputRef?.click()"
      >
        <Icon name="upload" size="sm" />
        <span>{{ uploading ? t('tickets.attachments.uploading') : t('tickets.attachments.add') }}</span>
      </button>
      <span class="text-xs text-gray-500 dark:text-dark-400">
        {{ t('tickets.attachments.limits') }} · {{ formatBytes(capabilities.max_file_bytes) }}
      </span>
    </div>
    <div v-if="modelValue.length" class="flex flex-wrap gap-2">
      <span
        v-for="(attachment, index) in modelValue"
        :key="attachment.upload_token"
        class="inline-flex max-w-full items-center gap-1.5 rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-xs text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
      >
        <span class="truncate">{{ attachment.original_name }}</span>
        <span class="flex-shrink-0 text-gray-400">{{ formatBytes(attachment.byte_size) }}</span>
        <button
          type="button"
          class="ml-1 flex h-5 w-5 flex-shrink-0 items-center justify-center rounded text-gray-400 hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-white"
          :title="t('tickets.attachments.remove')"
          @click="remove(index)"
        >
          <Icon name="x" size="xs" />
        </button>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { PendingTicketAttachment, TicketCapabilities } from '@/types/ticket'

const props = defineProps<{
  modelValue: PendingTicketAttachment[]
  capabilities: TicketCapabilities | null
  upload: (file: File) => Promise<PendingTicketAttachment>
}>()
const emit = defineEmits<{
  'update:modelValue': [value: PendingTicketAttachment[]]
  error: [error: unknown]
}>()
const { t } = useI18n()
const inputRef = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

async function handleFiles(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!props.capabilities || files.length === 0) return
  if (props.modelValue.length + files.length > props.capabilities.max_files_per_message) {
    emit('error', new Error(t('tickets.attachments.tooMany')))
    return
  }
  uploading.value = true
  const next = [...props.modelValue]
  try {
    for (const file of files) {
      if (file.size > props.capabilities.max_file_bytes) {
        throw new Error(t('tickets.attachments.uploadFailed'))
      }
      next.push(await props.upload(file))
      emit('update:modelValue', [...next])
    }
  } catch (error) {
    emit('error', error)
  } finally {
    uploading.value = false
  }
}

function remove(index: number): void {
  const next = [...props.modelValue]
  next.splice(index, 1)
  emit('update:modelValue', next)
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
</script>
