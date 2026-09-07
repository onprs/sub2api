export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '一次提交多条提示词，任务完成后可统一下载图片结果'
  },
  // Home Page
  home: {
    viewOnGithub: '在 GitHub 上查看',
    viewDocs: '查看文档',
    docs: '文档',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    uptime: {
      label: '累计运行时间',
      startDate: '自今年 5 月 1 日起',
      value: '{days} 天 {hours} 小时 {minutes} 分钟'
    },
    prototype: {
      navigation: '首页导航',
      gatewayOnline: '网关在线',
      routeTo: '路由至',
      allOperational: '所有服务正常',
      endpoints: '端点',
      providers: '服务商',
      ready: '可以发送',
      liveRoute: '实时路由',
      routingEdition: '统一路由层',
      oneEndpoint: '统一 API 根地址',
      manyModels: '多种模型',
      routeLayer: '模型路由',
      editorialCoverTitle: '多源模型。\n一个入口。',
      editorialRouteTitle: '四种协议。\n同一条路。',
      editorialDescription: '一个 API 根地址承载四类生成协议，按模型接入多来源 AI 能力',
      editorialModels: '多源模型。\n统一接入。',
      editorialEndingTitle: '让下一条请求，\n抵达更多可能。',
      editorialCatalogCount: '模型精选',
      editorialCatalogDescription: '六大来源的精选模型目录。实际可调用模型取决于账号分组与模型映射。',
      editorialNavigation: {
        sections: '页面章节',
        protocols: '协议接入',
        models: '模型目录',
        architecture: '请求架构'
      },
      editorialSemantics: {
        tag: '协议语义',
        title: '不只连接。\n更保留语义。',
        description: '在目标协议支持的范围内对齐响应、工具与推理信息，让应用关注模型能力，让网关处理协议差异。'
      },
      editorialProtocols: '已支持生成入口',
      editorialCatalogLead: '精选真实模型 ID · {count} 项',
      editorialCatalogSummary: '模型目录摘要',
      editorialSelectedModels: '精选模型 ID',
      editorialSummaryCatalog: '个精选模型',
      editorialSummaryProviders: '模型来源',
      editorialSummaryProtocols: '个生成入口',
      editorialCoverIndex: {
        surfaces: '标准生成入口',
        models: '精选模型 ID',
        semantic: '流式传输与工具调用对齐',
        routing: '模型感知健康调度与故障切换'
      },
      editorialAssurance: {
        stream: 'SSE 增量流式交付与连接生命周期管理',
        tools: '结构化工具调用与参数语义保持',
        reasoning: '在支持的目标协议中保留推理内容与必要元数据',
        usage: '提取上游 Token、缓存用量与请求计费数据'
      },
      editorialPipeline: {
        tag: '核心请求链路',
        title: '一条请求。\n四层处理。',
        subtitle: '从客户端接入到多模型执行，保持协议语义、调度可用性与用量可追溯',
        step1: {
          code: '01',
          name: '协议接入',
          action: 'RECEIVE',
          desc: '接收 Responses、Chat、Messages 与 GenAI 四类标准入口请求，校验 API Key 与配额。'
        },
        step2: {
          code: '02',
          name: '语义转换',
          action: 'NORMALIZE',
          desc: '在目标协议可表达范围内规范化消息、系统提示词、工具调用及推理内容。'
        },
        step3: {
          code: '03',
          name: '模型调度',
          action: 'ORCHESTRATE',
          desc: '根据模型与账号可调度状态选择上游，并在首个语义输出前执行失败切换。'
        },
        step4: {
          code: '04',
          name: '交付计费',
          action: 'DELIVER',
          desc: '以 SSE 增量交付响应，并记录单次请求 Token、缓存用量、费用与审计信息。'
        }
      },
      editorialControl: {
        tag: '用户控制面',
        title: '每一次调用，都有清晰上下文',
        subtitle: '围绕密钥、请求、价格与身份安全，提供可追踪的开发者控制面',
        keys: {
          title: 'API 密钥与配额',
          desc: '创建独立 API Key，并按使用场景设置限额与路由范围。',
          summary: '独立密钥 / 限额 / 路由'
        },
        usage: {
          title: '用量与请求审计',
          desc: '按请求查看 Token、耗时、费用与 Request ID。',
          descWithErrors: '按请求查看 Token、耗时、费用与 Request ID，并定位错误请求详情。',
          summary: 'TOKEN / REQUEST ID / 用量',
          summaryWithErrors: 'TOKEN / REQUEST ID / 错误详情'
        },
        pricing: {
          title: '模型价格与余额',
          desc: '查看模型单位价格与请求计费结果。',
          descWithPayment: '查看模型单位价格与请求计费结果，并通过在线支付补充余额。',
          descPayment: '通过在线支付补充余额，并持续查看余额变化。',
          summary: '模型单价 / 计费记录',
          summaryWithPayment: '模型单价 / 在线充值 / 余额',
          summaryPayment: '在线充值 / 余额'
        },
        security: {
          title: '身份验证与安全',
          desc: '邮箱验证与 TOTP 双因素认证共同保护账号。',
          descEmail: '通过邮箱验证保护账号身份。',
          descTotp: '通过 TOTP 双因素认证保护账号。',
          summary: '邮箱验证 / TOTP 2FA',
          summaryEmail: '邮箱验证',
          summaryTotp: 'TOTP 2FA'
        }
      },
      state: '状态'
    },
    getStarted: '立即开始',
    goToDashboard: '进入控制台',
    // 新增：面向用户的价值主张
    heroSubtitle: '一个密钥，畅用多个 AI 模型',
    heroDescription: '无需管理多个订阅账号，一站式接入 Claude、GPT、Gemini 等主流 AI 服务',
    tags: {
      subscriptionToApi: '订阅转 API',
      stickySession: '会话保持',
      realtimeBilling: '按量计费'
    },
    // 用户痛点区块
    painPoints: {
      title: '你是否也遇到这些问题？',
      items: {
        expensive: {
          title: '订阅费用高',
          desc: '每个 AI 服务都要单独订阅，每月支出越来越多'
        },
        complex: {
          title: '多账号难管理',
          desc: '不同平台的账号、密钥分散各处，管理起来很麻烦'
        },
        unstable: {
          title: '服务不稳定',
          desc: '单一账号容易触发限制，影响正常使用'
        },
        noControl: {
          title: '用量无法控制',
          desc: '不知道钱花在哪了，也无法限制团队成员的使用'
        }
      }
    },
    // 解决方案区块
    solutions: {
      title: '我们帮你解决',
      subtitle: '简单三步，开始省心使用 AI'
    },
    features: {
      unifiedGateway: '一键接入',
      unifiedGatewayDesc: '获取一个 API 密钥，即可调用所有已接入的 AI 模型，无需分别申请。',
      multiAccount: '稳定可靠',
      multiAccountDesc: '智能调度多个上游账号，自动切换和负载均衡，告别频繁报错。',
      balanceQuota: '用多少付多少',
      balanceQuotaDesc: '按实际使用量计费，支持设置配额上限，团队用量一目了然。'
    },
    // 优势对比
    comparison: {
      title: '为什么选择我们？',
      headers: {
        feature: '对比项',
        official: '官方订阅',
        us: '本平台'
      },
      items: {
        pricing: {
          feature: '付费方式',
          official: '固定月费，用不完也付',
          us: '按量付费，用多少付多少'
        },
        models: {
          feature: '模型选择',
          official: '单一服务商',
          us: '多模型随意切换'
        },
        management: {
          feature: '账号管理',
          official: '每个服务单独管理',
          us: '统一密钥，一站管理'
        },
        stability: {
          feature: '服务稳定性',
          official: '单账号易触发限制',
          us: '多账号池，自动切换'
        },
        control: {
          feature: '用量控制',
          official: '无法限制',
          us: '可设配额、查明细'
        }
      }
    },
    providers: {
      title: '已支持的 AI 模型',
      description: '一个 API，多种选择',
      supported: '已支持',
      soon: '即将推出',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: '更多'
    },
    // CTA 区块
    cta: {
      title: '准备好开始了吗？',
      description: '注册即可获得免费试用额度，体验一站式 AI 服务',
      button: '免费注册'
    },
    footer: {
      allRightsReserved: '保留所有权利。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查询',
    subtitle: '输入您的 API Key 以查看实时消费金额与使用状态',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查询',
    querying: '查询中...',
    privacyNote: '您的 Key 仅在浏览器本地处理，不会被存储',
    dateRange: '统计范围:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自定义',
    apply: '应用',
    used: '已使用',
    detailInfo: '详细信息',
    tokenStats: 'Token 统计',
    dailyDetail: '按日明细',
    modelStats: '模型用量统计',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '请求数',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    cacheCreationTokens: '缓存创建',
    cacheReadTokens: '缓存读取',
    cacheWriteTokens: '缓存写入',
    totalTokens: '总 Tokens',
    cost: '费用',
    // Status
    quotaMode: 'Key 限额模式',
    walletBalance: '钱包余额',
    // Ring card titles
    totalQuota: '总额度',
    limit5h: '5 小时限额',
    limitDaily: '日限额',
    limit7d: '7 天限额',
    limitWeekly: '周限额',
    limitMonthly: '月限额',
    // Detail rows
    remainingQuota: '剩余额度',
    expiresAt: '过期时间',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用额度',
    resetNow: '即将重置',
    subscriptionType: '订阅类型',
    subscriptionExpires: '订阅到期',
    // Usage stat cells
    todayRequests: '今日请求',
    todayInputTokens: '今日输入',
    todayOutputTokens: '今日输出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日缓存创建',
    todayCacheRead: '今日缓存读取',
    todayCost: '今日费用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累计请求',
    totalInputTokens: '累计输入',
    totalOutputTokens: '累计输出',
    totalTokensLabel: '累计 Tokens',
    totalCacheCreation: '累计缓存创建',
    totalCacheRead: '累计缓存读取',
    totalCost: '累计费用',
    avgDuration: '平均耗时',
    // Messages
    enterApiKey: '请输入 API Key',
    querySuccess: '查询成功',
    queryFailed: '查询失败',
    queryFailedRetry: '查询失败，请稍后重试',
    noDailyUsage: '暂无按日用量数据',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API 安装向导',
    description: '配置您的 Sub2API 实例',
    database: {
      title: '数据库配置',
      description: '连接到您的 PostgreSQL 数据库',
      host: '主机',
      port: '端口',
      username: '用户名',
      password: '密码',
      databaseName: '数据库名称',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密码',
      ssl: {
        disable: '禁用',
        require: '要求',
        verifyCa: '验证 CA',
        verifyFull: '完全验证'
      }
    },
    redis: {
      title: 'Redis 配置',
      description: '连接到您的 Redis 服务器',
      host: '主机',
      port: '端口',
      username: '用户名（可选）',
      password: '密码（可选）',
      database: '数据库',
      usernamePlaceholder: '默认用户留空',
      passwordPlaceholder: '密码',
      enableTls: '启用 TLS',
      enableTlsHint: '连接 Redis 时使用 TLS（公共 CA 证书）'
    },
    admin: {
      title: '管理员账户',
      description: '创建您的管理员账户',
      email: '邮箱',
      password: '密码',
      confirmPassword: '确认密码',
      passwordPlaceholder: '至少 8 个字符',
      confirmPasswordPlaceholder: '确认密码',
      passwordMismatch: '密码不匹配'
    },
    ready: {
      title: '准备安装',
      description: '检查您的配置并完成安装',
      database: '数据库',
      redis: 'Redis',
      adminEmail: '管理员邮箱'
    },
    status: {
      testing: '测试中...',
      success: '连接成功',
      testConnection: '测试连接',
      installing: '安装中...',
      completeInstallation: '完成安装',
      completed: '安装完成！',
      redirecting: '正在跳转到登录页面...',
      restarting: '服务正在重启，请稍候...',
      timeout: '服务重启时间超出预期，请手动刷新页面。'
    }
  },

  // Common
}
