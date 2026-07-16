<script setup lang="ts">
import { ArrowLeft, Search } from '@lucide/vue'

function openSearch() {
  const button = document.querySelector<HTMLButtonElement>(
    '.VPNavBarSearch button, .DocSearch-Button'
  )
  button?.click()
}
</script>

<template>
  <main class="not-found-shell">
    <p class="not-found-code">404</p>
    <h1>没有找到这个页面</h1>
    <p>该地址不存在或已调整。请搜索文档或返回目录。</p>
    <div class="not-found-actions">
      <button type="button" @click="openSearch"><Search :size="18" aria-hidden="true" />搜索文档</button>
      <a href="/troubleshooting/"><ArrowLeft :size="18" aria-hidden="true" />进入错误排查</a>
    </div>
    <a class="not-found-home" href="/">返回文档首页</a>
  </main>
</template>
