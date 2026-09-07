<template>
  <div data-testid="home-minimal" class="minimal-home">
    <header class="minimal-header">
      <router-link to="/home" class="brand-link">
        <img :src="siteLogo || '/logo.svg'" alt="Logo" />
        <span>{{ siteName }}</span>
      </router-link>

      <nav class="header-actions" :aria-label="t('home.prototype.navigation')">
        <div class="locale-wrap"><LocaleSwitcher /></div>
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="icon-action"
          :title="t('home.viewDocs')"
        >
          <Icon name="book" size="md" />
        </a>
        <router-link
          v-if="showModelPlazaEntry"
          to="/model-plaza"
          class="icon-action"
          :title="t('nav.modelPlaza')"
        >
          <Icon name="grid" size="md" />
        </router-link>
        <button
          type="button"
          class="icon-action"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="$emit('toggleTheme')"
        >
          <Icon :name="isDark ? 'sun' : 'moon'" size="md" />
        </button>
        <router-link :to="dashboardPath" class="primary-action">
          {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </nav>
    </header>

    <main>
      <section class="minimal-hero terminal-container">
        <div class="route-canvas" aria-hidden="true">
          <span class="route-line route-line-a" />
          <span class="route-line route-line-b" />
          <span class="route-line route-line-c" />
          <span class="route-node node-a">01</span>
          <span class="route-node node-b">02</span>
          <span class="route-node node-c">03</span>
          <img :src="siteLogo || '/logo.svg'" alt="" />
        </div>

        <div class="hero-copy">
          <p class="eyebrow"><span />{{ t('home.prototype.gatewayOnline') }}</p>
          <h1>{{ siteName }}</h1>
          <p class="subtitle">{{ siteSubtitle }}</p>
          <div class="hero-actions">
            <router-link :to="dashboardPath" class="hero-primary">
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <Icon name="arrowRight" size="md" />
            </router-link>
            <span class="uptime"><i />{{ t('home.uptime.label') }} {{ uptime }}</span>
          </div>
        </div>

        <div class="hero-index" aria-hidden="true">01</div>
      </section>

      <section class="model-strip" :aria-label="t('home.providers.title')">
        <span class="strip-label">{{ t('home.prototype.routeTo') }}</span>
        <div class="model-list">
          <span>Claude</span><i />
          <span>GPT</span><i />
          <span>Gemini</span><i />
          <span>Grok</span><i />
          <span>DeepSeek</span>
        </div>
      </section>
    </main>

    <footer>
      <span>&copy; {{ currentYear }} {{ siteName }}</span>
      <span>{{ t('home.uptime.startDate') }}</span>
      <a href="https://lmspeed.net/provider/api-onprs-top" target="_blank" rel="noopener noreferrer">
        <img
          src="https://lmspeed.net/api/provider/claim-badge/1420?claim=1420-pI3oIdhdh2Iekbg2DuIZuPDUska9-U9f"
          alt="Verified on LM Speed"
        />
      </a>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  siteName: string
  siteLogo: string
  siteSubtitle: string
  docUrl: string
  uptime: string
  currentYear: number
  dashboardPath: string
  isAuthenticated: boolean
  isDark: boolean
  showModelPlazaEntry: boolean
}>()

defineEmits<{ toggleTheme: [] }>()
const { t } = useI18n()
</script>

<style scoped>
.minimal-home {
  --page: #f7f9f8;
  --ink: #101615;
  --muted: #5c6866;
  --line: #cad3d1;
  --accent: #0d9488;
  min-height: 100svh;
  color: var(--ink);
  background: var(--page);
  font-family: Arial, "PingFang SC", "Microsoft YaHei", sans-serif;
  overflow-x: hidden;
}
:global(.dark .minimal-home) { --page: #090d0c; --ink: #eef5f3; --muted: #91a09d; --line: #293330; --accent: #2dd4bf; }
.minimal-header { height: 72px; padding: 0 4vw; display: flex; align-items: center; justify-content: space-between; gap: 24px; border-bottom: 1px solid var(--line); }
.brand-link { min-width: 0; display: flex; align-items: center; gap: 12px; color: inherit; font-size: 15px; font-weight: 700; }
.brand-link img { width: 34px; height: 34px; object-fit: contain; border-radius: 6px; }
.brand-link span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.header-actions { display: flex; align-items: center; gap: 6px; }
.icon-action { width: 38px; height: 38px; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); border-radius: 6px; }
.icon-action:hover { color: var(--ink); background: color-mix(in srgb, var(--ink) 7%, transparent); }
.primary-action, .hero-primary { display: inline-flex; align-items: center; justify-content: center; gap: 9px; border-radius: 6px; font-weight: 700; }
.primary-action { height: 40px; margin-left: 6px; padding: 0 16px; color: var(--page); background: var(--ink); font-size: 13px; }
.minimal-hero { position: relative; min-height: calc(100svh - 154px); padding: clamp(64px, 10vh, 112px) 6vw 72px; display: flex; align-items: center; overflow: hidden; }
.hero-copy { position: relative; z-index: 2; width: min(720px, 72%); }
.eyebrow { display: flex; align-items: center; gap: 10px; margin: 0 0 28px; color: var(--muted); font-size: 12px; font-weight: 700; text-transform: uppercase; }
.eyebrow span { width: 8px; height: 8px; border-radius: 50%; background: #22c55e; box-shadow: 0 0 0 5px color-mix(in srgb, #22c55e 18%, transparent); }
h1 { max-width: 940px; margin: 0; overflow-wrap: anywhere; font-size: clamp(62px, 11vw, 154px); font-weight: 800; line-height: .86; letter-spacing: 0; }
.subtitle { max-width: 560px; margin: 34px 0 0; color: var(--muted); font-size: clamp(17px, 2vw, 23px); line-height: 1.55; }
.hero-actions { margin-top: 42px; display: flex; align-items: center; flex-wrap: wrap; gap: 18px; }
.hero-primary { min-height: 48px; padding: 0 22px; color: white; background: var(--accent); font-size: 14px; }
.hero-primary:hover { filter: brightness(.92); }
.uptime { display: inline-flex; align-items: center; gap: 9px; color: var(--muted); font-size: 13px; font-variant-numeric: tabular-nums; }
.uptime i { width: 18px; height: 1px; background: var(--accent); }
.route-canvas { position: absolute; inset: 0 0 0 46%; overflow: hidden; }
.route-canvas img { position: absolute; right: 9%; top: 50%; width: clamp(150px, 21vw, 310px); height: clamp(150px, 21vw, 310px); object-fit: contain; opacity: .13; transform: translateY(-50%); filter: saturate(.7); }
.route-line { position: absolute; height: 1px; background: var(--line); transform-origin: left center; }
.route-line::after { content: ""; position: absolute; right: 0; top: -4px; width: 9px; height: 9px; border: 2px solid var(--accent); border-radius: 50%; background: var(--page); }
.route-line-a { left: 8%; top: 29%; width: 72%; transform: rotate(12deg); }
.route-line-b { left: 0; top: 54%; width: 88%; transform: rotate(-9deg); }
.route-line-c { left: 22%; top: 78%; width: 58%; transform: rotate(-25deg); }
.route-node { position: absolute; display: grid; place-items: center; width: 44px; height: 44px; color: var(--muted); border: 1px solid var(--line); border-radius: 50%; background: var(--page); font-size: 10px; }
.node-a { left: 7%; top: 24%; } .node-b { left: 17%; top: 50%; } .node-c { right: 13%; bottom: 14%; }
.hero-index { position: absolute; right: 4vw; bottom: 30px; color: var(--line); font-size: clamp(56px, 8vw, 110px); font-weight: 800; line-height: 1; }
.model-strip { min-height: 82px; padding: 0 4vw; display: flex; align-items: center; gap: 36px; color: var(--muted); border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.strip-label { flex: 0 0 auto; font-size: 11px; font-weight: 700; text-transform: uppercase; }
.model-list { width: 100%; display: flex; align-items: center; justify-content: space-around; gap: 20px; color: var(--ink); font-size: clamp(14px, 1.5vw, 18px); font-weight: 700; }
.model-list i { width: 3px; height: 3px; flex: 0 0 auto; border-radius: 50%; background: var(--accent); }
footer { min-height: 70px; padding: 14px 4vw; display: flex; align-items: center; justify-content: space-between; gap: 24px; color: var(--muted); font-size: 12px; }
footer img { width: auto; height: 27px; }
@media (max-width: 760px) {
  .minimal-header { height: auto; min-height: 64px; padding: 10px 14px; gap: 10px; }
  .brand-link { min-width: 0; flex: 1 1 auto; gap: 8px; font-size: 14px; }
  .brand-link img { width: 30px; height: 30px; }
  .brand-link span { display: inline-block; max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .locale-wrap, a.icon-action { display: none; }
  .header-actions { flex: 0 0 auto; gap: 6px; }
  button.icon-action { display: inline-flex !important; width: 34px; height: 34px; flex-shrink: 0; }
  .primary-action { display: inline-flex !important; min-height: 34px; margin-left: 0; padding: 0 12px; font-size: 12px; white-space: nowrap; flex-shrink: 0; }
  .minimal-hero { min-height: 560px; padding: 56px 16px 48px; align-items: flex-start; }
  .hero-copy { width: 100%; }
  h1 { font-size: clamp(48px, 15vw, 76px); letter-spacing: 0; }
  .subtitle { max-width: 100%; margin-top: 20px; font-size: 16px; }
  .hero-actions { align-items: flex-start; flex-direction: column; gap: 14px; margin-top: 32px; }
  .uptime { max-width: 100%; flex-wrap: wrap; line-height: 1.5; }
  .route-canvas { inset: 36% 0 0 15%; opacity: .55; overflow: hidden; pointer-events: none; }
  .hero-index { display: none; }
  .model-strip { padding: 16px; align-items: flex-start; flex-direction: column; gap: 12px; }
  .model-list { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px 14px; }
  .model-list i { display: none; }
  .model-list span { min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
  footer { align-items: flex-start; flex-direction: column; gap: 10px; padding: 16px; }
}
</style>
