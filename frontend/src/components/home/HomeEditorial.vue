<template>
  <div data-testid="home-editorial" class="editorial-home">
    <header class="editorial-header">
      <router-link to="/home" class="editorial-brand">
        <img :src="siteLogo || '/logo.svg'" alt="Logo" />
        <strong>{{ siteName }}</strong>
      </router-link>
      <nav class="editorial-actions" :aria-label="t('home.prototype.navigation')">
        <div class="locale-wrap"><LocaleSwitcher /></div>
        <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="square-action optional-action" :title="t('home.viewDocs')">
          <Icon name="book" size="md" />
        </a>
        <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="square-action optional-action" :title="t('nav.modelPlaza')">
          <Icon name="grid" size="md" />
        </router-link>
        <button type="button" class="square-action" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="$emit('toggleTheme')">
          <Icon :name="isDark ? 'sun' : 'moon'" size="md" />
        </button>
        <router-link :to="dashboardPath" class="enter-link">
          {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </nav>
    </header>

    <main ref="trackRef" :class="{ 'journey-track': enabled }" :style="trackStyle">
      <div ref="stageRef" class="editorial-scenes" :class="{ 'journey-stage': enabled }" :style="stageStyle">
      <HomeJourneyArtwork v-if="enabled && frame" :frame="frame" />
      <section id="cover" class="act-cover" aria-labelledby="cover-title" :style="sceneStyle(0)" :inert="inactive(0) || undefined" :aria-hidden="inactive(0)">
        <HomeRoutingArtwork v-if="!enabled" :progress="0" :dark="isDark" />
        <div class="cover-band mono">
          <span><i class="accent-mark" /> {{ t('home.prototype.routingEdition') }}</span>
          <span>AI INFRASTRUCTURE / {{ currentYear }}</span>
        </div>
        <div class="cover-body">
          <h1 id="cover-title">{{ siteName }}</h1>
          <div class="cover-deck">
            <h2>{{ t('home.prototype.editorialCoverTitle') }}</h2>
            <p>{{ siteSubtitle }}</p>
            <router-link :to="dashboardPath" class="cover-cta primary-link">
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <Icon name="arrowRight" size="md" />
            </router-link>
          </div>
        </div>
        <div class="cover-manifesto">
          <div class="cover-tech-index">
            <div><strong>{{ protocolChannels.length.toString().padStart(2, '0') }}</strong><span>{{ t('home.prototype.editorialCoverIndex.surfaces') }}</span></div>
            <div><strong>{{ totalModelsCount }}</strong><span>{{ t('home.prototype.editorialCoverIndex.models') }}</span></div>
            <div><strong>{{ providerGroups.length.toString().padStart(2, '0') }}</strong><span>{{ t('home.prototype.editorialSummaryProviders') }}</span></div>
          </div>
          <a href="#protocols" class="cover-next" :aria-label="t('home.prototype.editorialNavigation.protocols')" @click="scrollToSection"><Icon name="arrowDown" size="md" /></a>
        </div>
      </section>

      <section id="protocols" class="act-route section-pad inverse" aria-labelledby="protocol-title" :style="sceneStyle(1)" :inert="inactive(1) || undefined" :aria-hidden="inactive(1)">
        <div class="section-topline mono"><span>01 / CONNECTIVITY</span><span>{{ t('home.prototype.editorialProtocols') }}</span></div>
        <div class="route-core">
          <div class="route-endpoint" aria-hidden="true"><strong class="route-primary">/v1</strong><span>+</span></div>
          <div class="route-thesis">
            <h2 id="protocol-title">{{ t('home.prototype.editorialRouteTitle') }}</h2>
            <p>{{ t('home.prototype.editorialDescription') }}</p>
          </div>
        </div>
        <div class="protocol-ledger">
          <div v-for="(protocol, idx) in protocolChannels" :key="protocol.path" class="protocol-row">
            <span class="protocol-index mono">0{{ idx + 1 }}</span>
            <h3>{{ protocol.feature }}</h3>
            <div class="protocol-path"><span class="protocol-method mono">POST</span><code>{{ protocol.path }}</code></div>
            <Icon name="arrowRight" size="md" class="protocol-arrow" />
          </div>
        </div>
        <div class="section-foot mono"><span>RESPONSES / CHAT / MESSAGES / GENAI</span><span>{{ t('home.prototype.oneEndpoint') }}</span></div>
      </section>

      <section id="models" class="act-fleet section-pad" aria-labelledby="models-title" :style="sceneStyle(2)" :inert="inactive(2) || undefined" :aria-hidden="inactive(2)">
        <div class="section-topline mono"><span>02 / MODEL DIRECTORY</span><span>{{ t('home.prototype.editorialCatalogLead', { count: totalModelsCount }) }}</span></div>
        <div class="model-directory">
          <div class="model-intro">
            <span class="eyebrow mono">MODEL INDEX</span>
            <h2 id="models-title">{{ t('home.prototype.editorialModels') }}</h2>
            <div class="catalog-total" aria-hidden="true"><strong class="catalog-primary">{{ totalModelsCount }}</strong><span>/ {{ providerGroups.length.toString().padStart(2, '0') }}</span></div>
            <p>{{ t('home.prototype.editorialCatalogDescription') }}</p>
            <nav class="provider-index" :aria-label="t('home.prototype.editorialSummaryProviders')">
              <a v-for="group in providerGroups" :key="group.name" :href="`#model-${group.key}`" @click="scrollToSection"><span>{{ group.code }}</span>{{ group.displayName }}<Icon name="arrowRight" size="sm" /></a>
            </nav>
            <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="text-link">{{ t('nav.modelPlaza') }}<Icon name="arrowRight" size="sm" /></router-link>
          </div>
          <div class="provider-ledger">
            <article v-for="group in providerGroups" :id="`model-${group.key}`" :key="group.name" class="provider-row">
              <div class="provider-meta mono"><span>{{ group.code }} / {{ group.name }}</span><span>{{ group.models.length.toString().padStart(2, '0') }} {{ t('home.prototype.editorialSelectedModels') }}</span></div>
              <h3 class="provider-identity" :class="{ 'long-name': group.displayName.length > 8 }">{{ group.displayName }}</h3>
              <ul class="provider-models">
                <li v-for="(model, index) in group.models" :key="model" :class="{ 'model-featured': index === 0 }"><span class="model-tick" aria-hidden="true" /><code class="model-id">{{ model }}</code></li>
              </ul>
            </article>
          </div>
        </div>
        <div class="section-foot mono"><span>{{ totalModelsCount }} SELECTED MODELS</span><span>{{ providerGroups.length }} SOURCES / {{ protocolChannels.length }} API SURFACES</span></div>
      </section>

      <section id="architecture" class="spread-pipeline section-pad inverse" aria-labelledby="pipeline-title" :style="sceneStyle(3)" :inert="inactive(3) || undefined" :aria-hidden="inactive(3)">
        <div class="section-topline mono"><span>03 / ARCHITECTURE</span><span>{{ t('home.prototype.editorialPipeline.tag') }}</span></div>
        <div class="pipeline-layout">
          <div class="pipeline-intro">
            <h2 id="pipeline-title">{{ t('home.prototype.editorialPipeline.title') }}</h2>
            <p>{{ t('home.prototype.editorialPipeline.subtitle') }}</p>
            <div class="pipeline-diagram" aria-hidden="true"><span>IN</span><i /><strong>IR</strong><i /><span>OUT</span></div>
            <span class="pipeline-caption mono">INGRESS / INTERMEDIATE REPRESENTATION / EGRESS</span>
          </div>
          <div class="pipeline-ledger-grid">
            <article v-for="(step, index) in pipelineSteps" :key="step.action" class="pipeline-step-item">
              <span class="step-index">0{{ index + 1 }}</span>
              <div class="step-body">
                <strong class="step-action mono">{{ step.action }}</strong>
                <h3>{{ t(`home.prototype.editorialPipeline.step${index + 1}.name`) }}</h3>
                <p>{{ t(`home.prototype.editorialPipeline.step${index + 1}.desc`) }}</p>
                <span class="step-rule-foot mono">{{ step.detail }}</span>
              </div>
            </article>
          </div>
        </div>
      </section>

      <section id="semantics" class="spread-semantics section-pad" aria-labelledby="semantics-title" :style="sceneStyle(4)" :inert="inactive(4) || undefined" :aria-hidden="inactive(4)">
        <div class="section-topline mono"><span>04 / SEMANTICS</span><span>{{ t('home.prototype.editorialSemantics.tag') }}</span></div>
        <div class="semantics-heading"><h2 id="semantics-title">{{ t('home.prototype.editorialSemantics.title') }}</h2><p>{{ t('home.prototype.editorialSemantics.description') }}</p></div>
        <div class="semantic-assurance-rail">
          <article v-for="(item, index) in assuranceItems" :key="item.key" class="assurance-cell" :data-journey-icon="item.icon">
            <div class="assurance-topline mono"><span>0{{ index + 1 }}</span><Icon :name="item.icon" size="lg" /></div>
            <h3>{{ item.name }}</h3>
            <p>{{ t(`home.prototype.editorialAssurance.${item.key}`) }}</p>
          </article>
        </div>
      </section>

      <section id="control" class="spread-control section-pad" aria-labelledby="control-title" :style="sceneStyle(5)" :inert="inactive(5) || undefined" :aria-hidden="inactive(5)">
        <div class="section-topline mono"><span>05 / CONTROL PLANE</span><span>{{ t('home.prototype.editorialControl.tag') }}</span></div>
        <div class="control-layout">
          <div class="control-intro"><h2 id="control-title">{{ t('home.prototype.editorialControl.title') }}</h2><p>{{ t('home.prototype.editorialControl.subtitle') }}</p></div>
          <div class="control-ledger-grid">
            <article v-for="(feature, index) in controlFeatures" :key="feature.key" class="control-feature-entry" :data-journey-icon="feature.icon">
              <div class="feature-head"><span class="feature-idx mono">0{{ index + 1 }}</span><Icon :name="feature.icon" size="lg" /></div>
              <h3>{{ t(`home.prototype.editorialControl.${feature.key}.title`) }}</h3>
              <p>{{ t(`home.prototype.editorialControl.${feature.key}.${feature.desc}`) }}</p>
              <div class="feature-meta-bar mono"><span v-for="tag in feature.tags" :key="tag">{{ tag }}</span></div>
            </article>
          </div>
        </div>
      </section>

      <section id="ready" class="act-ready section-pad inverse" aria-labelledby="ready-title" :style="sceneStyle(6)" :inert="inactive(6) || undefined" :aria-hidden="inactive(6)">
        <div class="section-topline mono"><span>06 / YOUR NEXT REQUEST</span><span>{{ siteName }}</span></div>
        <div class="ready-center"><span class="eyebrow mono">ONE GATEWAY. MORE POSSIBILITIES.</span><h2 id="ready-title">{{ t('home.prototype.editorialEndingTitle') }}</h2></div>
        <div class="ready-bottom"><div class="ready-word" aria-hidden="true"><span v-for="(letter, index) in 'READY.'" :key="index" class="ready-letter" :class="{ 'ready-dot': letter === '.' }">{{ letter }}</span></div><router-link :to="dashboardPath" class="poster-cta-btn primary-link">{{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}<Icon name="arrowRight" size="lg" /></router-link></div>
        <div class="ready-capabilities-row" :aria-label="t('home.prototype.editorialControl.tag')"><div v-for="feature in controlFeatures" :key="feature.key" class="cap-col"><span class="mono">{{ t(`home.prototype.editorialControl.${feature.key}.${feature.summary}`) }}</span></div></div>
      </section>
      <nav v-if="enabled" class="journey-chapters mono" :aria-label="t('home.prototype.editorialNavigation.sections')">
        <a v-for="(id, index) in sceneIds" :key="id" :href="`#${id}`" :aria-current="phase === index ? 'step' : undefined" :title="t(chapterLabels[index]!)" :aria-label="t(chapterLabels[index]!)" @click="scrollToSection">
          <span>0{{ index + 1 }}</span><strong>{{ t(chapterLabels[index]!) }}</strong>
        </a>
      </nav>
      </div>
    </main>

    <footer class="editorial-footer">
      <div class="footer-brand"><strong>{{ siteName }}</strong><span>&copy; {{ currentYear }}</span></div>
      <div class="footer-uptime"><span>{{ t('home.uptime.label') }}</span><strong>{{ uptime }}</strong><small>{{ t('home.uptime.startDate') }}</small></div>
      <div class="footer-links"><router-link v-if="showModelPlazaEntry" to="/model-plaza">{{ t('nav.modelPlaza') }}</router-link><a href="https://github.com/Wei-Shaw/sub2api" target="_blank" rel="noopener noreferrer">GitHub</a><a href="https://lmspeed.net/provider/api-onprs-top" target="_blank" rel="noopener noreferrer">LM Speed</a></div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import HomeRoutingArtwork from './HomeRoutingArtwork.vue'
import HomeJourneyArtwork from './HomeJourneyArtwork.vue'
import { useHomeJourney } from './useHomeJourney'

const props = defineProps<{
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
const { t, locale } = useI18n()
const appStore = useAppStore()

// 公开配置优先；仅在配置不可用时沿用已核实的能力基线。
function publicCapabilityEnabled(value: boolean | undefined): boolean {
  if (appStore.cachedPublicSettings) return value === true
  return appStore.publicSettingsLoaded !== true
}
const emailVerifyEnabled = computed(() => publicCapabilityEnabled(appStore.cachedPublicSettings?.email_verify_enabled))
const totpEnabled = computed(() => publicCapabilityEnabled(appStore.cachedPublicSettings?.totp_enabled))
const paymentEnabled = computed(() => publicCapabilityEnabled(appStore.cachedPublicSettings?.payment_enabled))
const modelPricingEnabled = computed(() => publicCapabilityEnabled(appStore.cachedPublicSettings?.model_pricing_enabled))
const errorViewEnabled = computed(() => publicCapabilityEnabled(appStore.cachedPublicSettings?.allow_user_view_error_requests))

// 精选目录，不作为实时可用性或完整上游模型清单。
const providerGroups = [
  { key: 'openai', code: '01', name: 'OpenAI', displayName: 'GPT', models: ['gpt-6-astra', 'gpt-5.6', 'gpt-5.6-sol', 'gpt-5.4-mini'] },
  { key: 'google', code: '02', name: 'Google', displayName: 'GEMINI', models: ['google/gemini-3.8-flash', 'google/gemini-3.7-flash', 'google/gemini-3.6-flash', 'gemini-3.1-pro-preview'] },
  { key: 'deepseek', code: '03', name: 'DeepSeek', displayName: 'DEEPSEEK', models: ['deepseek/deepseek-v4-pro', 'deepseek/deepseek-v4-flash'] },
  { key: 'zhipu', code: '04', name: 'Zhipu AI', displayName: 'GLM', models: ['zai-org/GLM-5.3'] },
  { key: 'meta', code: '05', name: 'Meta', displayName: 'MUSE-SPARK', models: ['meta/muse-spark-1.3', 'meta/muse-spark-1.2'] },
  { key: 'moonshot', code: '06', name: 'Moonshot', displayName: 'KIMI', models: ['moonshotai/Kimi-K3', 'moonshotai/Kimi-K2.7-Code'] },
]
const totalModelsCount = providerGroups.reduce((count, group) => count + group.models.length, 0)
const protocolChannels = [
  { path: '/v1/responses', feature: 'Responses' },
  { path: '/v1/chat/completions', feature: 'Chat Completions' },
  { path: '/v1/messages', feature: 'Messages' },
  { path: '/v1beta/models/{model}:generateContent', feature: 'Google GenAI' },
]
const pipelineSteps = [
  { action: 'RECEIVE', detail: 'RESPONSES / CHAT / MESSAGES / GENAI' },
  { action: 'NORMALIZE', detail: 'TOOLS / REASONING / MESSAGES SCHEMAS' },
  { action: 'ORCHESTRATE', detail: 'MODEL ROUTING & PRE-OUTPUT FAILOVER' },
  { action: 'DELIVER', detail: 'SSE STREAM & CACHED TOKEN LEDGER' },
]
const assuranceItems = [
  { key: 'stream', name: 'STREAM', icon: 'bolt' },
  { key: 'tools', name: 'TOOLS', icon: 'terminal' },
  { key: 'reasoning', name: 'REASONING', icon: 'brain' },
  { key: 'usage', name: 'USAGE', icon: 'chart' },
] as const

interface ControlFeature {
  key: string
  icon: InstanceType<typeof Icon>['$props']['name']
  desc: string
  summary: string
  tags: string[]
}
const controlFeatures = computed(() => {
  const features: ControlFeature[] = [
    { key: 'keys', icon: 'key', desc: 'desc', summary: 'summary', tags: ['ISOLATED KEYS', 'QUOTA CAPS'] },
    { key: 'usage', icon: 'chart', desc: errorViewEnabled.value ? 'descWithErrors' : 'desc', summary: errorViewEnabled.value ? 'summaryWithErrors' : 'summary', tags: errorViewEnabled.value ? ['PER-REQUEST TOKENS', 'ERROR TRACE'] : ['PER-REQUEST TOKENS', 'REQUEST ID'] },
  ]
  if (modelPricingEnabled.value || paymentEnabled.value) {
    features.push({
      key: 'pricing', icon: 'creditCard',
      desc: modelPricingEnabled.value && paymentEnabled.value ? 'descWithPayment' : modelPricingEnabled.value ? 'desc' : 'descPayment',
      summary: modelPricingEnabled.value && paymentEnabled.value ? 'summaryWithPayment' : modelPricingEnabled.value ? 'summary' : 'summaryPayment',
      tags: [...(modelPricingEnabled.value ? ['MODEL UNIT RATES'] : []), ...(paymentEnabled.value ? ['ONLINE PAY'] : [])],
    })
  }
  if (emailVerifyEnabled.value || totpEnabled.value) {
    features.push({
      key: 'security', icon: 'shield',
      desc: emailVerifyEnabled.value && totpEnabled.value ? 'desc' : emailVerifyEnabled.value ? 'descEmail' : 'descTotp',
      summary: emailVerifyEnabled.value && totpEnabled.value ? 'summary' : emailVerifyEnabled.value ? 'summaryEmail' : 'summaryTotp',
      tags: [...(emailVerifyEnabled.value ? ['EMAIL VERIFY'] : []), ...(totpEnabled.value ? ['TOTP 2FA'] : [])],
    })
  }
  return features
})

const { trackRef, stageRef, enabled, trackStyle, stageStyle, frame, phase, sceneStyle, inactive, scrollToSection, sceneIds } = useHomeJourney(() => props.isDark, () => [locale.value, controlFeatures.value, props.siteName, props.siteSubtitle])
const chapterLabels = [
  'home.prototype.routingEdition',
  'home.prototype.editorialNavigation.protocols',
  'home.prototype.editorialNavigation.models',
  'home.prototype.editorialPipeline.tag',
  'home.prototype.editorialSemantics.tag',
  'home.prototype.editorialControl.tag',
  'home.getStarted',
]
</script>

<style scoped>
.editorial-home {
  --paper: #fafafa;
  --ink: #1b1c1f;
  --muted: #65666c;
  --rule: #dcdde0;
  --accent: #315be8;
  --soft: #f0f0f2;
  --gutter: 64px;
  color: var(--ink);
  background: var(--paper);
  font-family: Arial, "Helvetica Neue", "PingFang SC", "Microsoft YaHei", sans-serif;
  letter-spacing: 0;
}
:global(.dark .editorial-home) {
  --paper: #1c1d20;
  --ink: #f5f5f6;
  --muted: #a9aab0;
  --rule: #3b3c42;
  --accent: #8aa5ff;
  --soft: #242529;
}
.editorial-home :is(h1, h2, h3, p) { margin: 0; }
.editorial-home :is(a, button):focus-visible { outline: 2px solid var(--accent); outline-offset: 5px; }
.editorial-home :is(section[id], article[id]) { scroll-margin-top: 88px; }
.editorial-home a { text-decoration: none; }
.editorial-home :is(h1, h2, h3, p, code) { overflow-wrap: anywhere; }
.mono, code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.editorial-header {
  position: sticky;
  top: 0;
  z-index: 50;
  height: 64px;
  padding: 0 32px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 24px;
  border-bottom: 1px solid var(--rule);
  background: var(--paper);
}
.editorial-brand { display: flex; align-items: center; gap: 10px; min-width: 0; color: var(--ink); }
.editorial-brand img { width: 28px; height: 28px; flex: 0 0 auto; object-fit: contain; border-radius: 4px; }
.editorial-brand strong { font-size: 15px; text-overflow: ellipsis; overflow: hidden; white-space: nowrap; }
.editorial-actions { justify-self: end; display: flex; align-items: center; gap: 6px; }
.square-action { width: 36px; height: 36px; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); background: transparent; border: 0; border-radius: 4px; }
.square-action:hover { background: var(--soft); color: var(--ink); }
.enter-link { display: flex; align-items: center; gap: 18px; padding: 9px 14px; font-size: 12px; font-weight: 700; color: var(--paper); background: var(--ink); border-radius: 4px; white-space: nowrap; }
.enter-link:hover { background: var(--accent); color: var(--paper); }
.locale-wrap :deep(button[title]) { color: var(--muted); }
.section-pad { padding: 28px var(--gutter) 32px; }
.section-topline, .section-foot { display: flex; align-items: center; justify-content: space-between; gap: 20px; font-size: 11px; line-height: 1.6; }
.section-topline { color: var(--muted); }
.section-foot { margin-top: 64px; padding-top: 22px; border-top: 1px solid var(--rule); color: var(--muted); }
.inverse { --paper: #17181b; --ink: #f8f8fa; --muted: #a7a8b0; --rule: #3b3c43; --accent: #8aa5ff; color: var(--ink); background: var(--paper); }
.act-cover { position: relative; min-height: calc(100svh - 112px); display: grid; grid-template-rows: 64px 1fr auto; isolation: isolate; }
.cover-band { position: relative; display: flex; justify-content: space-between; align-items: center; padding: 0 var(--gutter); color: var(--muted); font-size: 11px; gap: 16px; }
.cover-band > span:first-child { display: flex; align-items: center; gap: 8px; }
.accent-mark { display: block; width: 7px; height: 7px; background: var(--accent); }
.cover-body { position: relative; display: flex; flex-direction: column; align-items: flex-start; justify-content: space-between; gap: 48px; padding: 8px var(--gutter) 48px; }
.cover-body h1 { max-width: 100%; font-size: 176px; font-weight: 600; line-height: 1; }
.cover-deck { max-width: 43%; }
.cover-deck h2 { font-size: 48px; font-weight: 500; line-height: 1.2; white-space: pre-line; }
.cover-deck p { margin: 20px 0 28px; font-size: 14px; line-height: 1.6; color: var(--muted); }
.primary-link { display: inline-flex; align-items: center; justify-content: space-between; gap: 40px; min-height: 48px; padding: 14px 22px; background: #315be8; color: #fff; border-radius: 4px; font-size: 14px; font-weight: 500; }
.primary-link:hover { background: #2447b8; }
.cover-manifesto { position: relative; display: flex; align-items: center; justify-content: space-between; gap: 24px; margin: 0 var(--gutter); padding: 24px 0; border-top: 1px solid var(--rule); }
.cover-tech-index { display: flex; gap: 48px; }
.cover-tech-index > div { display: flex; align-items: baseline; gap: 10px; }
.cover-tech-index strong { font-size: 28px; font-weight: 400; line-height: 1; }
.cover-tech-index span { font-size: 11px; color: var(--muted); }
.cover-next { display: flex; align-items: center; justify-content: center; width: 40px; height: 40px; color: var(--ink); border: 1px solid var(--rule); border-radius: 50%; flex: 0 0 auto; }
.cover-next:hover { border-color: var(--accent); color: var(--accent); }
.route-core { display: grid; grid-template-columns: 1fr 1fr; align-items: center; gap: 64px; padding: 76px 0 64px; }
.route-endpoint { font-size: 220px; font-weight: 500; line-height: 1; white-space: nowrap; }
.route-primary, .catalog-primary { display: inline-block; font-weight: inherit; }
.route-endpoint > span { color: var(--accent); font-size: 120px; vertical-align: top; }
.route-thesis h2 { font-size: 48px; line-height: 1.2; font-weight: 500; white-space: pre-line; }
.route-thesis p { max-width: 420px; margin-top: 28px; color: var(--muted); font-size: 15px; line-height: 1.8; }
.protocol-row { display: grid; grid-template-columns: 32px minmax(180px, 0.85fr) minmax(0, 1.15fr) 24px; align-items: center; gap: 24px; min-height: 106px; padding: 24px 0; border-top: 1px solid var(--rule); }
.protocol-index { font-size: 11px; color: var(--muted); }
.protocol-row h3 { font-size: 25px; font-weight: 400; }
.protocol-path { display: flex; align-items: flex-start; gap: 16px; min-width: 0; }
.protocol-method { color: var(--accent); font-size: 10px; line-height: 24px; flex: 0 0 auto; }
.protocol-path code { font-size: 13px; line-height: 24px; }
.protocol-arrow { color: var(--muted); }
.act-route .section-foot { margin-top: 0; }
.act-fleet { padding-top: 32px; }
.model-directory { display: grid; grid-template-columns: minmax(0, 0.42fr) minmax(0, 0.58fr); gap: 80px; margin-top: 88px; }
.model-intro { position: sticky; top: 112px; align-self: start; padding-bottom: 32px; }
.eyebrow { font-size: 11px; color: var(--muted); }
.model-intro h2 { margin-top: 24px; max-width: 420px; font-size: 48px; line-height: 1.22; font-weight: 500; white-space: pre-line; }
.catalog-total { font-size: 144px; font-weight: 400; line-height: 1; margin: 36px 0 24px; }
.catalog-total > span { display: inline-block; margin-left: 16px; font-size: 34px; color: var(--muted); }
.model-intro p { max-width: 320px; font-size: 13px; line-height: 1.8; color: var(--muted); }
.provider-index { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); max-width: 360px; margin-top: 32px; gap: 0 24px; }
.provider-index a { display: flex; align-items: center; gap: 8px; min-height: 38px; border-bottom: 1px solid var(--rule); color: var(--ink); font-size: 10px; }
.provider-index a > span { color: var(--muted); }
.provider-index a > svg { margin-left: auto; flex: 0 0 auto; }
.provider-index a:hover { color: var(--accent); }
.text-link { display: inline-flex; align-items: center; gap: 20px; color: var(--accent); font-size: 12px; margin-top: 28px; }
.provider-row { padding: 28px 0 44px; border-top: 1px solid var(--ink); }
.provider-row + .provider-row { margin-top: 36px; }
.provider-meta { display: flex; justify-content: space-between; gap: 12px; font-size: 10px; color: var(--muted); }
.provider-identity { padding: 28px 0; font-size: 72px; font-weight: 500; line-height: 1.1; }
.provider-identity.long-name { font-size: 58px; }
.provider-models { list-style: none; padding: 0; margin: 0; }
.provider-models > li { display: flex; align-items: center; gap: 14px; min-height: 42px; padding: 10px 0; color: var(--muted); }
.provider-models > .model-featured { color: var(--accent); }
.model-tick { width: 7px; height: 7px; border: 1px solid var(--rule); flex: 0 0 auto; }
.model-featured .model-tick { background: var(--accent); border-color: var(--accent); }
.model-id { font-size: 14px; line-height: 1.6; }
.spread-pipeline { padding-top: 32px; padding-bottom: 72px; }
.pipeline-layout { display: grid; grid-template-columns: minmax(0, 0.46fr) minmax(0, 0.54fr); gap: 88px; margin-top: 100px; }
.pipeline-intro { position: sticky; top: 112px; align-self: start; }
.pipeline-intro h2 { max-width: 460px; font-size: 60px; font-weight: 500; line-height: 1.18; white-space: pre-line; }
.pipeline-intro p { max-width: 380px; color: var(--muted); font-size: 14px; line-height: 1.9; margin-top: 32px; }
.pipeline-diagram { display: flex; align-items: center; margin-top: 88px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.pipeline-diagram span { font-size: 13px; }
.pipeline-diagram i { flex: 1; height: 1px; background: var(--rule); margin: 0 18px; }
.pipeline-diagram strong { display: flex; align-items: center; justify-content: center; width: 92px; height: 92px; color: var(--accent); border: 1px solid var(--accent); border-radius: 50%; font-size: 26px; font-weight: 400; }
.pipeline-caption { display: block; margin-top: 28px; font-size: 9px; line-height: 1.8; color: var(--muted); }
.pipeline-step-item { display: grid; grid-template-columns: 80px minmax(0, 1fr); gap: 32px; padding: 32px 0 44px; border-top: 1px solid var(--rule); }
.step-index { font-size: 64px; font-weight: 400; line-height: 1; color: #737580; }
.step-action { color: var(--accent); font-size: 10px; font-weight: 400; }
.step-body h3 { margin: 16px 0; font-size: 27px; font-weight: 400; }
.step-body p { color: var(--muted); line-height: 1.9; font-size: 14px; }
.step-rule-foot { display: block; margin-top: 24px; font-size: 9px; line-height: 1.8; color: var(--muted); }
.spread-semantics { padding-bottom: 96px; }
.semantics-heading { display: grid; grid-template-columns: 1.1fr 0.9fr; align-items: end; gap: 80px; margin: 96px 0 80px; }
.semantics-heading h2 { font-size: 56px; font-weight: 500; line-height: 1.25; white-space: pre-line; }
.semantics-heading > p { max-width: 400px; font-size: 14px; line-height: 1.9; color: var(--muted); }
.semantic-assurance-rail { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-top: 1px solid var(--ink); }
.assurance-cell { padding: 28px 28px 0; border-left: 1px solid var(--rule); }
.assurance-cell:first-child { padding-left: 0; border-left: 0; }
.assurance-cell:last-child { padding-right: 0; }
.assurance-topline { display: flex; align-items: center; justify-content: space-between; font-size: 11px; color: var(--muted); }
.assurance-topline > svg { color: var(--accent); }
.assurance-cell h3 { margin: 56px 0 20px; font-size: 23px; font-weight: 400; }
.assurance-cell p { font-size: 13px; line-height: 1.9; color: var(--muted); }
.spread-control { background: var(--soft); padding-bottom: 96px; }
.control-layout { display: grid; grid-template-columns: minmax(0, 0.4fr) minmax(0, 0.6fr); gap: 80px; margin-top: 96px; }
.control-intro h2 { font-size: 48px; font-weight: 500; line-height: 1.25; }
.control-intro p { margin-top: 32px; max-width: 340px; color: var(--muted); font-size: 14px; line-height: 1.9; }
.control-ledger-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 48px 36px; }
.control-feature-entry { border-top: 1px solid var(--rule); padding-top: 24px; }
.feature-head { display: flex; align-items: center; justify-content: space-between; color: var(--muted); }
.feature-idx { font-size: 11px; }
.control-feature-entry h3 { margin: 32px 0 18px; font-size: 22px; font-weight: 500; }
.control-feature-entry p { color: var(--muted); font-size: 13px; line-height: 1.9; }
.feature-meta-bar { display: flex; flex-wrap: wrap; gap: 8px 16px; color: var(--muted); font-size: 9px; line-height: 1.8; margin-top: 24px; }
.act-ready { padding-bottom: 40px; }
.ready-center { padding: 96px 0 56px; }
.ready-center h2 { margin-top: 24px; font-size: 56px; font-weight: 400; line-height: 1.2; white-space: pre-line; }
.ready-bottom { display: flex; align-items: flex-end; justify-content: space-between; gap: 40px; padding-bottom: 48px; }
.ready-word { font-size: 176px; font-weight: 500; line-height: 1.2; white-space: nowrap; }
.ready-letter { display: inline-block; }
.ready-word > .ready-dot { color: var(--accent); }
.poster-cta-btn { margin-bottom: 10px; flex: 0 0 auto; gap: 64px; }
.ready-capabilities-row { display: flex; justify-content: space-between; gap: 28px; padding-top: 24px; border-top: 1px solid var(--rule); }
.cap-col { color: var(--muted); font-size: 9px; line-height: 1.8; }
.editorial-footer { display: grid; grid-template-columns: 1fr 1fr auto; align-items: start; gap: 32px; padding: 48px var(--gutter); }
.footer-brand { display: flex; flex-direction: column; gap: 12px; }
.footer-brand strong { font-size: 24px; font-weight: 500; overflow-wrap: anywhere; }
.footer-brand > span, .footer-uptime > span, .footer-uptime small { color: var(--muted); font-size: 11px; }
.footer-uptime { display: flex; flex-direction: column; gap: 8px; }
.footer-uptime strong { font-size: 13px; font-weight: 400; }
.footer-links { display: flex; gap: 24px; flex-wrap: wrap; }
.footer-links a { color: var(--muted); font-size: 12px; }
.footer-links a:hover { color: var(--accent); }

@media (min-width: 1800px) {
  .editorial-home { --gutter: max(80px, calc((100% - 1640px) / 2)); }
  .cover-body h1 { font-size: 208px; }
  .cover-deck h2 { font-size: 60px; }
  .provider-identity { font-size: 88px; }
  .provider-identity.long-name { font-size: 68px; }
  .ready-word { font-size: 208px; }
}
@media (max-width: 1279px) {
  .editorial-home { --gutter: 40px; }
  .cover-body h1 { font-size: 144px; }
  .cover-deck h2 { font-size: 40px; }
  .cover-tech-index { gap: 32px; }
  .route-core { gap: 40px; }
  .route-endpoint { font-size: 172px; }
  .route-endpoint > span { font-size: 88px; }
  .route-thesis h2 { font-size: 40px; }
  .protocol-row { grid-template-columns: 24px minmax(160px, 0.7fr) minmax(0, 1.3fr) 20px; gap: 16px; }
  .protocol-row h3 { font-size: 22px; }
  .model-directory, .pipeline-layout, .control-layout { gap: 48px; }
  .model-intro h2, .control-intro h2 { font-size: 40px; }
  .catalog-total { font-size: 112px; }
  .provider-identity { font-size: 58px; }
  .provider-identity.long-name { font-size: 44px; }
  .pipeline-intro h2, .semantics-heading h2 { font-size: 48px; }
  .pipeline-step-item { grid-template-columns: 56px minmax(0, 1fr); gap: 24px; }
  .step-index { font-size: 48px; }
  .assurance-cell { padding-left: 20px; padding-right: 20px; }
  .assurance-cell h3 { font-size: 20px; }
  .ready-word { font-size: 144px; }
}
@media (max-width: 1023px) {
  .editorial-header { padding: 0 24px; }
  .cover-body h1 { font-size: 120px; }
  .cover-deck { max-width: 46%; }
  .cover-tech-index { gap: 24px; }
  .cover-tech-index > div { flex-direction: column; gap: 8px; }
  .route-endpoint { font-size: 140px; }
  .route-endpoint > span { font-size: 72px; }
  .route-thesis h2 { font-size: 36px; }
  .protocol-row { grid-template-columns: 20px minmax(120px, 0.7fr) minmax(0, 1.3fr); }
  .protocol-arrow { display: none; }
  .protocol-row h3 { font-size: 20px; }
  .protocol-path { gap: 10px; }
  .protocol-path code { font-size: 12px; }
  .model-directory { grid-template-columns: minmax(0, 0.4fr) minmax(0, 0.6fr); gap: 36px; }
  .model-intro h2 { font-size: 34px; }
  .provider-index { grid-template-columns: minmax(0, 1fr); }
  .provider-identity { font-size: 48px; }
  .provider-identity.long-name { font-size: 38px; }
  .provider-meta { flex-wrap: wrap; }
  .model-id { font-size: 12px; }
  .pipeline-layout { grid-template-columns: minmax(0, 0.43fr) minmax(0, 0.57fr); gap: 40px; }
  .pipeline-intro h2 { font-size: 40px; }
  .pipeline-diagram strong { width: 64px; height: 64px; }
  .pipeline-diagram i { margin: 0 10px; }
  .pipeline-step-item { grid-template-columns: minmax(0, 1fr); gap: 16px; }
  .semantics-heading { gap: 48px; }
  .semantics-heading h2 { font-size: 40px; }
  .semantic-assurance-rail { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 48px 0; }
  .assurance-cell:nth-child(3) { border-left: 0; padding-left: 0; }
  .assurance-cell:nth-child(2) { padding-right: 0; }
  .control-layout { grid-template-columns: minmax(0, 1fr); }
  .control-intro { display: grid; grid-template-columns: 1fr 1fr; gap: 48px; align-items: start; }
  .control-intro p { margin-top: 0; }
  .ready-word { font-size: 120px; }
  .poster-cta-btn { gap: 32px; }
  .editorial-footer { grid-template-columns: 1fr 1fr; }
  .footer-links { grid-column: 1 / -1; }
}
@media (max-width: 760px) {
  .editorial-home { --gutter: 24px; }
  .editorial-header { gap: 12px; height: 56px; padding: 0 16px; }
  .editorial-brand { gap: 7px; }
  .editorial-brand img { width: 25px; height: 25px; }
  .editorial-brand strong { font-size: 13px; }
  .editorial-actions { gap: 2px; }
  .optional-action { display: none; }
  .enter-link { gap: 8px; padding: 9px 10px; font-size: 11px; }
  .square-action { width: 32px; height: 36px; }
  .locale-wrap :deep(button[title]) { padding-left: 5px; padding-right: 5px; }
  .locale-wrap :deep(button[title] > .sm\:inline) { display: none; }
  .section-pad { padding-top: 24px; }
  .section-topline, .section-foot { font-size: 9px; gap: 12px; align-items: flex-start; }
  .section-topline > :last-child, .section-foot > :last-child { text-align: right; }
  .section-foot { margin-top: 48px; }
  .act-cover { min-height: calc(100svh - 100px); grid-template-rows: 52px 1fr auto; }
  .cover-band { font-size: 8px; gap: 10px; }
  .cover-band > span:last-child { max-width: 145px; text-align: right; }
  .cover-body { padding-top: 12px; padding-bottom: 220px; gap: 28px; justify-content: flex-start; }
  .cover-body h1 { font-size: 76px; font-weight: 500; }
  .cover-deck { max-width: 100%; }
  .cover-deck h2 { font-size: 32px; line-height: 1.2; }
  .cover-deck p { margin-top: 14px; margin-bottom: 20px; font-size: 12px; max-width: 260px; }
  .primary-link { font-size: 12px; min-height: 44px; padding: 12px 16px; gap: 32px; }
  .cover-manifesto { padding: 20px 0; gap: 12px; }
  .cover-tech-index { flex: 1; justify-content: space-between; gap: 10px; }
  .cover-tech-index > div { gap: 7px; }
  .cover-tech-index strong { font-size: 25px; }
  .cover-tech-index span { font-size: 9px; max-width: 76px; line-height: 1.5; }
  .cover-next { width: 32px; height: 32px; }
  .route-core { grid-template-columns: minmax(0, 1fr); gap: 36px; padding: 64px 0 48px; }
  .route-endpoint { font-size: 152px; }
  .route-endpoint > span { font-size: 80px; }
  .route-thesis h2 { font-size: 36px; }
  .route-thesis p { font-size: 13px; margin-top: 20px; }
  .protocol-row { grid-template-columns: 20px minmax(0, 1fr); gap: 12px; padding: 24px 0; min-height: 122px; }
  .protocol-row h3 { font-size: 21px; }
  .protocol-path { grid-column: 1 / -1; }
  .protocol-path code { font-size: 11px; }
  .protocol-method { font-size: 9px; }
  .act-route .section-foot { flex-direction: column; }
  .model-directory { grid-template-columns: minmax(0, 1fr); margin-top: 60px; gap: 48px; }
  .model-intro { position: static; padding-bottom: 0; }
  .model-intro h2 { font-size: 40px; max-width: 320px; }
  .catalog-total { font-size: 100px; margin-top: 32px; margin-bottom: 20px; }
  .catalog-total > span { font-size: 26px; }
  .model-intro p { font-size: 13px; }
  .provider-index { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .provider-row { padding-bottom: 24px; }
  .provider-row + .provider-row { margin-top: 32px; }
  .provider-meta { font-size: 9px; }
  .provider-identity { font-size: 53px; }
  .provider-identity.long-name { font-size: 40px; }
  .model-id { font-size: 12px; }
  .pipeline-layout { grid-template-columns: minmax(0, 1fr); margin-top: 64px; gap: 56px; }
  .pipeline-intro { position: static; }
  .pipeline-intro h2 { font-size: 42px; }
  .pipeline-intro p { font-size: 13px; margin-top: 24px; }
  .pipeline-diagram { margin-top: 40px; max-width: 360px; }
  .pipeline-caption { font-size: 8px; }
  .pipeline-step-item { grid-template-columns: 48px minmax(0, 1fr); gap: 24px; padding-bottom: 32px; }
  .step-index { font-size: 40px; }
  .step-body h3 { font-size: 24px; }
  .step-body p { font-size: 13px; }
  .step-rule-foot { font-size: 8px; }
  .spread-pipeline { padding-bottom: 40px; }
  .semantics-heading { grid-template-columns: minmax(0, 1fr); gap: 24px; margin: 64px 0 40px; }
  .semantics-heading h2 { font-size: 40px; }
  .semantics-heading > p { font-size: 13px; }
  .semantic-assurance-rail { gap: 36px 0; }
  .assurance-cell { padding: 24px 16px 0; }
  .assurance-cell h3 { font-size: 17px; margin-top: 36px; }
  .assurance-cell p { font-size: 12px; }
  .spread-semantics { padding-bottom: 64px; }
  .control-layout { margin-top: 64px; gap: 40px; }
  .control-intro { grid-template-columns: minmax(0, 1fr); gap: 24px; }
  .control-intro h2 { font-size: 38px; max-width: 400px; }
  .control-intro p { font-size: 13px; }
  .control-ledger-grid { gap: 36px 24px; }
  .control-feature-entry h3 { font-size: 19px; margin-top: 24px; }
  .control-feature-entry p { font-size: 12px; }
  .feature-meta-bar { font-size: 8px; }
  .spread-control { padding-bottom: 64px; }
  .ready-center { padding-top: 64px; padding-bottom: 40px; }
  .ready-center h2 { font-size: 38px; }
  .ready-bottom { align-items: flex-start; flex-direction: column; gap: 28px; padding-bottom: 28px; }
  .ready-word { font-size: 82px; }
  .poster-cta-btn { gap: 64px; }
  .ready-capabilities-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
  .cap-col { font-size: 8px; }
  .editorial-footer { padding-top: 32px; padding-bottom: 32px; gap: 28px 16px; }
  .footer-brand strong { font-size: 22px; }
  .footer-uptime strong { font-size: 11px; line-height: 1.6; }
  .footer-links { gap: 24px; }
}
@media (max-width: 374px) {
  .editorial-home { --gutter: 20px; }
  .editorial-header { padding: 0 12px; gap: 8px; }
  .cover-body h1 { font-size: 68px; }
  .cover-tech-index span { max-width: 65px; font-size: 8px; }
  .provider-identity { font-size: 48px; }
  .provider-identity.long-name { font-size: 36px; }
  .ready-word { font-size: 70px; }
  .assurance-cell h3 { font-size: 15px; }
}
@media (max-height: 800px) and (min-width: 761px) {
  .model-intro, .pipeline-intro { position: static; }
  .cover-body h1 { font-size: 120px; }
  .cover-body { gap: 32px; padding-bottom: 32px; }
  .cover-deck h2 { font-size: 36px; }
}
@media (max-height: 740px) and (max-width: 760px) {
  .act-cover { grid-template-rows: 44px 1fr auto; }
  .cover-body { gap: 18px; padding-top: 8px; padding-bottom: 160px; }
  .cover-body h1 { font-size: 58px; }
  .cover-deck h2 { font-size: 26px; }
  .cover-deck p { font-size: 11px; margin-top: 10px; margin-bottom: 14px; }
}
/* 动画模式保留同一份正文，图形层持续连接相邻章节。 */
.journey-track { position: relative; }
.journey-stage { position: sticky; top: 64px; height: calc(100svh - 64px); overflow: hidden; isolation: isolate; }
.journey-stage > section { position: absolute; top: 0; left: 0; z-index: 2; width: 100%; min-height: calc(100svh - 104px); background: transparent; will-change: transform; }
.journey-stage > section > :not(.routing-artwork) { opacity: var(--journey-copy, 1); }
.journey-stage .act-cover { isolation: auto; }
.journey-stage .route-primary, .journey-stage .catalog-primary, .journey-stage .pipeline-diagram strong { opacity: 0; }
.journey-stage [data-journey-entry]:not([data-journey-exit]) { opacity: var(--journey-entry-title, 1); }
.journey-stage [data-journey-exit]:not([data-journey-entry]) { opacity: var(--journey-exit-title, 1); }
.journey-stage [data-journey-entry][data-journey-exit] { opacity: calc(var(--journey-entry-title, 1) * var(--journey-exit-title, 1)); }
.journey-stage [data-journey-item] { opacity: var(--journey-item-copy, 1); }
.journey-stage .poster-cta-btn { background: transparent; }
.journey-stage .poster-cta-btn:hover { background: #2447b8; }
.journey-stage .cover-body { transform: translate3d(calc(var(--journey-exit) * -120px), calc(var(--journey-exit) * -64px), 0) scale(calc(1 - var(--journey-exit) * 0.12)); transform-origin: 75% 60%; }
.journey-stage .cover-manifesto { transform: translateY(calc(var(--journey-exit) * 64px)); }
.journey-stage .cover-band, .journey-stage .section-topline { transform: translateX(calc(var(--journey-enter) * 36px - var(--journey-exit) * 36px)); }
.journey-stage .route-thesis { transform: translate3d(calc(var(--journey-enter) * 160px - var(--journey-exit) * 100px), 0, 0); }
.journey-stage .protocol-row { transform: translate3d(calc(var(--journey-enter) * 100px - var(--journey-exit) * 80px), calc(var(--journey-enter) * 30px), 0) scaleX(calc(1 - var(--journey-exit) * 0.1)); transform-origin: left; }
.journey-stage .protocol-row:nth-child(even) { transform: translate3d(calc(var(--journey-enter) * 150px + var(--journey-exit) * 80px), calc(var(--journey-enter) * 50px), 0); }
.journey-stage .model-intro, .journey-stage .pipeline-intro { position: static; transform: translate3d(calc(var(--journey-enter) * -64px - var(--journey-exit) * 64px), var(--journey-read), 0); }
.journey-stage .provider-row { transform: translate3d(calc(var(--journey-enter) * 120px + var(--journey-exit) * 80px), calc(var(--journey-enter) * 40px - var(--journey-exit) * 30px), 0) scale(calc(1 - var(--journey-enter) * 0.08 - var(--journey-exit) * 0.08)); transform-origin: left top; }
.journey-stage .pipeline-step-item { transform: translate3d(calc(var(--journey-enter) * 100px - var(--journey-exit) * 60px), calc(var(--journey-enter) * 48px), 0); }
.journey-stage .semantics-heading, .journey-stage .control-intro { transform: translate3d(calc(var(--journey-enter) * -80px), calc(var(--journey-exit) * -64px), 0); }
.journey-stage .assurance-cell, .journey-stage .control-feature-entry { transform: translate3d(calc(var(--journey-enter) * 48px), calc(var(--journey-enter) * 64px - var(--journey-exit) * 48px), 0) scale(calc(1 - var(--journey-enter) * 0.1 - var(--journey-exit) * 0.1)); transform-origin: left top; }
.journey-stage .ready-center { transform: translateY(calc(var(--journey-enter) * -80px)); }
.journey-stage .ready-word { transform: translateX(calc(var(--journey-enter) * -120px)); }
.journey-stage .section-foot, .journey-stage .ready-capabilities-row { transform: translateY(calc(var(--journey-enter) * 40px + var(--journey-exit) * 40px)); }
.journey-chapters { position: absolute; z-index: 5; bottom: 0; left: 0; right: 0; display: flex; height: 40px; padding: 0 24px; border-top: 1px solid var(--rule); background: var(--paper); }
.journey-chapters a { min-width: 0; flex: 1; display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 10px; border-top: 2px solid transparent; }
.journey-chapters a[aria-current] { color: var(--accent); border-top-color: var(--accent); }
.journey-chapters strong { font-size: 10px; font-weight: 400; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.journey-stage.journey-measuring > section > *, .journey-measuring :is(.model-intro, .pipeline-intro, .route-thesis, .protocol-row, .provider-row, .pipeline-step-item, .semantics-heading, .control-intro, .assurance-cell, .control-feature-entry, .ready-word) { transform: none !important; }
@media (max-width: 760px) {
  .journey-stage { top: 56px; height: calc(100svh - 56px); }
  .journey-stage > section { min-height: calc(100svh - 96px); }
  .journey-chapters { padding: 0 20px; }
  .journey-chapters a { justify-content: center; font-size: 10px; }
  .journey-chapters strong { display: none; }
}
@media (prefers-reduced-motion: reduce) {
  .model-intro, .pipeline-intro { position: static; }
}
</style>
