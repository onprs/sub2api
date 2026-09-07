<template>
  <div data-testid="home-console" class="console-home">
    <header class="console-header">
      <div class="console-brand">
        <img :src="siteLogo || '/logo.svg'" alt="Logo" />
        <div><strong>{{ siteName }}</strong><span>GATEWAY CONSOLE</span></div>
      </div>
      <div class="console-status"><i />{{ t('home.prototype.allOperational') }}</div>
      <nav class="console-actions" :aria-label="t('home.prototype.navigation')">
        <div class="locale-wrap"><LocaleSwitcher /></div>
        <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="tool-button" :title="t('home.viewDocs')"><Icon name="book" size="md" /></a>
        <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="tool-button" :title="t('nav.modelPlaza')"><Icon name="grid" size="md" /></router-link>
        <button type="button" class="tool-button" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="$emit('toggleTheme')"><Icon :name="isDark ? 'sun' : 'moon'" size="md" /></button>
        <router-link :to="dashboardPath" class="console-login">{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
      </nav>
    </header>

    <main class="console-shell">
      <aside class="endpoint-rail">
        <div class="rail-title">{{ t('home.prototype.endpoints') }}</div>
        <button class="endpoint active"><span>POST</span><b>/v1/messages</b></button>
        <button class="endpoint"><span>POST</span><b>/v1/chat/completions</b></button>
        <button class="endpoint"><span>POST</span><b>/v1/responses</b></button>
        <button class="endpoint"><span>GET</span><b>/v1/models</b></button>
        <div class="rail-title secondary">{{ t('home.prototype.providers') }}</div>
        <ul class="provider-list">
          <li><i class="claude" />Anthropic <span>READY</span></li>
          <li><i class="openai" />OpenAI <span>READY</span></li>
          <li><i class="gemini" />Gemini <span>READY</span></li>
          <li><i class="grok" />Grok <span>READY</span></li>
        </ul>
      </aside>

      <section class="request-workspace terminal-container">
        <div class="workspace-tabs"><span class="active">request.json</span><span>response.json</span><span>headers</span></div>
        <div class="code-editor">
          <ol class="line-numbers"><li v-for="line in 10" :key="line">{{ line }}</li></ol>
          <pre><code><span class="syntax-muted">{</span>
  <span class="syntax-key">"model"</span>: <span class="syntax-value">"claude-sonnet-4"</span>,
  <span class="syntax-key">"messages"</span>: <span class="syntax-muted">[</span>
    <span class="syntax-muted">{</span>
      <span class="syntax-key">"role"</span>: <span class="syntax-value">"user"</span>,
      <span class="syntax-key">"content"</span>: <span class="syntax-value">"Hello"</span>
    <span class="syntax-muted">}</span>
  <span class="syntax-muted">]</span>,
  <span class="syntax-key">"stream"</span>: <span class="syntax-boolean">true</span>
<span class="syntax-muted">}</span></code></pre>
          <div class="editor-cursor" aria-hidden="true" />
        </div>
        <div class="request-bar">
          <span><i />{{ t('home.prototype.ready') }}</span>
          <router-link :to="dashboardPath" class="send-button"><Icon name="play" size="sm" />{{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}</router-link>
        </div>
      </section>

      <aside class="telemetry-rail">
        <section class="metric-block">
          <span>{{ t('home.uptime.label') }}</span>
          <strong>{{ uptime }}</strong>
          <small>{{ t('home.uptime.startDate') }}</small>
        </section>
        <section class="metric-block latency">
          <span>LATENCY</span>
          <strong>842 <em>ms</em></strong>
          <div class="bar-chart" aria-hidden="true"><i v-for="height in bars" :key="height" :style="{ height: `${height}%` }" /></div>
        </section>
        <section class="route-log">
          <span>{{ t('home.prototype.liveRoute') }}</span>
          <ol>
            <li><time>00:00.014</time><b>auth</b><em>pass</em></li>
            <li><time>00:00.027</time><b>schedule</b><em>pass</em></li>
            <li><time>00:00.041</time><b>upstream</b><em>200</em></li>
          </ol>
        </section>
      </aside>
    </main>

    <footer class="console-footer">
      <span><i /> API READY · {{ t('home.uptime.label') }} {{ uptime }}</span>
      <span>{{ siteSubtitle }}</span>
      <div>
        <router-link v-if="showModelPlazaEntry" to="/model-plaza">{{ t('nav.modelPlaza') }}</router-link>
        <a href="https://lmspeed.net/provider/api-onprs-top" target="_blank" rel="noopener noreferrer">LM SPEED</a>
        <span>&copy; {{ currentYear }}</span>
      </div>
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
const bars = [34, 51, 43, 68, 46, 76, 64, 89, 71, 58, 78, 94]
</script>

<style scoped>
.console-home {
  --page: #e9edef;
  --panel: #f5f7f8;
  --panel-strong: #ffffff;
  --ink: #11181c;
  --muted: #637078;
  --line: #c5cdd1;
  --green: #00a36c;
  --blue: #2563eb;
  min-height: 100svh;
  display: grid;
  grid-template-rows: 62px minmax(620px, calc(100svh - 96px)) 34px;
  color: var(--ink);
  background: var(--page);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-x: hidden;
}
:global(.dark .console-home) { --page: #080b0d; --panel: #0d1215; --panel-strong: #11171b; --ink: #e9f0f2; --muted: #718088; --line: #263137; --green: #37dc9b; --blue: #60a5fa; }
.console-header { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 18px; padding: 0 18px; border-bottom: 1px solid var(--line); background: var(--panel); }
.console-brand { min-width: 0; display: flex; align-items: center; gap: 10px; }
.console-brand img { width: 32px; height: 32px; border-radius: 5px; object-fit: contain; }
.console-brand div { min-width: 0; display: flex; flex-direction: column; }
.console-brand strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: Arial, sans-serif; font-size: 14px; }
.console-brand span { color: var(--muted); font-size: 9px; }
.console-status { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: 10px; }
.console-status i, .console-footer > span:first-child i { width: 7px; height: 7px; border-radius: 50%; background: var(--green); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 65%, transparent); }
.console-actions { justify-self: end; display: flex; align-items: center; gap: 5px; }
.tool-button { width: 36px; height: 36px; display: inline-flex; align-items: center; justify-content: center; border: 1px solid transparent; border-radius: 5px; color: var(--muted); }
.tool-button:hover { color: var(--ink); border-color: var(--line); background: var(--panel-strong); }
.console-login { min-height: 36px; padding: 0 13px; display: inline-flex; align-items: center; color: white; border-radius: 5px; background: #11181c; font-size: 11px; font-weight: 700; }
:global(.dark .console-login) { color: #07100c; background: #e9f0f2; }
.console-shell { min-width: 0; display: grid; grid-template-columns: 240px minmax(360px, 1fr) 286px; margin: 14px; border: 1px solid var(--line); background: var(--panel); box-shadow: 0 18px 50px rgba(15, 23, 42, .1); overflow: hidden; }
.endpoint-rail, .telemetry-rail { min-width: 0; background: var(--panel); }
.endpoint-rail { padding: 18px 12px; border-right: 1px solid var(--line); }
.rail-title, .metric-block > span, .route-log > span { display: block; margin: 0 8px 12px; color: var(--muted); font-size: 9px; font-weight: 700; }
.rail-title.secondary { margin-top: 32px; }
.endpoint { width: 100%; min-height: 50px; padding: 8px; display: grid; grid-template-columns: 38px minmax(0, 1fr); align-items: center; gap: 8px; text-align: left; border-radius: 4px; color: var(--muted); }
.endpoint:hover, .endpoint.active { color: var(--ink); background: var(--panel-strong); box-shadow: inset 2px 0 var(--blue); }
.endpoint span { color: var(--blue); font-size: 9px; font-weight: 800; }
.endpoint b { overflow: hidden; text-overflow: ellipsis; font-size: 10px; font-weight: 500; }
.provider-list { display: grid; gap: 3px; }
.provider-list li { min-height: 35px; padding: 0 8px; display: grid; grid-template-columns: 8px 1fr auto; align-items: center; gap: 9px; color: var(--muted); font-size: 10px; }
.provider-list li > i { width: 7px; height: 7px; border-radius: 50%; }
.provider-list li > span { color: var(--green); font-size: 8px; }
.claude { background: #dc7848; } .openai { background: #18a67d; } .gemini { background: #4285f4; } .grok { background: #65717a; }
.request-workspace { min-width: 0; display: grid; grid-template-rows: 45px 1fr 58px; background: var(--panel-strong); }
.workspace-tabs { display: flex; align-items: end; gap: 6px; padding: 0 12px; border-bottom: 1px solid var(--line); }
.workspace-tabs span { height: 44px; padding: 0 12px; display: inline-flex; align-items: center; color: var(--muted); border-bottom: 2px solid transparent; font-size: 10px; }
.workspace-tabs .active { color: var(--ink); border-color: var(--blue); }
.code-editor { position: relative; min-height: 430px; display: grid; grid-template-columns: 50px 1fr; padding: 32px 20px 32px 0; overflow: auto; }
.line-numbers { padding-right: 16px; color: var(--muted); border-right: 1px solid var(--line); text-align: right; font-size: 12px; line-height: 2.25; user-select: none; }
.code-editor pre { padding-left: 24px; color: var(--ink); font-size: clamp(12px, 1.1vw, 15px); line-height: 2.25; }
.syntax-key { color: #2563eb; } .syntax-value { color: #b45309; } .syntax-boolean { color: #7c3aed; } .syntax-muted { color: var(--muted); }
:global(.dark .console-home .syntax-key) { color: #7dd3fc; }
:global(.dark .console-home .syntax-value) { color: #fbbf24; }
:global(.dark .console-home .syntax-boolean) { color: #c4b5fd; }
.editor-cursor { position: absolute; left: 74px; top: 63px; width: 2px; height: 17px; background: var(--blue); animation: blink 1.1s steps(1) infinite; }
.request-bar { padding: 0 18px; display: flex; align-items: center; justify-content: space-between; gap: 16px; border-top: 1px solid var(--line); }
.request-bar > span { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: 10px; }
.request-bar > span i { width: 7px; height: 7px; border-radius: 50%; background: var(--green); }
.send-button { min-height: 36px; padding: 0 16px; display: inline-flex; align-items: center; gap: 8px; color: white; border-radius: 4px; background: var(--blue); font-size: 10px; font-weight: 800; }
.telemetry-rail { border-left: 1px solid var(--line); }
.metric-block, .route-log { padding: 24px 20px; border-bottom: 1px solid var(--line); }
.metric-block > span, .route-log > span { margin-left: 0; }
.metric-block strong { display: block; overflow-wrap: anywhere; font-family: Arial, sans-serif; font-size: 22px; line-height: 1.2; }
.metric-block small { display: block; margin-top: 8px; color: var(--muted); font-size: 9px; }
.metric-block.latency strong { font-size: 36px; }
.metric-block em { color: var(--muted); font-size: 11px; font-style: normal; }
.bar-chart { height: 58px; margin-top: 24px; display: flex; align-items: end; gap: 5px; }
.bar-chart i { min-width: 3px; flex: 1; background: var(--blue); opacity: .72; }
.route-log ol { display: grid; gap: 14px; }
.route-log li { display: grid; grid-template-columns: 70px 1fr auto; gap: 8px; color: var(--muted); font-size: 9px; }
.route-log b { color: var(--ink); font-weight: 500; }
.route-log em { color: var(--green); font-style: normal; }
.console-footer { padding: 0 18px; display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 16px; color: var(--muted); font-size: 9px; }
.console-footer > span:first-child { display: flex; align-items: center; gap: 7px; color: var(--green); }
.console-footer div { justify-self: end; display: flex; gap: 16px; }
.console-footer a:hover { color: var(--ink); }
@keyframes blink { 50% { opacity: 0; } }
@media (max-width: 940px) {
  .console-status { display: none; }
  .console-header { grid-template-columns: 1fr auto; }
  .console-shell { grid-template-columns: 190px minmax(0, 1fr); }
  .telemetry-rail { display: none; }
}
@media (max-width: 680px) {
  .console-home { grid-template-rows: auto auto auto; }
  .console-header { min-height: 60px; padding: 8px 12px; gap: 8px; }
  .console-brand { min-width: 0; flex: 1 1 auto; gap: 8px; }
  .console-brand strong { display: block; max-width: 130px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
  .console-brand span, .locale-wrap, a.tool-button { display: none; }
  .console-actions { flex: 0 0 auto; gap: 4px; }
  button.tool-button { display: inline-flex !important; width: 32px; height: 32px; flex-shrink: 0; }
  .console-login { display: inline-flex !important; min-height: 32px; padding: 0 10px; font-size: 11px; white-space: nowrap; flex-shrink: 0; }
  .console-shell { grid-template-columns: 1fr; margin: 8px; }
  .endpoint-rail { display: none; }
  .request-workspace { grid-template-rows: 42px 1fr auto; }
  .request-bar { padding: 10px 12px; align-items: stretch; flex-direction: column; gap: 8px; }
  .send-button { width: 100%; min-height: 36px; justify-content: center; }
  .code-editor { min-height: 360px; padding: 20px 8px 20px 0; }
  .code-editor pre { padding-left: 12px; font-size: 12px; line-height: 2; }
  .console-footer { min-height: 60px; padding: 12px; grid-template-columns: 1fr; line-height: 1.5; }
  .console-footer > span:first-child { flex-wrap: wrap; }
  .console-footer div { display: none; }
}
</style>
