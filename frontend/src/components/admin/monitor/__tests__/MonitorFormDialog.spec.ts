import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorFormDialog from '../MonitorFormDialog.vue'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  listTemplates: vi.fn(),
  listClinePassModels: vi.fn(),
  listOpenRouterModels: vi.fn(),
  getAllIncludingInactive: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      create: mocks.create,
      update: mocks.update,
      listClinePassModels: mocks.listClinePassModels,
      listOpenRouterModels: mocks.listOpenRouterModels,
    },
    channelMonitorTemplate: {
      list: mocks.listTemplates,
    },
    groups: {
      getAllIncludingInactive: mocks.getAllIncludingInactive,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError: mocks.showError,
    showSuccess: mocks.showSuccess,
  }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/composables/useChannelMonitorFormat', () => ({
  useChannelMonitorFormat: () => ({
    providerPickerClass: () => '',
  }),
}))

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show', 'title', 'width'],
  template: '<section><slot /><footer><slot name="footer" /></footer></section>',
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'placeholder', 'disabled'],
  emits: ['update:modelValue'],
  methods: {
    selectFirst(this: { options: Array<{ value: unknown }>; $emit: (event: string, value: unknown) => void }) {
      this.$emit('update:modelValue', this.options[0]?.value ?? null)
    },
  },
  template: '<button type="button" :data-select="placeholder" :disabled="disabled" @click="selectFirst">{{ modelValue }}</button>',
}

function mountDialog(monitor: ChannelMonitor | null = null) {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Toggle: { name: 'Toggle', template: '<span />' },
        Icon: { name: 'Icon', template: '<span />' },
        ProviderIcon: { name: 'ProviderIcon', template: '<span />' },
        ModelTagInput: { name: 'ModelTagInput', template: '<span />' },
        MonitorAdvancedRequestConfig: {
          name: 'MonitorAdvancedRequestConfig',
          template: '<span />',
        },
      },
    },
  })
}

function externalMonitor(): ChannelMonitor {
  return {
    id: 44,
    name: 'External OpenAI',
    provider: 'openai',
    api_mode: 'chat_completions',
    target_type: 'external',
    group_id: null,
    endpoint: 'https://api.example.com',
    api_key_masked: 'sk-e***',
    api_key_decrypt_failed: false,
    primary_model: 'gpt-5.4',
    extra_models: [],
    group_name: '',
    enabled: true,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 0,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
  }
}

describe('MonitorFormDialog target fields', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.listTemplates.mockResolvedValue({ items: [] })
    mocks.listClinePassModels.mockResolvedValue([])
    mocks.listOpenRouterModels.mockResolvedValue([])
    mocks.getAllIncludingInactive.mockResolvedValue([
      { id: 20, name: 'Anthropic Primary', platform: 'anthropic', status: 'active' },
      { id: 21, name: 'OpenAI Primary', platform: 'openai', status: 'active' },
      { id: 22, name: 'Inactive Anthropic', platform: 'anthropic', status: 'inactive' },
    ])
    mocks.create.mockResolvedValue({})
    mocks.update.mockResolvedValue({})
  })

  it('submits a local group without endpoint or API key fields', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.find('input[type="url"]').exists()).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    const groupSelect = wrapper.findComponent({ name: 'Select' })
    expect(groupSelect.props('options')).toEqual([
      { value: 20, label: 'Anthropic Primary', disabled: false },
    ])

    await wrapper.find('input[placeholder="admin.channelMonitor.form.namePlaceholder"]').setValue('Local Anthropic')
    await wrapper.find('input[placeholder="admin.channelMonitor.form.primaryModelPlaceholder"]').setValue('claude-sonnet-4-6')
    await wrapper.find('button[data-select="admin.channelMonitor.form.groupPlaceholder"]').trigger('click')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(mocks.create).toHaveBeenCalledTimes(1)
    const payload = mocks.create.mock.calls[0][0]
    expect(payload).toEqual(expect.objectContaining({
      name: 'Local Anthropic',
      provider: 'anthropic',
      target_type: 'local',
      group_id: 20,
      primary_model: 'claude-sonnet-4-6',
    }))
    expect(payload).not.toHaveProperty('endpoint')
    expect(payload).not.toHaveProperty('api_key')
    expect(payload).not.toHaveProperty('group_name')
  })

  it('submits external endpoint credentials without a local group', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const externalButton = wrapper.findAll('button').find((button) =>
      button.text() === 'admin.channelMonitor.form.targetExternal'
    )
    expect(externalButton).toBeDefined()
    await externalButton!.trigger('click')

    expect(wrapper.find('button[data-select="admin.channelMonitor.form.groupPlaceholder"]').exists()).toBe(false)
    await wrapper.find('input[placeholder="admin.channelMonitor.form.namePlaceholder"]').setValue('External Anthropic')
    await wrapper.find('input[placeholder="admin.channelMonitor.form.endpointPlaceholder"]').setValue('https://api.example.com')
    await wrapper.find('input[placeholder="admin.channelMonitor.form.apiKeyPlaceholder"]').setValue('external-secret')
    await wrapper.find('input[placeholder="admin.channelMonitor.form.primaryModelPlaceholder"]').setValue('claude-sonnet-4-6')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const payload = mocks.create.mock.calls[0][0]
    expect(payload).toEqual(expect.objectContaining({
      target_type: 'external',
      endpoint: 'https://api.example.com',
      api_key: 'external-secret',
    }))
    expect(payload).not.toHaveProperty('group_id')
  })

  it('requires a new external API key after changing provider', async () => {
    const wrapper = mountDialog(externalMonitor())
    await flushPromises()

    const keyInput = wrapper.find('input[type="password"]')
    expect(keyInput.attributes('required')).toBeUndefined()

    const anthropicButton = wrapper.findAll('button').find((button) =>
      button.text() === 'monitorCommon.providers.anthropic'
    )
    expect(anthropicButton).toBeDefined()
    await anthropicButton!.trigger('click')

    expect(wrapper.find('input[type="password"]').attributes('required')).toBeDefined()
  })
})
