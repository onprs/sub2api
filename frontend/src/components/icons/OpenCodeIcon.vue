<template>
  <span
    class="inline-flex shrink-0 items-center justify-center"
    :style="sizeStyle"
    aria-hidden="true"
    data-opencode-icon
  >
    <img :src="lightIcon" alt="" class="h-full w-full dark:hidden" />
    <img :src="darkIcon" alt="" class="hidden h-full w-full dark:block" />
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import lightIcon from '@/assets/icons/opencode-light.svg'
import darkIcon from '@/assets/icons/opencode-dark.svg'

// 官方 OpenCode 方形图标及明暗配色：https://opencode.ai/brand
const props = defineProps<{
  size?: number | string
}>()

const sizeStyle = computed(() => {
  if (props.size == null) return undefined
  const value = typeof props.size === 'number' ? `${props.size}px` : props.size
  return { width: value, height: value }
})
</script>
