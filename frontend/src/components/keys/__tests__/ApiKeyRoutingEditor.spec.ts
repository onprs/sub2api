import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import ApiKeyRoutingEditor from '../ApiKeyRoutingEditor.vue'
import type {
  ApiKeyRoutingDraft,
  ApiKeyRoutingGroupHealth,
  Group,
  GroupPlatform
} from '@/types'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'keys.routing.selectedCount') return `${params?.count}/${params?.max}`
      if (key === 'keys.routing.sampleCount') return `${params?.count} samples`
      return key
    }
  })
}))

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<button data-test="platform-select">{{ modelValue }}</button>'
}

const GroupBadgeStub = {
  name: 'GroupBadge',
  props: ['name'],
  template: '<span data-test="group-name">{{ name }}</span>'
}

const IconStub = {
  name: 'Icon',
  template: '<span />'
}

const createGroup = (id: number, name: string, platform: GroupPlatform, status: Group['status'] = 'active') => ({
  id,
  name,
  platform,
  status,
  description: null,
  subscription_type: 'standard',
  rate_multiplier: 1,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1
}) as Group

const mountEditor = (
  modelValue: ApiKeyRoutingDraft,
  groups: Group[],
  maxCandidates = 20,
  health: Record<number, ApiKeyRoutingGroupHealth> = {}
) =>
  mount(ApiKeyRoutingEditor, {
    props: { modelValue, groups, maxCandidates, health },
    global: {
      stubs: {
        Select: SelectStub,
        GroupBadge: GroupBadgeStub,
        Icon: IconStub
      }
    }
  })

describe('ApiKeyRoutingEditor', () => {
  it('clears candidate groups when the platform changes', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'balanced',
        groups: [{ group_id: 1, priority: 0 }]
      },
      [
        createGroup(1, 'Anthropic A', 'anthropic'),
        createGroup(2, 'OpenAI A', 'openai')
      ]
    )

    await wrapper.findComponent({ name: 'Select' }).vm.$emit('update:modelValue', 'openai')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      { platform: 'openai', strategy: 'balanced', groups: [] }
    ])
  })

  it('appends candidates with normalized priorities', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [{ group_id: 1, priority: 0 }]
      },
      [
        createGroup(1, 'Group A', 'anthropic'),
        createGroup(2, 'Group B', 'anthropic')
      ]
    )

    await wrapper.findAll('input[type="checkbox"]')[1].trigger('change')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [
          { group_id: 1, priority: 0 },
          { group_id: 2, priority: 1 }
        ]
      }
    ])
  })

  it('reorders selected candidates through priority controls', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [
          { group_id: 1, priority: 0 },
          { group_id: 2, priority: 1 }
        ]
      },
      [
        createGroup(1, 'Group A', 'anthropic'),
        createGroup(2, 'Group B', 'anthropic')
      ]
    )

    await wrapper.findAll('button[title="keys.routing.moveUp"]')[1].trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [
          { group_id: 2, priority: 0 },
          { group_id: 1, priority: 1 }
        ]
      }
    ])
  })

  it('disables additional candidates at the configured limit', () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'balanced',
        groups: [
          { group_id: 1, priority: 0 },
          { group_id: 2, priority: 1 }
        ]
      },
      [
        createGroup(1, 'Group A', 'anthropic'),
        createGroup(2, 'Group B', 'anthropic'),
        createGroup(3, 'Group C', 'anthropic'),
        createGroup(4, 'Inactive', 'anthropic', 'inactive')
      ],
      2
    )

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(3)
    expect(checkboxes[0].attributes('disabled')).toBeUndefined()
    expect(checkboxes[1].attributes('disabled')).toBeUndefined()
    expect(checkboxes[2].attributes('disabled')).toBeDefined()
  })

  it('keeps an inactive selected candidate visible so it can be removed', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [
          { group_id: 1, priority: 0 },
          { group_id: 2, priority: 1 }
        ]
      },
      [
        createGroup(1, 'Active Group', 'anthropic'),
        createGroup(2, 'Inactive Group', 'anthropic', 'inactive')
      ]
    )

    expect(wrapper.findAll('[data-test="group-name"]').map((item) => item.text())).toEqual([
      'Active Group',
      'Inactive Group'
    ])
    expect(wrapper.text()).toContain('common.inactive')

    await wrapper.findAll('input[type="checkbox"]')[1].trigger('change')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [{ group_id: 1, priority: 0 }]
      }
    ])
  })

  it('renders a missing selected candidate so it can be removed', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [{ group_id: 99, priority: 0 }]
      },
      []
    )

    expect(wrapper.get('[data-test="group-name"]').text()).toBe('keys.routing.missingGroup')
    expect(wrapper.text()).toContain('common.inactive')

    await wrapper.get('input[type="checkbox"]').trigger('change')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      { platform: 'anthropic', strategy: 'manual', groups: [] }
    ])
  })

  it('keeps a selected candidate visible after its platform changes', async () => {
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [
          { group_id: 1, priority: 0 },
          { group_id: 2, priority: 1 }
        ]
      },
      [
        createGroup(1, 'Anthropic Group', 'anthropic'),
        createGroup(2, 'Moved Group', 'openai')
      ]
    )

    expect(wrapper.findAll('[data-test="group-name"]').map((item) => item.text())).toEqual([
      'Anthropic Group',
      'Moved Group'
    ])

    await wrapper.findAll('input[type="checkbox"]')[1].trigger('change')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      {
        platform: 'anthropic',
        strategy: 'manual',
        groups: [{ group_id: 1, priority: 0 }]
      }
    ])
  })

  it('renders recent status, latency, stability, and refreshes health data', async () => {
    const health: Record<number, ApiKeyRoutingGroupHealth> = {
      1: {
        group_id: 1,
        status: 'operational',
        success_rate: 97.5,
        average_latency_ms: 245,
        sample_count: 40,
        last_observed_at: '2026-08-21T00:00:00Z'
      },
      2: {
        group_id: 2,
        status: 'failed',
        success_rate: 25,
        average_latency_ms: null,
        sample_count: 4,
        last_observed_at: '2026-08-21T00:01:00Z'
      }
    }
    const wrapper = mountEditor(
      {
        platform: 'anthropic',
        strategy: 'stability_first',
        groups: [{ group_id: 1, priority: 0 }]
      },
      [
        createGroup(1, 'Group A', 'anthropic'),
        createGroup(2, 'Group B', 'anthropic')
      ],
      20,
      health
    )

    expect(wrapper.text()).toContain('keys.routing.healthStatus.operational')
    expect(wrapper.text()).toContain('keys.routing.healthStatus.failed')
    expect(wrapper.text()).toContain('245 ms')
    expect(wrapper.text()).toContain('97.5%')
    expect(wrapper.text()).toContain('40 samples')
    expect(wrapper.text()).toContain('keys.routing.healthWindowDays')

    await wrapper.get('button[title="keys.routing.refreshHealth"]').trigger('click')
    expect(wrapper.emitted('refresh-health')).toHaveLength(1)
  })

  it('emits the selected routing strategy without changing candidates', async () => {
    const draft: ApiKeyRoutingDraft = {
      platform: 'anthropic',
      strategy: 'balanced',
      groups: [{ group_id: 1, priority: 0 }]
    }
    const wrapper = mountEditor(draft, [createGroup(1, 'Group A', 'anthropic')])

    const costButton = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.routing.strategies.costFirst')
    )
    expect(costButton).toBeDefined()
    await costButton!.trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      { ...draft, strategy: 'cost_first' }
    ])
  })
})
