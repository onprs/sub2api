import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import HomeView from '../HomeView.vue'
import HomeRoutingArtwork from '@/components/home/HomeRoutingArtwork.vue'

const checkAuth = vi.hoisted(() => vi.fn())
const fetchPublicSettings = vi.hoisted(() => vi.fn())
const publicSettingsState = vi.hoisted(() => ({
  current: null as Record<string, boolean | string> | null,
}))

const messages: Record<string, string> = {
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.login': 'Login',
  'home.dashboard': 'Dashboard',
  'home.goToDashboard': 'Dashboard',
  'home.getStarted': 'Get Started',
  'home.tags.subscriptionToApi': 'Subscription to API',
  'home.tags.stickySession': 'Sticky Session',
  'home.tags.realtimeBilling': 'Realtime Billing',
  'home.features.unifiedGateway': 'Unified Gateway',
  'home.features.unifiedGatewayDesc': 'Unified gateway description',
  'home.features.multiAccount': 'Multi Account',
  'home.features.multiAccountDesc': 'Multi account description',
  'home.features.balanceQuota': 'Balance and Quota',
  'home.features.balanceQuotaDesc': 'Balance and quota description',
  'home.providers.title': 'Supported Providers',
  'home.providers.description': 'Provider description',
  'home.providers.claude': 'Claude',
  'home.providers.gemini': 'Gemini',
  'home.providers.antigravity': 'Antigravity',
  'home.providers.supported': 'Supported',
  'home.providers.more': 'More',
  'home.providers.soon': 'Soon',
  'home.docs': 'Docs',
  'home.footer.allRightsReserved': 'All rights reserved.',
  'home.prototype.editorialPipeline.title': 'One Request. Four Processing Layers.',
  'home.prototype.editorialPipeline.subtitle': 'Request Lifecycle',
  'home.prototype.editorialControl.title': 'Every Request, In Context',
  'home.prototype.editorialControl.subtitle': 'Control Plane',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    isAuthenticated: false,
    isAdmin: false,
    user: null,
    checkAuth,
  }),
  useAppStore: () => ({
    cachedPublicSettings: publicSettingsState.current,
    siteName: 'Sub2API',
    siteLogo: '',
    siteSubtitle: 'AI API Gateway Platform',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
  }),
}))

describe('HomeView footer', () => {
  beforeEach(() => {
    checkAuth.mockReset()
    fetchPublicSettings.mockReset()
    publicSettingsState.current = null
    localStorage.clear()
    window.history.replaceState({}, '', '/home')
    document.documentElement.classList.remove('dark')

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('renders the LM Speed verification badge in the minimal homepage footer', () => {
    window.history.replaceState({}, '', '/home?variant=minimal')
    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    const badgeLink = wrapper.find(
      'footer a[href="https://lmspeed.net/provider/api-onprs-top"]',
    )
    expect(badgeLink.exists()).toBe(true)
    expect(badgeLink.attributes('target')).toBe('_blank')
    expect(badgeLink.attributes('rel')).toBe('noopener noreferrer')

    const badgeImage = badgeLink.find(
      'img[src="https://lmspeed.net/api/provider/claim-badge/1420?claim=1420-pI3oIdhdh2Iekbg2DuIZuPDUska9-U9f"]',
    )
    expect(badgeImage.exists()).toBe(true)
    expect(badgeImage.attributes('alt')).toBe('Verified on LM Speed')

    wrapper.unmount()
  })

  it.each(['/home', '/', '/home?variant=unknown'])('默认入口 %s 使用 Editorial 首页', path => {
    window.history.replaceState({}, '', path)
    const wrapper = mount(HomeView, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, LocaleSwitcher: true, Icon: true } },
    })
    expect(wrapper.find('[data-testid="home-editorial"]').exists()).toBe(true)
    expect(wrapper.find('.editorial-header .chapter-nav').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['minimal', 'home-minimal'],
    ['console', 'home-console'],
    ['editorial', 'home-editorial'],
  ])('renders the %s homepage design variant according to url search param', (variant, testId) => {
    window.history.replaceState({}, '', `/home?variant=${variant}`)

    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    expect(wrapper.find(`[data-testid="${testId}"]`).exists()).toBe(true)
    expect(wrapper.text()).toContain('Sub2API')
    expect(wrapper.find('button[title="Dark"]').exists()).toBe(true)

    if (variant === 'editorial') {
      const editorialText = wrapper.find('[data-testid="home-editorial"]').text()
      expect(wrapper.find('h1').text()).toBe('Sub2API')
      expect(wrapper.find('canvas.routing-artwork').attributes('aria-hidden')).toBe('true')
      expect(wrapper.find('.cover-cta').text()).toContain('Get Started')
      expect(wrapper.find('.ready-word').text()).toBe('READY.')
      expect(wrapper.findAll('.editorial-scenes > section')).toHaveLength(7)
      expect(wrapper.find('.editorial-scroll-track').exists()).toBe(false)
      expect(wrapper.find('.editorial-header .chapter-nav').exists()).toBe(false)
      expect(wrapper.find('.editorial-header a[href^="#"]').exists()).toBe(false)
      expect(wrapper.find('.editorial-header .editorial-brand').exists()).toBe(true)
      expect(wrapper.find('.editorial-header .editorial-actions').exists()).toBe(true)

      // 断言四类标准生成入口完整准确存在
      expect(editorialText).toContain('/v1/responses')
      expect(editorialText).toContain('/v1/chat/completions')
      expect(editorialText).toContain('/v1/messages')
      expect(editorialText).toContain('/v1beta/models/{model}:generateContent')

      // 断言真实请求生命周期与语义保障能力关键词
      expect(editorialText).toContain('RECEIVE')
      expect(editorialText).toContain('NORMALIZE')
      expect(editorialText).toContain('ORCHESTRATE')
      expect(editorialText).toContain('DELIVER')
      expect(editorialText).toContain('STREAM')
      expect(editorialText).toContain('TOOLS')
      expect(editorialText).toContain('REASONING')
      expect(editorialText).toContain('USAGE')
      expect(editorialText).toContain('SSE STREAM & CACHED TOKEN LEDGER')
      expect(editorialText).not.toContain('SSE DUPLEX')

      // 保留原有模型并将三个新增型号放在对应来源首位。
      expect(wrapper.findAll('.model-id')).toHaveLength(15)
      expect(wrapper.find('#model-openai .model-id').text()).toBe('gpt-6-astra')
      expect(wrapper.find('#model-google .model-id').text()).toBe('google/gemini-3.8-flash')
      expect(wrapper.find('#model-meta .model-id').text()).toBe('meta/muse-spark-1.3')
      expect(wrapper.find('.cover-tech-index').text()).toContain('15')
      expect(editorialText).toContain('gpt-5.6')
      expect(editorialText).toContain('gpt-5.6-sol')
      expect(editorialText).toContain('gpt-5.4-mini')
      expect(editorialText).toContain('google/gemini-3.7-flash')
      expect(editorialText).toContain('google/gemini-3.6-flash')
      expect(editorialText).toContain('gemini-3.1-pro-preview')
      expect(editorialText).toContain('deepseek/deepseek-v4-pro')
      expect(editorialText).toContain('deepseek/deepseek-v4-flash')
      expect(editorialText).toContain('zai-org/GLM-5.3')
      expect(editorialText).toContain('meta/muse-spark-1.2')
      expect(editorialText).toContain('moonshotai/Kimi-K3')
      expect(editorialText).toContain('moonshotai/Kimi-K2.7-Code')

      // 绝不包含未准入模型展示
      expect(editorialText).not.toMatch(/claude/i)
      expect(editorialText).not.toMatch(/anthropic/i)
      expect(editorialText).not.toMatch(/grok/i)
    }

    wrapper.unmount()
  })

  it.each([
    ['/branding/site-icon.png', '/branding/site-icon.png'],
    ['https://cdn.example.com/site-icon.png', 'https://cdn.example.com/site-icon.png'],
    ['data:image/png;base64,iVBORw0KGgo=', 'data:image/png;base64,iVBORw0KGgo='],
    ['javascript:alert(1)', '/logo.svg'],
  ])('Editorial 沿用公开配置中的站点名称和安全 Logo：%s', (siteLogo, expectedLogo) => {
    window.history.replaceState({}, '', '/home?variant=editorial')
    publicSettingsState.current = {
      site_name: '站点品牌配置',
      site_logo: siteLogo,
      site_subtitle: '自定义站点副标题',
    }
    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })
    expect(wrapper.find('.editorial-brand strong').text()).toBe('站点品牌配置')
    expect(wrapper.find('.editorial-brand img').attributes('src')).toBe(expectedLogo)
    expect(wrapper.find('#cover-title').text()).toBe('站点品牌配置')
    expect(wrapper.find('.cover-deck p').text()).toBe('自定义站点副标题')
    expect(wrapper.find('.footer-brand strong').text()).toBe('站点品牌配置')
    wrapper.unmount()
  })

  it('章节跳转在当前页面内滚动并把键盘焦点交给目标章节', async () => {
    window.history.replaceState({}, '', '/home?variant=editorial')
    const wrapper = mount(HomeView, {
      attachTo: document.body,
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })
    const target = wrapper.find('#protocols').element as HTMLElement
    const scrollIntoView = vi.fn()
    target.scrollIntoView = scrollIntoView
    await wrapper.find('.cover-next').trigger('click', { button: 0 })
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'instant', block: 'start' })
    expect(document.activeElement).toBe(target)
    expect(window.location.hash).toBe('')
    scrollIntoView.mockClear()
    await wrapper.find('.cover-next').trigger('click', { button: 0, ctrlKey: true })
    expect(scrollIntoView).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('连续舞台的导航只启用当前幕，并能在运行中切换到完整静态阅读', async () => {
    window.history.replaceState({}, '', '/home?variant=editorial')
    let scroll = 0
    let motionChanged: (() => void) | undefined
    const query = {
      matches: false,
      addEventListener: vi.fn((_event: string, callback: () => void) => { motionChanged = callback }),
      removeEventListener: vi.fn(),
    }
    vi.spyOn(window, 'matchMedia').mockReturnValue(query as unknown as MediaQueryList)
    vi.spyOn(window, 'scrollY', 'get').mockImplementation(() => scroll)
    vi.spyOn(window, 'scrollTo').mockImplementation((options: ScrollToOptions | number) => {
      if (typeof options === 'object') scroll = options.top ?? 0
    })
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.id === 'models' ? 2400 : 896
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      let top = this.tagName === 'MAIN' ? 64 - scroll : 64
      if (this.tagName === 'SECTION' && !this.parentElement?.classList.contains('journey-stage')) {
        top += ['cover', 'protocols', 'models', 'architecture', 'semantics', 'control', 'ready'].indexOf(this.id) * 1000
      }
      return { x: 0, y: top, top, left: 0, right: 1440, bottom: top + 936, width: 1440, height: 936 } as DOMRect
    })
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class { observe = vi.fn(); disconnect = disconnect })
    const wrapper = mount(HomeView, {
      attachTo: document.body,
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
          HomeJourneyArtwork: true,
          HomeRoutingArtwork: true,
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('.journey-stage').exists()).toBe(true)
    expect(wrapper.find('#cover').attributes('inert')).toBeUndefined()
    expect(wrapper.find('#models').attributes('inert')).toBeDefined()
    expect(wrapper.findComponent({ name: 'HomeJourneyArtwork' }).props('frame')).toMatchObject({ ink: '#1b1c1f', accent: '#315be8' })
    await wrapper.find('.journey-chapters a[href="#protocols"]').trigger('click', { button: 0 })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'HomeJourneyArtwork' }).props('frame')).toMatchObject({ ink: '#f5f5f6', accent: '#8aa5ff' })
    await wrapper.find('.journey-chapters a[href="#models"]').trigger('click', { button: 0 })
    await flushPromises()
    expect(scroll).toBeGreaterThan(0)
    expect(wrapper.find('#models').attributes('inert')).toBeUndefined()
    expect(wrapper.find('#cover').attributes('inert')).toBeDefined()
    expect(document.activeElement).toBe(wrapper.find('#models').element)
    query.matches = true
    motionChanged?.()
    await flushPromises()
    expect(wrapper.find('.journey-stage').exists()).toBe(false)
    expect(wrapper.findAll('.editorial-scenes > section[inert]')).toHaveLength(0)
    expect(wrapper.findAll('.model-id')).toHaveLength(15)
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()
    expect(query.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it.each([
    ['email_verify_enabled', 'EMAIL VERIFY', 'TOTP 2FA'],
    ['totp_enabled', 'TOTP 2FA', 'EMAIL VERIFY'],
    ['payment_enabled', 'ONLINE PAY', 'MODEL UNIT RATES'],
    ['model_pricing_enabled', 'MODEL UNIT RATES', 'ONLINE PAY'],
    ['allow_user_view_error_requests', 'ERROR TRACE', 'ONLINE PAY'],
  ])('单独启用 %s 时不展示其他被关闭的能力', (setting, present, absent) => {
    window.history.replaceState({}, '', '/home?variant=editorial')
    publicSettingsState.current = { [setting]: true }
    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })
    expect(wrapper.text()).toContain(present)
    expect(wrapper.text()).not.toContain(absent)
    expect(wrapper.findAll('.ready-capabilities-row .cap-col')).toHaveLength(
      setting === 'allow_user_view_error_requests' ? 2 : 3,
    )
    wrapper.unmount()
  })

  it('renders only capabilities enabled by public settings', () => {
    window.history.replaceState({}, '', '/home?variant=editorial')
    publicSettingsState.current = {
      email_verify_enabled: true,
      totp_enabled: true,
      payment_enabled: true,
      model_pricing_enabled: true,
      allow_user_view_error_requests: true,
    }

    const enabledWrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    expect(enabledWrapper.findAll('.ready-capabilities-row .cap-col')).toHaveLength(4)
    expect(enabledWrapper.findAll('.control-ledger-grid .control-feature-entry')).toHaveLength(4)
    expect(enabledWrapper.text()).toContain('EMAIL VERIFY')
    expect(enabledWrapper.text()).toContain('TOTP 2FA')
    expect(enabledWrapper.text()).toContain('ONLINE PAY')
    enabledWrapper.unmount()

    publicSettingsState.current = {
      email_verify_enabled: false,
      totp_enabled: false,
      payment_enabled: false,
      model_pricing_enabled: false,
      allow_user_view_error_requests: false,
    }

    const disabledWrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    expect(disabledWrapper.findAll('.ready-capabilities-row .cap-col')).toHaveLength(2)
    expect(disabledWrapper.findAll('.control-ledger-grid .control-feature-entry')).toHaveLength(2)
    expect(disabledWrapper.text()).not.toContain('EMAIL VERIFY')
    expect(disabledWrapper.text()).not.toContain('TOTP 2FA')
    expect(disabledWrapper.text()).not.toContain('ONLINE PAY')
    disabledWrapper.unmount()
  })
})

describe('首页路由图形', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  function setupArtwork(reduced = false) {
    const disconnect = vi.fn()
    const observe = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      observe = observe
      disconnect = disconnect
    })
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: reduced } as MediaQueryList)
    vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 1440, height: 936,
    } as DOMRect)
    const context = {
      scale: vi.fn(),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      stroke: vi.fn(),
      setLineDash: vi.fn(),
      fillRect: vi.fn(),
      fillText: vi.fn(),
      lineDashOffset: 0,
    }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(
      context as unknown as CanvasRenderingContext2D,
    )
    return { context, disconnect, observe }
  }

  it('绘制路由节点并在反向滚动时恢复相同图形位置', async () => {
    const { context, disconnect, observe } = setupArtwork()
    const wrapper = mount(HomeRoutingArtwork, { props: { progress: 0 } })
    expect(context.fillText).toHaveBeenCalledWith('/v1', expect.any(Number), expect.any(Number))
    expect(context.stroke).toHaveBeenCalled()
    expect(observe).toHaveBeenCalledWith(wrapper.element)
    const initialOffset = context.lineDashOffset
    const initialNode = context.fillRect.mock.calls[0]
    await wrapper.setProps({ progress: 0.14 })
    expect(context.lineDashOffset).not.toBe(initialOffset)
    await wrapper.setProps({ progress: 0 })
    expect(context.lineDashOffset).toBe(initialOffset)
    expect(context.fillRect.mock.lastCall).toEqual(initialNode)
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('滚动条使画布小于临界宽度时仍与页面使用相同的平板布局', () => {
    const { context } = setupArtwork()
    vi.stubGlobal('innerWidth', 768)
    vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 760, height: 936,
    } as DOMRect)
    const wrapper = mount(HomeRoutingArtwork, { props: { progress: 0 } })
    expect(context.moveTo.mock.calls[0]?.[0]).toBeGreaterThan(300)
    wrapper.unmount()
  })

  it('减少动态效果时保持路由信号静止', async () => {
    const { context } = setupArtwork(true)
    const wrapper = mount(HomeRoutingArtwork, { props: { progress: 0 } })
    const initialOffset = context.lineDashOffset
    await wrapper.setProps({ progress: 0.14 })
    expect(context.lineDashOffset).toBe(initialOffset)
    wrapper.unmount()
  })

  it('主题变化时重新绘制，并清理减少动态效果的监听器', async () => {
    const { context } = setupArtwork()
    const addEventListener = vi.fn()
    const removeEventListener = vi.fn()
    vi.spyOn(window, 'matchMedia').mockReturnValue({
      matches: false, addEventListener, removeEventListener,
    } as unknown as MediaQueryList)
    const wrapper = mount(HomeRoutingArtwork, { props: { progress: 0, dark: false } })
    const count = context.stroke.mock.calls.length
    await wrapper.setProps({ dark: true })
    expect(context.stroke.mock.calls.length).toBeGreaterThan(count)
    expect(addEventListener).toHaveBeenCalledWith('change', expect.any(Function))
    wrapper.unmount()
    expect(removeEventListener).toHaveBeenCalledWith('change', addEventListener.mock.calls[0]?.[1])
  })

  it('浏览器没有 Canvas 上下文时保留页面其余内容', () => {
    setupArtwork()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    const wrapper = mount(HomeRoutingArtwork, { props: { progress: 0 } })
    expect(wrapper.find('canvas').exists()).toBe(true)
    wrapper.unmount()
  })
})
