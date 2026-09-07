export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in one job and download the generated images when complete'
  },
  // Home Page
  home: {
    viewOnGithub: 'View on GitHub',
    viewDocs: 'View Documentation',
    docs: 'Docs',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    uptime: {
      label: 'Uptime',
      startDate: 'Since May 1 this year',
      value: '{days}d {hours}h {minutes}m'
    },
    prototype: {
      navigation: 'Home navigation',
      gatewayOnline: 'Gateway online',
      routeTo: 'Route to',
      allOperational: 'All systems operational',
      endpoints: 'Endpoints',
      providers: 'Providers',
      ready: 'Ready to send',
      liveRoute: 'Live route',
      routingEdition: 'Unified routing layer',
      oneEndpoint: 'One API root',
      manyModels: 'Many models',
      routeLayer: 'Model routing',
      editorialCoverTitle: 'Many models.\nOne gateway.',
      editorialRouteTitle: 'Four protocols.\nOne way in.',
      editorialDescription: 'One API root carries four generation protocols and routes each model to the right upstream',
      editorialModels: 'Many models.\nOne connection.',
      editorialEndingTitle: 'Your next request.\nMore possibilities.',
      editorialCatalogCount: 'Model Selection',
      editorialCatalogDescription: 'A curated selection from six model sources. Models available to each request depend on your account group and model mapping.',
      editorialNavigation: {
        sections: 'Page sections',
        protocols: 'Connectivity',
        models: 'Models',
        architecture: 'Architecture'
      },
      editorialSemantics: {
        tag: 'Protocol Semantics',
        title: 'Beyond connection.\nPreserving meaning.',
        description: 'Align responses, tools, and reasoning within the target protocol’s capabilities. Your application focuses on models; the gateway handles protocol differences.'
      },
      editorialProtocols: 'Standard generation surfaces',
      editorialCatalogLead: '{count} Selected Model IDs',
      editorialCatalogSummary: 'Model catalog summary',
      editorialSelectedModels: 'Selected Model IDs',
      editorialSummaryCatalog: 'Selected Models',
      editorialSummaryProviders: 'Model Sources',
      editorialSummaryProtocols: 'Generation Surfaces',
      editorialCoverIndex: {
        surfaces: 'Standard API Surfaces',
        models: 'Selected Model IDs',
        semantic: 'Streaming & Tool Calling Alignment',
        routing: 'Model-Aware Health Routing & Failover'
      },
      editorialAssurance: {
        stream: 'Incremental SSE delivery and connection lifecycle management',
        tools: 'Structured tool calls with argument fidelity',
        reasoning: 'Reasoning and required metadata preserved where the target supports them',
        usage: 'Upstream token, cached usage, and request cost extraction'
      },
      editorialPipeline: {
        tag: 'Request Lifecycle',
        title: 'One request.\nFour layers.',
        subtitle: 'From client ingress to multi-model execution, preserving protocol semantics, routing health, and usage traceability',
        step1: {
          code: '01',
          name: 'Ingest',
          action: 'RECEIVE',
          desc: 'Accepts Responses, Chat, Messages, and GenAI requests with instant API key validation.'
        },
        step2: {
          code: '02',
          name: 'Transform',
          action: 'NORMALIZE',
          desc: 'Normalizes messages, system prompts, tool calls, and reasoning within the target protocol’s capabilities.'
        },
        step3: {
          code: '03',
          name: 'Route',
          action: 'ORCHESTRATE',
          desc: 'Selects an upstream by model and account schedulability, with failover before the first semantic output.'
        },
        step4: {
          code: '04',
          name: 'Deliver',
          action: 'DELIVER',
          desc: 'Delivers incremental SSE while recording per-request tokens, cached usage, cost, and audit context.'
        }
      },
      editorialControl: {
        tag: 'Control Plane',
        title: 'Every Request, In Context',
        subtitle: 'A traceable developer control plane for keys, requests, pricing, and account security',
        keys: {
          title: 'API Keys & Quotas',
          desc: 'Create dedicated API keys and scope quotas and routing for each workload.',
          summary: 'ISOLATED KEYS / QUOTAS / ROUTING'
        },
        usage: {
          title: 'Usage & Request Audit',
          desc: 'Inspect tokens, duration, cost, and Request ID for each request.',
          descWithErrors: 'Inspect tokens, duration, cost, and Request ID, with details for failed requests.',
          summary: 'TOKENS / REQUEST ID / USAGE',
          summaryWithErrors: 'TOKENS / REQUEST ID / ERROR DETAIL'
        },
        pricing: {
          title: 'Model Rates & Balance',
          desc: 'Review model unit rates and per-request billing results.',
          descWithPayment: 'Review model unit rates and billing results, then add balance through online payment.',
          descPayment: 'Add balance through online payment and track balance changes.',
          summary: 'MODEL RATES / BILLING RECORDS',
          summaryWithPayment: 'MODEL RATES / ONLINE TOP-UP / BALANCE',
          summaryPayment: 'ONLINE TOP-UP / BALANCE'
        },
        security: {
          title: 'Identity & Security',
          desc: 'Email verification and TOTP two-factor authentication protect the account together.',
          descEmail: 'Email verification protects account identity.',
          descTotp: 'TOTP two-factor authentication protects the account.',
          summary: 'EMAIL VERIFICATION / TOTP 2FA',
          summaryEmail: 'EMAIL VERIFICATION',
          summaryTotp: 'TOTP 2FA'
        }
      },
      state: 'State'
    },
    getStarted: 'Get Started',
    goToDashboard: 'Go to Dashboard',
    // User-focused value proposition
    heroSubtitle: 'One Key, All AI Models',
    heroDescription: 'No need to manage multiple subscriptions. Access Claude, GPT, Gemini and more with a single API key',
    tags: {
      subscriptionToApi: 'Subscription to API',
      stickySession: 'Session Persistence',
      realtimeBilling: 'Pay As You Go'
    },
    // Pain points section
    painPoints: {
      title: 'Sound Familiar?',
      items: {
        expensive: {
          title: 'High Subscription Costs',
          desc: 'Paying for multiple AI subscriptions that add up every month'
        },
        complex: {
          title: 'Account Chaos',
          desc: 'Managing scattered accounts and API keys across different platforms'
        },
        unstable: {
          title: 'Service Interruptions',
          desc: 'Single accounts hitting rate limits and disrupting your workflow'
        },
        noControl: {
          title: 'No Usage Control',
          desc: "Can't track where your money goes or limit team member usage"
        }
      }
    },
    // Solutions section
    solutions: {
      title: 'We Solve These Problems',
      subtitle: 'Three simple steps to stress-free AI access'
    },
    features: {
      unifiedGateway: 'One-Click Access',
      unifiedGatewayDesc: 'Get a single API key to call all connected AI models. No separate applications needed.',
      multiAccount: 'Always Reliable',
      multiAccountDesc: 'Smart routing across multiple upstream accounts with automatic failover. Say goodbye to errors.',
      balanceQuota: 'Pay What You Use',
      balanceQuotaDesc: 'Usage-based billing with quota limits. Full visibility into team consumption.'
    },
    // Comparison section
    comparison: {
      title: 'Why Choose Us?',
      headers: {
        feature: 'Comparison',
        official: 'Official Subscriptions',
        us: 'Our Platform'
      },
      items: {
        pricing: {
          feature: 'Pricing',
          official: 'Fixed monthly fee, pay even if unused',
          us: 'Pay only for what you use'
        },
        models: {
          feature: 'Model Selection',
          official: 'Single provider only',
          us: 'Switch between models freely'
        },
        management: {
          feature: 'Account Management',
          official: 'Manage each service separately',
          us: 'Unified key, one dashboard'
        },
        stability: {
          feature: 'Stability',
          official: 'Single account rate limits',
          us: 'Multi-account pool, auto-failover'
        },
        control: {
          feature: 'Usage Control',
          official: 'Not available',
          us: 'Quotas & detailed analytics'
        }
      }
    },
    providers: {
      title: 'Supported AI Models',
      description: 'One API, Multiple Choices',
      supported: 'Supported',
      soon: 'Soon',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: 'More'
    },
    // CTA section
    cta: {
      title: 'Ready to Get Started?',
      description: 'Sign up now and get free trial credits to experience seamless AI access',
      button: 'Sign Up Free'
    },
    footer: {
      allRightsReserved: 'All rights reserved.'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key Usage',
    subtitle: 'Enter your API Key to view real-time spending and usage status',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: 'Query',
    querying: 'Querying...',
    privacyNote: 'Your Key is processed locally in the browser and will not be stored',
    dateRange: 'Date Range:',
    dateRangeToday: 'Today',
    dateRange7d: '7 Days',
    dateRange30d: '30 Days',
    dateRange90d: '90 Days',
    dateRangeCustom: 'Custom',
    apply: 'Apply',
    used: 'Used',
    detailInfo: 'Detail Information',
    tokenStats: 'Token Statistics',
    dailyDetail: 'Daily Detail',
    modelStats: 'Model Usage Statistics',
    // Table headers
    date: 'Date',
    model: 'Model',
    requests: 'Requests',
    inputTokens: 'Input Tokens',
    outputTokens: 'Output Tokens',
    cacheCreationTokens: 'Cache Creation',
    cacheReadTokens: 'Cache Read',
    cacheWriteTokens: 'Cache Write',
    totalTokens: 'Total Tokens',
    cost: 'Cost',
    // Status
    quotaMode: 'Key Quota Mode',
    walletBalance: 'Wallet Balance',
    // Ring card titles
    totalQuota: 'Total Quota',
    limit5h: '5-Hour Limit',
    limitDaily: 'Daily Limit',
    limit7d: '7-Day Limit',
    limitWeekly: 'Weekly Limit',
    limitMonthly: 'Monthly Limit',
    // Detail rows
    remainingQuota: 'Remaining Quota',
    expiresAt: 'Expires At',
    todayExpires: '(expires today)',
    daysLeft: '({days} days)',
    usedQuota: 'Used Quota',
    resetNow: 'Resetting soon',
    subscriptionType: 'Subscription Type',
    subscriptionExpires: 'Subscription Expires',
    // Usage stat cells
    todayRequests: 'Today Requests',
    todayInputTokens: 'Today Input',
    todayOutputTokens: 'Today Output',
    todayTokens: 'Today Tokens',
    todayCacheCreation: 'Today Cache Creation',
    todayCacheRead: 'Today Cache Read',
    todayCost: 'Today Cost',
    rpmTpm: 'RPM / TPM',
    totalRequests: 'Total Requests',
    totalInputTokens: 'Total Input',
    totalOutputTokens: 'Total Output',
    totalTokensLabel: 'Total Tokens',
    totalCacheCreation: 'Total Cache Creation',
    totalCacheRead: 'Total Cache Read',
    totalCost: 'Total Cost',
    avgDuration: 'Avg Duration',
    // Messages
    enterApiKey: 'Please enter an API Key',
    querySuccess: 'Query successful',
    queryFailed: 'Query failed',
    queryFailedRetry: 'Query failed, please try again later',
    noDailyUsage: 'No daily usage data',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API Setup',
    description: 'Configure your Sub2API instance',
    database: {
      title: 'Database Configuration',
      description: 'Connect to your PostgreSQL database',
      host: 'Host',
      port: 'Port',
      username: 'Username',
      password: 'Password',
      databaseName: 'Database Name',
      sslMode: 'SSL Mode',
      passwordPlaceholder: 'Password',
      ssl: {
        disable: 'Disable',
        require: 'Require',
        verifyCa: 'Verify CA',
        verifyFull: 'Verify Full'
      }
    },
    redis: {
      title: 'Redis Configuration',
      description: 'Connect to your Redis server',
      host: 'Host',
      port: 'Port',
      username: 'Username (optional)',
      password: 'Password (optional)',
      database: 'Database',
      usernamePlaceholder: 'Leave empty for default user',
      passwordPlaceholder: 'Password',
      enableTls: 'Enable TLS',
      enableTlsHint: 'Use TLS when connecting to Redis (public CA certs)'
    },
    admin: {
      title: 'Admin Account',
      description: 'Create your administrator account',
      email: 'Email',
      password: 'Password',
      confirmPassword: 'Confirm Password',
      passwordPlaceholder: 'Min 8 characters',
      confirmPasswordPlaceholder: 'Confirm password',
      passwordMismatch: 'Passwords do not match'
    },
    ready: {
      title: 'Ready to Install',
      description: 'Review your configuration and complete setup',
      database: 'Database',
      redis: 'Redis',
      adminEmail: 'Admin Email'
    },
    status: {
      testing: 'Testing...',
      success: 'Connection Successful',
      testConnection: 'Test Connection',
      installing: 'Installing...',
      completeInstallation: 'Complete Installation',
      completed: 'Installation completed!',
      redirecting: 'Redirecting to login page...',
      restarting: 'Service is restarting, please wait...',
      timeout: 'Service restart is taking longer than expected. Please refresh the page manually.'
    }
  },

  // Common
}
