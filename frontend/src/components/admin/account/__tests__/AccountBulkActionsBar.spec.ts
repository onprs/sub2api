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

const mountBar = (selectedIds: number[] = []) =>
  mount(AccountBulkActionsBar, {
    props: {
      selectedIds,
      totalResults: 45,
      selectingAll: false,
      allResultsSelected: false
    }
  })

describe('AccountBulkActionsBar', () => {
  it('emits copy-model-mapping when selected accounts exist', async () => {
    const wrapper = mountBar([1])
    const button = wrapper
      .findAll('button')
      .find((candidate) => candidate.text() === 'admin.accounts.bulkActions.copyModelMapping')

    expect(button).toBeTruthy()
    await button!.trigger('click')

    expect(wrapper.emitted('copy-model-mapping')).toHaveLength(1)
  })

  it('hides copy mapping action when nothing is selected', () => {
    const wrapper = mountBar()

    expect(wrapper.text()).not.toContain('admin.accounts.bulkActions.copyModelMapping')
  })

  it('allows selecting all results before any row is selected', async () => {
    const wrapper = mountBar()
    const button = wrapper
      .findAll('button')
      .find((candidate) => candidate.text().includes('admin.accounts.bulkActions.selectAllResults'))

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('select-all-results')).toHaveLength(1)
  })

  it('preserves the upstream billing probe action', async () => {
    const wrapper = mountBar([1])
    const button = wrapper
      .findAll('button')
      .find((candidate) => candidate.text().includes('admin.accounts.bulkActions.probeUpstreamBilling'))

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('probe-upstream-billing')).toHaveLength(1)
  })
})
