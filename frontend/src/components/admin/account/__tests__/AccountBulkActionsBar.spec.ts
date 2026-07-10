import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AccountBulkActionsBar from '../AccountBulkActionsBar.vue'

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

describe('AccountBulkActionsBar', () => {
  it('emits copy-model-mapping when selected accounts exist', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [10, 11]
      }
    })

    const button = wrapper
      .findAll('button')
      .find((candidate) => candidate.text() === 'admin.accounts.bulkActions.copyModelMapping')

    expect(button).toBeTruthy()
    await button!.trigger('click')

    expect(wrapper.emitted('copy-model-mapping')).toHaveLength(1)
  })

  it('hides copy mapping action when nothing is selected', () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: []
      }
    })

    expect(wrapper.text()).not.toContain('admin.accounts.bulkActions.copyModelMapping')
  })
})
