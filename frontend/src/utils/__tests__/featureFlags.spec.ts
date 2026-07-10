import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import { useAppStore } from '@/stores/app'
import {
  FeatureFlags,
  isFeatureFlagEnabled,
  type FeatureFlagDefinition
} from '@/utils/featureFlags'

describe('featureFlags', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('resolves model pricing independently from available channels', () => {
    const store = useAppStore()
    store.cachedPublicSettings = {
      available_channels_enabled: false,
      model_pricing_enabled: true
    } as any

    const registry = FeatureFlags as Record<string, FeatureFlagDefinition>
    expect(registry.modelPricing?.key).toBe('model_pricing_enabled')
    expect(isFeatureFlagEnabled(registry.availableChannels)).toBe(false)
    expect(isFeatureFlagEnabled(registry.modelPricing)).toBe(true)
  })
})
