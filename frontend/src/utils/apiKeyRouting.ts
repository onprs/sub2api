import type {
  ApiKey,
  ApiKeyRoutingDraft,
  ApiKeyRoutingInput,
  Group
} from '@/types'

export const createEmptyRoutingDraft = (): ApiKeyRoutingDraft => ({
  platform: '',
  strategy: 'balanced',
  groups: []
})

export const createRoutingDraftFromApiKey = (apiKey: ApiKey): ApiKeyRoutingDraft => {
  if (apiKey.routing) {
    return {
      platform: apiKey.routing.platform,
      strategy: apiKey.routing.strategy,
      groups: apiKey.routing.groups.map((candidate) => ({
        group_id: candidate.group_id,
        priority: candidate.priority
      }))
    }
  }
  if (apiKey.group) {
    return {
      platform: apiKey.group.platform,
      strategy: 'manual',
      groups: [{ group_id: apiKey.group.id, priority: 0 }]
    }
  }
  return createEmptyRoutingDraft()
}

export const createRoutingInput = (draft: ApiKeyRoutingDraft): ApiKeyRoutingInput | null => {
  if (!draft.platform || draft.groups.length === 0) return null
  return {
    platform: draft.platform,
    strategy: draft.strategy,
    groups: [...draft.groups]
      .sort((left, right) => left.priority - right.priority || left.group_id - right.group_id)
      .map((candidate, priority) => ({ group_id: candidate.group_id, priority }))
  }
}

export const getRoutingPrimaryGroup = (apiKey: ApiKey): Group | undefined => {
  const candidates = apiKey.routing?.groups
  if (candidates?.length) {
    return [...candidates]
      .sort((left, right) => left.priority - right.priority || left.group_id - right.group_id)[0]
      ?.group || apiKey.group
  }
  return apiKey.group
}

export const getRoutingGroupCount = (apiKey: ApiKey): number =>
  apiKey.routing?.groups.length || (apiKey.group ? 1 : 0)
