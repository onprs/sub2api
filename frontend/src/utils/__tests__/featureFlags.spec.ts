import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import { useAppStore } from '@/stores/app'
import {
  FeatureFlags,
  isFeatureFlagEnabled,
  makeSidebarFlag,
  resolveFeatureFlag,
} from '@/utils/featureFlags'
import type { PublicSettings } from '@/types'

vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn(),
}))

describe('featureFlags', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    delete (window as any).__APP_CONFIG__
  })

  it('resolves model pricing independently from available channels', () => {
    const store = useAppStore()
    store.cachedPublicSettings = {
      available_channels_enabled: false,
      model_pricing_enabled: true,
    } as PublicSettings

    expect(FeatureFlags.modelPricing.key).toBe('model_pricing_enabled')
    expect(isFeatureFlagEnabled(FeatureFlags.availableChannels)).toBe(false)
    expect(isFeatureFlagEnabled(FeatureFlags.modelPricing)).toBe(true)
  })
})

describe('FeatureFlags.subscription', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('reads subscription_enabled as an opt-out flag: visible before settings load', () => {
    expect(FeatureFlags.subscription.key).toBe('subscription_enabled')
    expect(FeatureFlags.subscription.mode).toBe('opt-out')
    expect(useAppStore().cachedPublicSettings).toBeNull()
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(true)
  })

  it('hides only when the backend explicitly sends false', () => {
    const store = useAppStore()
    const sidebarFlag = makeSidebarFlag(FeatureFlags.subscription)

    store.cachedPublicSettings = { subscription_enabled: false } as PublicSettings
    expect(sidebarFlag()).toBe(false)

    store.cachedPublicSettings = { subscription_enabled: true } as PublicSettings
    expect(sidebarFlag()).toBe(true)

    store.cachedPublicSettings = {} as PublicSettings
    expect(sidebarFlag()).toBe(true)
  })
})

describe('resolveFeatureFlag', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('reads an explicit boolean from the given settings object', () => {
    expect(resolveFeatureFlag({ subscription_enabled: false }, FeatureFlags.subscription)).toBe(false)
    expect(resolveFeatureFlag({ subscription_enabled: true }, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({ available_channels_enabled: true }, FeatureFlags.availableChannels)).toBe(true)
  })

  it('falls back to the declared mode when settings are missing or the key is absent', () => {
    expect(resolveFeatureFlag(undefined, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag(null, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({}, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({}, FeatureFlags.availableChannels)).toBe(false)
  })

  it('backs isFeatureFlagEnabled with the same resolution', () => {
    useAppStore().cachedPublicSettings = { subscription_enabled: false }
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(false)
  })
})
