import { defineConfig } from 'vitepress'

const consoleUrl = 'https://cdn-api.onprs.online/'

export default defineConfig({
  lang: 'zh-CN',
  title: 'OnprsCodexApi 文档',
  description: 'OnprsCodexApi 接入、客户端配置、API 使用与问题排查',
  base: '/',
  srcExclude: ['README.md', 'DEPLOYMENT.md'],
  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: false,
  appearance: true,
  head: [
    ['link', { rel: 'icon', href: '/favicon.ico' }],
    ['meta', { name: 'theme-color', content: '#75639a' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'OnprsCodexApi 文档' }],
    ['meta', { property: 'og:description', content: 'OnprsCodexApi 接入、客户端配置与使用文档' }],
    ['meta', { property: 'og:image', content: '/og-image.png' }],
    ['meta', { name: 'robots', content: 'index,follow' }]
  ],
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      title: 'OnprsCodexApi 文档',
      description: 'OnprsCodexApi 接入与使用文档'
    }
  },
  themeConfig: {
    logo: '/logo.png',
    siteTitle: 'OnprsCodexApi 文档',
    nav: [
      { text: '快速开始', link: '/getting-started/' },
      { text: '返回控制台', link: consoleUrl }
    ],
    sidebar: [
      {
        text: '快速开始',
        collapsed: true,
        items: [
          { text: '接入概览', link: '/getting-started/' },
          { text: '注册与账号安全', link: '/getting-started/account' },
          { text: '创建 API Key', link: '/getting-started/api-key' },
          { text: '发送第一条请求', link: '/getting-started/first-request' },
          { text: '查看模型与用量', link: '/getting-started/console' }
        ]
      },
      {
        text: 'API 使用',
        collapsed: true,
        items: [
          { text: 'API 总览', link: '/api/' },
          { text: '协议与端点', link: '/api/protocols' },
          { text: '模型与映射', link: '/api/models' },
          { text: '流式、工具与结构化输出', link: '/api/capabilities' },
          { text: '响应、用量与请求 ID', link: '/api/responses' }
        ]
      },
      {
        text: '客户端',
        collapsed: true,
        items: [
          { text: '选择客户端', link: '/clients/' },
          { text: 'Claude Code', link: '/clients/claude-code' },
          { text: 'Codex CLI', link: '/clients/codex-cli' },
          { text: 'OpenCode', link: '/clients/opencode' },
          { text: 'Gemini CLI', link: '/clients/gemini' },
          { text: 'IDE、GUI 与 OpenAI SDK', link: '/clients/openai-compatible' }
        ]
      },
      {
        text: '套餐与额度',
        collapsed: true,
        items: [
          { text: '计费方式', link: '/plans/' },
          { text: '5h / 7d / 30d 额度', link: '/plans/rolling-quotas' },
          { text: '购买、续费与权益快照', link: '/plans/lifecycle' },
          { text: '订单、支付与兑换码', link: '/plans/orders' }
        ]
      },
      {
        text: '错误排查',
        collapsed: true,
        items: [
          { text: '统一排错流程', link: '/troubleshooting/' },
          { text: 'HTTP 状态码', link: '/troubleshooting/status-codes' },
          { text: 'Key、权限与模型', link: '/troubleshooting/auth-model' },
          { text: '余额、套餐与限流', link: '/troubleshooting/quota' },
          { text: '超时、流与工具调用', link: '/troubleshooting/streaming' },
          { text: 'DNS、TLS 与代理', link: '/troubleshooting/network' },
          { text: '支付与订单', link: '/troubleshooting/payment' }
        ]
      },
      {
        text: '账号与安全',
        collapsed: true,
        items: [
          { text: 'API Key 安全', link: '/account/' },
          { text: '联系支持', link: '/account/support' },
          { text: '条款、隐私与合规', link: '/account/legal' }
        ]
      },
      {
        text: '问题解答',
        collapsed: true,
        items: [{ text: 'FAQ', link: '/faq/' }]
      },
      {
        text: '文档更新',
        collapsed: true,
        items: [{ text: '更新记录', link: '/changelog/' }]
      }
    ],
    search: {
      provider: 'local',
      options: {
        translations: {
          button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
          modal: {
            noResultsText: '没有找到相关内容',
            resetButtonTitle: '清除查询',
            footer: {
              selectText: '选择',
              navigateText: '切换',
              closeText: '关闭'
            }
          }
        }
      }
    },
    outline: {
      level: [2, 3],
      label: '本页目录'
    },
    docFooter: {
      prev: '上一篇',
      next: '下一篇'
    },
    lastUpdated: {
      text: '最后更新于',
      formatOptions: {
        dateStyle: 'medium',
        timeStyle: 'short'
      }
    },
    darkModeSwitchLabel: '切换深色模式',
    lightModeSwitchTitle: '切换浅色模式',
    darkModeSwitchTitle: '切换深色模式',
    sidebarMenuLabel: '文档目录',
    returnToTopLabel: '返回顶部',
    langMenuLabel: '切换语言',
    externalLinkIcon: true,
    footer: {
      message: 'OnprsCodexApi 使用文档',
      copyright: '支持邮箱：839097298@qq.com'
    }
  }
})
