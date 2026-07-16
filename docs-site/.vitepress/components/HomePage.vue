<script setup lang="ts">
import {
  ArrowRight,
  CircleAlert,
  Gauge,
  KeyRound,
  Search,
  Terminal,
  ExternalLink
} from '@lucide/vue'

function openSearch() {
  const button = document.querySelector<HTMLButtonElement>(
    '.VPNavBarSearch button, .DocSearch-Button'
  )
  button?.click()
}

const tasks = [
  {
    title: '5 分钟接入',
    detail: '创建 Key 并发送第一条 API 请求',
    href: '/getting-started/',
    icon: KeyRound,
    tone: 'mint'
  },
  {
    title: '配置客户端',
    detail: 'Claude Code、Codex CLI、OpenCode 与 Gemini',
    href: '/clients/',
    icon: Terminal,
    tone: 'blue'
  },
  {
    title: '理解套餐',
    detail: '计费方式、续费和 5h / 7d / 30d 额度',
    href: '/plans/',
    icon: Gauge,
    tone: 'amber'
  },
  {
    title: '按错误排查',
    detail: '从状态码、错误码和请求 ID 开始定位',
    href: '/troubleshooting/',
    icon: CircleAlert,
    tone: 'coral'
  }
]
</script>

<template>
  <main class="home-shell">
    <section class="home-intro" aria-labelledby="home-title">
      <div class="home-brand">
        <img src="/logo.png" alt="OnprsCodexApi Logo" width="72" height="70">
        <div>
          <p class="home-kicker">用户帮助中心</p>
          <h1 id="home-title">OnprsCodexApi 文档</h1>
        </div>
      </div>
      <p class="home-summary">接入 API、配置开发客户端、查看套餐额度，并按错误码快速定位问题。</p>

      <button class="home-search" type="button" @click="openSearch">
        <Search :size="20" aria-hidden="true" />
        <span>搜索错误码、客户端或套餐额度</span>
        <kbd>Ctrl K</kbd>
      </button>

      <div class="home-endpoints" aria-label="服务入口">
        <span>API Base URL</span>
        <code>https://api.onprs.top/v1</code>
        <a href="https://api.onprs.top/" target="_blank" rel="noreferrer">
          打开控制台
          <ExternalLink :size="15" aria-hidden="true" />
        </a>
      </div>
    </section>

    <section class="home-tasks" aria-labelledby="task-title">
      <div class="section-heading">
        <p>常用任务</p>
        <h2 id="task-title">从你要完成的事情开始</h2>
      </div>
      <div class="task-grid">
        <a v-for="task in tasks" :key="task.href" :href="task.href" class="task-card" :data-tone="task.tone">
          <span class="task-icon"><component :is="task.icon" :size="21" aria-hidden="true" /></span>
          <span class="task-copy">
            <strong>{{ task.title }}</strong>
            <small>{{ task.detail }}</small>
          </span>
          <ArrowRight class="task-arrow" :size="18" aria-hidden="true" />
        </a>
      </div>
    </section>

    <section class="home-reference" aria-labelledby="reference-title">
      <div class="section-heading">
        <p>快速参考</p>
        <h2 id="reference-title">接入时先确认这四项</h2>
      </div>
      <div class="reference-table" role="table" aria-label="API 快速参考">
        <div role="row"><strong role="cell">Base URL</strong><code role="cell">https://api.onprs.top/v1</code></div>
        <div role="row"><strong role="cell">认证</strong><span role="cell">API Key，通常使用 <code>Authorization: Bearer</code></span></div>
        <div role="row"><strong role="cell">模型</strong><span role="cell">以控制台“可用渠道”和模型列表为准</span></div>
        <div role="row"><strong role="cell">支持</strong><a role="cell" href="mailto:839097298@qq.com">839097298@qq.com</a></div>
      </div>
    </section>

    <section class="home-updates" aria-labelledby="updates-title">
      <div>
        <p>最近更新</p>
        <h2 id="updates-title">首版用户文档</h2>
        <span>覆盖四类 API 协议、五类客户端、套餐滚动额度和常见错误。</span>
      </div>
      <a href="/changelog/">查看更新记录 <ArrowRight :size="16" aria-hidden="true" /></a>
    </section>
  </main>
</template>
