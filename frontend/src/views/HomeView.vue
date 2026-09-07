<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
      referrerpolicy="no-referrer"
      allowfullscreen
    />
    <div v-else v-html="homeContent" />
  </div>

  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-9 w-9 rounded-md object-contain" />
          <span class="truncate font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="hidden rounded-md px-2 py-2 text-sm text-gray-500 dark:text-dark-300 sm:inline"
          >{{ t('nav.modelPlaza') }}</router-link>
          <button
            type="button"
            class="rounded-md p-2 text-gray-500 dark:text-dark-300"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          ><Icon :name="isDark ? 'sun' : 'moon'" size="md" /></button>
          <router-link
            :to="dashboardPath"
            class="rounded-md bg-gray-900 px-3 py-2 text-sm text-white dark:bg-white dark:text-gray-900"
          >{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
        </div>
      </nav>
    </header>
    <main class="flex flex-1 items-center justify-center px-6 py-16 text-center">
      <div class="max-w-2xl">
        <img :src="siteLogo || '/logo.svg'" alt="Logo" class="mx-auto mb-6 h-20 w-20 rounded-md object-contain" />
        <h1 class="text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <span class="mt-8 inline-flex rounded-md border border-primary-200 bg-primary-50 px-4 py-2 text-sm font-medium text-primary-700 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-300">
          {{ t('home.uptime.label') }} · {{ uptime }}
        </span>
        <div>
          <router-link
            :to="dashboardPath"
            class="mt-8 inline-flex rounded-md bg-primary-600 px-5 py-3 text-sm font-medium text-white"
          >{{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}</router-link>
        </div>
      </div>
    </main>
    <footer class="border-t border-gray-200 px-6 py-5 text-center text-sm text-gray-500 dark:border-dark-800 dark:text-dark-400">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <component
    :is="variantComponent"
    v-else
    :site-name="siteName"
    :site-logo="siteLogo"
    :site-subtitle="siteSubtitle"
    :doc-url="docUrl"
    :uptime="uptime"
    :current-year="currentYear"
    :dashboard-path="dashboardPath"
    :is-authenticated="isAuthenticated"
    :is-dark="isDark"
    :show-model-plaza-entry="showModelPlazaEntry"
    @toggle-theme="toggleTheme"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import HomeMinimal from '@/components/home/HomeMinimal.vue'
import HomeConsole from '@/components/home/HomeConsole.vue'
import HomeEditorial from '@/components/home/HomeEditorial.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => homeContent.value.trim().startsWith('https://'))
const isDark = ref(document.documentElement.classList.contains('dark'))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => {
  if (!isAuthenticated.value) return '/login'
  return authStore.isAdmin ? '/admin/dashboard' : '/dashboard'
})
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(
  FeatureFlags.modelPlaza,
  appStore.cachedPublicSettings,
))
const showModelPlazaEntry = computed(() => modelPlazaEnabled.value && (
  isAuthenticated.value || appStore.cachedPublicSettings?.model_plaza_require_auth !== true
))

const variant = computed(() => {
  const requested = new URLSearchParams(window.location.search).get('variant')
  if (requested === 'minimal' || requested === 'console' || requested === 'editorial') return requested
  return 'editorial'
})
const variantComponent = computed(() => {
  if (variant.value === 'console') return HomeConsole
  if (variant.value === 'editorial') return HomeEditorial
  return HomeMinimal
})

const uptimeSeconds = ref(0)
let uptimeTimer: number | undefined
const uptime = computed(() => {
  const days = Math.floor(uptimeSeconds.value / 86400)
  const hours = Math.floor(uptimeSeconds.value / 3600) % 24
  const minutes = Math.floor(uptimeSeconds.value / 60) % 60
  return t('home.uptime.value', { days, hours, minutes })
})

function refreshUptime() {
  const year = new Date().getFullYear()
  const storageKey = `sub2api-home-uptime-start-${year}`
  const defaultStart = new Date(year, 4, 1, 0, 0, 0, 0).getTime()
  const savedStart = Number(localStorage.getItem(storageKey))
  const configuredStart = appStore.cachedPublicSettings?.home_uptime_start_at
  const parsedConfiguredStart = configuredStart ? Date.parse(configuredStart) : Number.NaN
  const start = Number.isFinite(parsedConfiguredStart) && parsedConfiguredStart > 0
    ? parsedConfiguredStart
    : (Number.isFinite(savedStart) && savedStart > 0 ? savedStart : defaultStart)

  if (!configuredStart && savedStart !== start) {
    localStorage.setItem(storageKey, String(start))
  }
  uptimeSeconds.value = Math.max(0, Math.floor((Date.now() - start) / 1000))
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  refreshUptime()
  uptimeTimer = window.setInterval(refreshUptime, 60_000)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings().then(refreshUptime)
  }
})

onUnmounted(() => {
  if (uptimeTimer) window.clearInterval(uptimeTimer)
})
</script>
