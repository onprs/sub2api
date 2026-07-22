import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const { createAccountMock, checkMixedChannelRiskMock, createOpenCodeGoConsoleAuthTicketMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  createOpenCodeGoConsoleAuthTicketMock: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    cachedPublicSettings: {},
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true,
  }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock,
      createOpenCodeGoConsoleAuthTicket: createOpenCodeGoConsoleAuthTicketMock,
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
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false,
    },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  emits: ['update:modelValue'],
  template: '<div data-testid="model-whitelist-selector" />',
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: {
      show: true,
      proxies: [],
      groups: [],
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
        OAuthAuthorizationFlow: true,
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
})
