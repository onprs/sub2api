<script setup lang="ts">
import {
  ArrowRight,
  BookOpen,
  CircleAlert,
  CircleHelp,
  ExternalLink,
  Gauge,
  KeyRound,
  Search,
  ShieldCheck,
  Terminal
} from '@lucide/vue'

function openSearch() {
  const button = document.querySelector<HTMLButtonElement>(
    '.VPNavBarSearch button, .DocSearch-Button'
  )
  button?.click()
}

const tasks = [
  {
    title: '开始接入',
    detail: '创建 Key 并完成首次请求',
    href: '/getting-started/',
    icon: KeyRound
  },
  {
    title: '配置客户端',
    detail: 'Claude Code、Codex CLI 等',
    href: '/clients/',
    icon: Terminal
  },
  {
    title: '套餐与额度',
    detail: '查看计费、续费与滚动额度',
    href: '/plans/',
    icon: Gauge
  },
  {
    title: '错误排查',
    detail: '按状态码和错误码定位',
    href: '/troubleshooting/',
    icon: CircleAlert
  }
]

const resources = [
  { title: 'API 参考', detail: '协议、端点与响应', href: '/api/', icon: BookOpen },
  { title: '问题解答', detail: '接入与套餐问题', href: '/faq/', icon: CircleHelp },
  { title: '账号安全', detail: 'Key 管理与支持', href: '/account/', icon: ShieldCheck }
]
</script>

<template>
  <main class="home-shell">
    <section class="home-intro" aria-labelledby="home-title">
      <div class="home-brand">
        <img src="/logo.png" alt="OnprsCodexApi Logo" width="68" height="68">
        <div>
          <h1 id="home-title">OnprsCodexApi 文档</h1>
          <p class="home-summary">查找接入配置、API 使用、套餐额度和故障处理。</p>
        </div>
      </div>

      <button class="home-search" type="button" aria-label="搜索文档内容" @click="openSearch">
        <Search :size="20" aria-hidden="true" />
        <span>搜索文档</span>
        <kbd>Ctrl K</kbd>
      </button>

      <div class="home-endpoints" aria-label="服务入口">
        <span>API Base URL</span>
        <code>https://cdn-api.onprs.online/v1</code>
        <a href="https://cdn-api.onprs.online/" target="_blank" rel="noreferrer">
          打开控制台
          <ExternalLink :size="15" aria-hidden="true" />
        </a>
      </div>
    </section>

    <section class="home-tasks" aria-labelledby="task-title">
      <div class="section-heading">
        <h2 id="task-title">快速开始</h2>
      </div>
      <div class="task-grid">
        <a v-for="task in tasks" :key="task.href" :href="task.href" class="task-card">
          <span class="task-icon"><component :is="task.icon" :size="20" aria-hidden="true" /></span>
          <span class="task-copy">
            <strong>{{ task.title }}</strong>
            <small>{{ task.detail }}</small>
          </span>
          <ArrowRight class="task-arrow" :size="17" aria-hidden="true" />
        </a>
      </div>
    </section>

    <section class="home-secondary">
      <div class="home-reference" aria-labelledby="reference-title">
        <div class="section-heading">
          <h2 id="reference-title">接入信息</h2>
        </div>
        <dl class="reference-list">
          <div>
            <dt>Base URL</dt>
            <dd><code>https://cdn-api.onprs.online/v1</code></dd>
          </div>
          <div>
            <dt>认证</dt>
            <dd>API Key</dd>
          </div>
          <div>
            <dt>模型</dt>
            <dd><a href="/api/models">拉取模型列表</a></dd>
          </div>
        </dl>
      </div>

      <nav class="home-resources" aria-labelledby="resources-title">
        <div class="section-heading">
          <h2 id="resources-title">更多文档</h2>
        </div>
        <a v-for="resource in resources" :key="resource.href" :href="resource.href">
          <component :is="resource.icon" :size="18" aria-hidden="true" />
          <span><strong>{{ resource.title }}</strong><small>{{ resource.detail }}</small></span>
          <ArrowRight :size="16" aria-hidden="true" />
        </a>
      </nav>
    </section>
  </main>
</template>
