import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  checkMixedChannelRiskMock,
  createOpenCodeGoConsoleAuthTicketMock,
  importCodexSessionMock,
  createOpenAICodexPATMock,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  createOpenCodeGoConsoleAuthTicketMock: vi.fn(),
  importCodexSessionMock: vi.fn(),
  createOpenAICodexPATMock: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn(),
    cachedPublicSettings: {},
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock,
      createOpenCodeGoConsoleAuthTicket: createOpenCodeGoConsoleAuthTicketMock,
      importCodexSession: importCodexSessionMock,
      createOpenAICodexPAT: createOpenAICodexPATMock,
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({}),
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({}),
}))

function oauthState() {
  return {
    authUrl: { value: '' },
    sessionId: { value: '' },
    loading: { value: false },
    error: { value: '' },
    oauthState: { value: '' },
    state: { value: '' },
    generateAuthUrl: vi.fn().mockResolvedValue(undefined),
    exchangeAuthCode: vi.fn(),
    validateRefreshToken: vi.fn(),
    buildCredentials: vi.fn(() => ({})),
    buildExtraInfo: vi.fn(() => ({})),
    parseSessionKeys: vi.fn(() => []),
    resetState: vi.fn(),
    getCapabilities: vi.fn().mockResolvedValue({ ai_studio_oauth_enabled: false }),
  }
}

vi.mock('@/composables/useAccountOAuth', () => ({
  useAccountOAuth: () => oauthState(),
}))

vi.mock('@/composables/useOpenAIOAuth', () => ({
  useOpenAIOAuth: () => oauthState(),
}))

vi.mock('@/composables/useGeminiOAuth', () => ({
  useGeminiOAuth: () => oauthState(),
}))

vi.mock('@/composables/useAntigravityOAuth', () => ({
  useAntigravityOAuth: () => oauthState(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: ['modelValue', 'platform', 'accountType', 'tierId'],
  emits: ['update:modelValue'],
  template: '<div data-testid="model-whitelist-selector" />',
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String,
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-codex-session', 'import-codex-pat'],
  template: `
    <div>
      <button data-testid="import-codex-session" @click="$emit('import-codex-session', 'session-json')">session</button>
      <button data-testid="import-codex-pat" @click="$emit('import-codex-pat', 'pat-token')">pat</button>
    </div>
  `,
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  createAccountMock.mockResolvedValue({
    id: 42,
    name: 'OpenCode Go Key',
    platform: 'opencode_go',
    type: 'apikey',
    credentials: {},
    extra: {},
  })
  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
  createOpenCodeGoConsoleAuthTicketMock.mockResolvedValue({
    ticket_id: 'ticket_123',
    expires_at: '2026-06-22T12:00:00Z',
    workspace_id: 'wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ',
    helper_url: 'https://api.onprs.top/api/v1/opencode-go/console-auth/helper.ps1?ticket=ticket_123',
    helper_command: 'powershell -NoProfile -ExecutionPolicy Bypass -Command "iwr helper | iex"',
  })
})

describe('CreateAccountModal', () => {
  it('提供完整的临时不可调度快捷规则并可填入网关异常', async () => {
    const wrapper = mountModal()

    await wrapper.get('[data-testid="temp-unsched-toggle"]').trigger('click')
    const presetButtons = wrapper.findAll('[data-testid^="temp-unsched-preset-"]')
    expect(presetButtons.map((button) => button.attributes('data-testid'))).toEqual([
      'temp-unsched-preset-request-timeout',
      'temp-unsched-preset-rate-limit',
      'temp-unsched-preset-internal-error',
      'temp-unsched-preset-bad-gateway',
      'temp-unsched-preset-service-unavailable',
      'temp-unsched-preset-gateway-timeout',
      'temp-unsched-preset-cloudflare-timeout',
      'temp-unsched-preset-overload'
    ])

    await wrapper.get('[data-testid="temp-unsched-preset-bad-gateway"]').trigger('click')
    const inputValues = wrapper.findAll('input').map((input) => input.element.value)
    expect(inputValues).toContain('502')
    expect(inputValues).toContain('10')
    expect(inputValues).toContain('bad gateway, bad_gateway, upstream error, upstream_error, upstream connect error, upstream reset, connection reset, upstream service temporarily unavailable')
    expect(inputValues).toContain('admin.accounts.tempUnschedulable.presets.badGatewayDesc')
  })

  it('Gemini API Key Free Tier 默认使用 AI Studio 实际请求 ID', async () => {
    const wrapper = mountModal()

    const geminiButton = wrapper.findAll('button').find((button) => button.text().trim() === 'Gemini')
    expect(geminiButton).toBeDefined()
    await geminiButton!.trigger('click')
    await flushPromises()

    const apiKeyButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.accounts.gemini.accountType.apiKeyTitle')
    )
    expect(apiKeyButton).toBeDefined()
    await apiKeyButton!.trigger('click')
    await flushPromises()

    const selector = wrapper.getComponent(ModelWhitelistSelectorStub)
    expect(selector.props('platform')).toBe('gemini')
    expect(selector.props('accountType')).toBe('apikey')
    expect(selector.props('tierId')).toBe('aistudio_free')
    expect(selector.props('modelValue')).toEqual([
      'gemini-3-flash-preview',
      'gemini-2.5-flash',
      'gemini-2.5-flash-lite',
      'gemini-3.1-flash-lite',
      'gemini-3.5-flash',
      'gemini-3.5-flash-lite',
      'gemini-3.6-flash',
      'gemini-3.7-flash',
      'gemma-4-26b-a4b-it',
      'gemma-4-31b-it',
    ])
  })

  it('creates OpenCode Go as an API key account with the default upstream base URL', async () => {
    const wrapper = mountModal()

    const platformButton = wrapper.findAll('button').find((button) => button.text().includes('OpenCode Go'))
    expect(platformButton).toBeDefined()
    await platformButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('OAuth')
    expect(wrapper.text()).not.toContain('admin.accounts.types.responsesApi')
    expect(wrapper.find('[data-testid="openai-responses-mode-select"]').exists()).toBe(false)

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenCode Go Key')
    const keyInput = wrapper.findAll('input[type="password"]').find((input) =>
      (input.attributes('placeholder') || '').includes('sk-')
    )
    expect(keyInput).toBeDefined()
    await keyInput!.setValue('sk-opencode-go')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload.platform).toBe('opencode_go')
    expect(payload.type).toBe('apikey')
    expect(payload.credentials).toEqual({
      base_url: 'https://opencode.ai/zen/go/v1',
      api_key: 'sk-opencode-go',
    })
    expect(payload.extra).toBeUndefined()
  })

  it('creates ClinePass as API key only with the official API root and no console flow', async () => {
    const wrapper = mountModal()
    const platformButton = wrapper.findAll('button').find((button) => button.text().includes('ClinePass'))
    expect(platformButton).toBeDefined()
    await platformButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('OAuth')
    expect(wrapper.find('[data-testid="opencode-go-console-panel"]').exists()).toBe(false)
    await wrapper.get('[data-tour="account-form-name"]').setValue('ClinePass Key')
    const keyInput = wrapper.findAll('input[type="password"]').find((input) =>
      (input.attributes('placeholder') || '').includes('sk-')
    )
    expect(keyInput).toBeDefined()
    await keyInput!.setValue('sk-clinepass')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload.platform).toBe('clinepass')
    expect(payload.type).toBe('apikey')
    expect(payload.credentials.base_url).toBe('https://api.cline.bot/api/v1')
    expect(payload.credentials.api_key).toBe('sk-clinepass')
    expect(createOpenCodeGoConsoleAuthTicketMock).not.toHaveBeenCalled()
  })

  it('shows OpenCode Go official usage sync step after creation and generates helper command', async () => {
    const wrapper = mountModal()

    const platformButton = wrapper.findAll('button').find((button) => button.text().includes('OpenCode Go'))
    await platformButton!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenCode Go Key')
    const keyInput = wrapper.findAll('input[type="password"]').find((input) =>
      (input.attributes('placeholder') || '').includes('sk-')
    )
    await keyInput!.setValue('sk-opencode-go')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('官方用量同步')
    expect(wrapper.text()).toContain('OpenCode Go Key')

    await wrapper.get('[data-testid="opencode-go-console-workspace"]').setValue('https://opencode.ai/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go')
    await wrapper.get('[data-testid="opencode-go-console-ticket-button"]').trigger('click')
    await flushPromises()

    expect(createOpenCodeGoConsoleAuthTicketMock).toHaveBeenCalledWith(
      42,
      'https://opencode.ai/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go'
    )
    expect(wrapper.text()).toContain('powershell -NoProfile')
  })

  it('creates OpenRouter account with API key and temporary unschedulable rules', async () => {
    const wrapper = mountModal()

    const platformButton = wrapper.findAll('button').find((button) => button.text().includes('OpenRouter'))
    expect(platformButton).toBeDefined()
    await platformButton!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenRouter Production')
    const keyInput = wrapper.findAll('input[type="password"]').find((input) =>
      (input.attributes('placeholder') || '').includes('sk-')
    )
    expect(keyInput).toBeDefined()
    await keyInput!.setValue('sk-or-v1-abcdef')

    // 开启临时不可调度并填入 429 预设
    await wrapper.get('[data-testid="temp-unsched-toggle"]').trigger('click')
    await wrapper.get('[data-testid="temp-unsched-preset-rate-limit"]').trigger('click')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload.platform).toBe('openrouter')
    expect(payload.type).toBe('apikey')
    expect(payload.credentials.base_url).toBe('https://openrouter.ai/api/v1')
    expect(payload.credentials.api_key).toBe('sk-or-v1-abcdef')
    expect(payload.credentials.temp_unschedulable_enabled).toBe(true)
    expect(payload.credentials.temp_unschedulable_rules).toHaveLength(1)
    expect(payload.credentials.temp_unschedulable_rules[0].error_code).toBe(429)
  })

  it('creates OpenAI API key account with temporary unschedulable rules', async () => {
    const wrapper = mountModal()

    const platformButton = wrapper.findAll('button').find((button) => button.text().includes('OpenAI'))
    expect(platformButton).toBeDefined()
    await platformButton!.trigger('click')
    await flushPromises()

    // 切换到 API Key 类型
    const apiKeyCategoryButton = wrapper.findAll('button').find((button) =>
      button.text().includes('API Key') && button.text().includes('admin.accounts.types.responsesApi')
    )
    expect(apiKeyCategoryButton).toBeDefined()
    await apiKeyCategoryButton!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenAI Direct Key')
    const keyInput = wrapper.findAll('input[type="password"]').find((input) =>
      (input.attributes('placeholder') || '').includes('sk-')
    )
    expect(keyInput).toBeDefined()
    await keyInput!.setValue('sk-proj-123456')

    // 开启临时不可调度并填入 503 预设
    await wrapper.get('[data-testid="temp-unsched-toggle"]').trigger('click')
    await wrapper.get('[data-testid="temp-unsched-preset-service-unavailable"]').trigger('click')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload.platform).toBe('openai')
    expect(payload.type).toBe('apikey')
    expect(payload.credentials.api_key).toBe('sk-proj-123456')
    expect(payload.credentials.temp_unschedulable_enabled).toBe(true)
    expect(payload.credentials.temp_unschedulable_rules).toHaveLength(1)
    expect(payload.credentials.temp_unschedulable_rules[0].error_code).toBe(503)
  })
})

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(platform: 'openai' | 'anthropic', enableLongContextBilling = false) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, platform === 'openai' ? 'OpenAI' : 'admin.accounts.claudeConsole')
  if (platform === 'openai') {
    await selectButtonByText(wrapper, 'API Key')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} account`)
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
}

async function openCodexImportStep(toggleClicks = 0) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, 'OpenAI')
  for (let click = 0; click < toggleClicks; click += 1) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  return wrapper
}

describe('CreateAccountModal OpenAI long-context billing', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({})
    importCodexSessionMock.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      errors: [],
      warnings: [],
    })
    createOpenAICodexPATMock.mockReset().mockResolvedValue({})
  })

  it('sends false explicitly for normal OpenAI account creation by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('exposes Agent Identity in the OpenAI authorization methods', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')

    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    expect(flow.props('showManualOption')).toBe(true)
    expect(flow.props('showCodexSessionImportOption')).toBe(true)
    expect(flow.props('showAgentIdentityOption')).toBe(true)
    expect(flow.props('showCodexPatOption')).toBe(true)
    expect(flow.props('initialInputMethod')).toBe('manual')
  })

  it.each([
    ['camelCase', { authMode: 'agentIdentity', agentIdentity: { agentRuntimeId: 'runtime' } }],
    ['nested identity without auth_mode', { agent_identity: { agent_runtime_id: 'runtime' } }],
  ])('accepts backend-compatible %s Agent Identity imports', async (_name, content) => {
    const wrapper = await openCodexImportStep()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.inputMethod = 'agent_identity'

    flow.vm.$emit('import-codex-session', JSON.stringify(content))
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
  })

  it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
    await submitApiKeyAccount('openai', true)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('omits the OpenAI setting for non-OpenAI account creation', async () => {
    await submitApiKeyAccount('anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('leaves Codex session import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('leaves Codex PAT import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('sends explicit true for Codex session import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex session import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('sends explicit true for Codex PAT import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex PAT import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })
})
