import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import CopyModelMappingModal from '../CopyModelMappingModal.vue'
import { adminAPI } from '@/api/admin'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: vi.fn(),
      copyModelMapping: vi.fn()
    }
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params?.count ? `${key}:${params.count}` : key
    })
  }
})

function mountModal(extraProps: Record<string, unknown> = {}) {
  return mount(CopyModelMappingModal, {
    props: {
      show: true,
      targetAccountIds: [200],
      targetPlatform: 'antigravity',
      ...extraProps
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: `
            <select
              data-test="source-select"
              :value="modelValue"
              @change="$emit('update:modelValue', Number($event.target.value))"
            >
              <option value=""></option>
              <option
                v-for="option in options"
                :key="option.value"
                :value="option.value"
                :disabled="option.disabled"
              >
                {{ option.label }}
              </option>
            </select>
          `
        }
      }
    }
  })
}

describe('CopyModelMappingModal', () => {
  beforeEach(() => {
    vi.mocked(adminAPI.accounts.list).mockReset()
    vi.mocked(adminAPI.accounts.copyModelMapping).mockReset()

    vi.mocked(adminAPI.accounts.list).mockResolvedValue({
      items: [
        {
          id: 100,
          name: 'Source With Mapping',
          platform: 'antigravity',
          type: 'oauth',
          credentials: {
            model_mapping: {
              'claude-sonnet-4-6': 'claude-sonnet-4-6',
              'gemini-3-flash': 'gemini-3-flash'
            }
          }
        },
        {
          id: 200,
          name: 'Selected Target',
          platform: 'antigravity',
          type: 'oauth',
          credentials: {
            model_mapping: {
              'old-model': 'old-model'
            }
          }
        },
        {
          id: 300,
          name: 'Empty Mapping',
          platform: 'antigravity',
          type: 'oauth',
          credentials: {}
        }
      ],
      total: 3,
      page: 1,
      page_size: 100,
      pages: 1
    } as any)

    vi.mocked(adminAPI.accounts.copyModelMapping).mockResolvedValue({
      source_account_id: 100,
      target_account_ids: [200],
      platform: 'antigravity',
      mapping_count: 2,
      success: 1,
      failed: 0,
      success_ids: [200],
      failed_ids: [],
      results: [{ account_id: 200, success: true }]
    } as any)
  })

  it('loads same-platform source accounts and excludes targets or empty mappings', async () => {
    const wrapper = mountModal()

    await flushPromises()

    expect(adminAPI.accounts.list).toHaveBeenCalledWith(1, 500, { platform: 'antigravity' })
    expect(wrapper.text()).toContain('Source With Mapping')
    expect(wrapper.text()).toContain('admin.accounts.copyModelMapping.mappingCount:2')
    expect(wrapper.text()).not.toContain('Selected Target')
    expect(wrapper.text()).not.toContain('Empty Mapping')
  })

  it('copies the selected source mapping to target accounts', async () => {
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.get('[data-test="source-select"]').setValue('100')
    await wrapper.get('[data-test="copy-model-mapping-submit"]').trigger('click')
    await flushPromises()

    expect(adminAPI.accounts.copyModelMapping).toHaveBeenCalledWith({
      source_account_id: 100,
      target_account_ids: [200]
    })
    expect(wrapper.emitted('copied')?.[0]?.[0]).toMatchObject({
      success: 1,
      failed: 0
    })
  })
})
