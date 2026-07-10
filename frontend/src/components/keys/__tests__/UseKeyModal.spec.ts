import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const apiMocks = vi.hoisted(() => ({
  downloadCliImportScript: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true)
  })
}))

vi.mock('@/api/keys', () => apiMocks)

import UseKeyModal from '../UseKeyModal.vue'

describe('UseKeyModal', () => {
  beforeEach(() => {
    apiMocks.downloadCliImportScript.mockClear()
  })

  it('renders GPT-5.5 and goals feature in OpenAI Codex config', () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.5"')
    expect(configToml).toContain('review_model = "gpt-5.5"')
    expect(configToml).not.toContain('model = "gpt-5.4"')
    expect(configToml).not.toContain('model_context_window')
    expect(configToml).not.toContain('model_auto_compact_token_limit')
    expect(configToml).toContain('[features]\ngoals = true')
  })

  it('renders GPT-5.5 and goals feature in OpenAI Codex WebSocket config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const wsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )

    expect(wsTab).toBeDefined()
    await wsTab!.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('supports_websockets = true'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.5"')
    expect(configToml).toContain('review_model = "gpt-5.5"')
    expect(configToml).not.toContain('model = "gpt-5.4"')
    expect(configToml).not.toContain('model_context_window')
    expect(configToml).not.toContain('model_auto_compact_token_limit')
    expect(configToml).toContain('[features]\nresponses_websockets_v2 = true\ngoals = true')
  })

  it('renders GPT-5.4 mini entry in OpenCode config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Mini"')
    expect(codeBlock.text()).not.toContain('"name": "GPT-5.4 Nano"')
  })

  it('renders OpenCode Go config with opencode-go provider id', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-opencode-go',
        baseUrl: 'https://opencode.ai/zen/go/v1',
        platform: 'opencode_go' as any
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const config = JSON.parse(wrapper.find('pre code').text())

    expect(Object.keys(config.provider)).toEqual(['opencode-go'])
    expect(config.provider['opencode-go'].options).toEqual({
      baseURL: 'https://opencode.ai/zen/go/v1',
      apiKey: 'sk-opencode-go'
    })
    expect(config.provider).not.toHaveProperty('opencode')
    expect(config.provider).not.toHaveProperty('opencode_go')
  })

  it('downloads generated Linux CLI import script for an active grouped key', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai',
        apiKeyRecord: {
          id: 42,
          name: 'Daily Key',
          status: 'active',
          group_id: 7,
          quota: 0,
          quota_used: 0,
          expires_at: null,
          group: {
            id: 7,
            name: 'Pro Coding',
            platform: 'openai',
            default_mapped_model: 'gpt-5.1-codex'
          }
        } as any
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    await wrapper.find('[data-testid="download-linux-cli-import"]').trigger('click')

    expect(apiMocks.downloadCliImportScript).toHaveBeenCalledWith(42, 'linux')
  })

  it('disables generated CLI script downloads when key quota is exhausted', () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai',
        apiKeyRecord: {
          id: 42,
          name: 'Daily Key',
          status: 'quota_exhausted',
          group_id: 7,
          quota: 10,
          quota_used: 10,
          expires_at: null,
          group: {
            id: 7,
            name: 'Pro Coding',
            platform: 'openai'
          }
        } as any
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    expect(wrapper.find('[data-testid="download-linux-cli-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="download-windows-cli-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('keys.useKeyModal.cliImport.disabled.quotaExhausted')
  })

  it('shows disabled generated CLI script downloads when key has no group', () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: null,
        apiKeyRecord: {
          id: 42,
          name: 'Ungrouped Key',
          status: 'active',
          group_id: null,
          quota: 0,
          quota_used: 0,
          expires_at: null,
          group: null
        } as any
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    expect(wrapper.find('[data-testid="download-linux-cli-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="download-windows-cli-import"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('keys.useKeyModal.cliImport.disabled.noGroup')
  })
})
