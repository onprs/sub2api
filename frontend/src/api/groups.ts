/**
 * User Groups API endpoints (non-admin)
 * Handles group-related operations for regular users
 */

import { apiClient } from './client'
import type { ApiKeyRoutingHealthResponse, Group } from '@/types'

/**
 * Get available groups that the current user can bind to API keys
 * This returns groups based on user's permissions:
 * - Standard groups: public (non-exclusive) or explicitly allowed
 * - Subscription groups: user has active subscription
 * @returns List of available groups
 */
export async function getAvailable(): Promise<Group[]> {
  const { data } = await apiClient.get<Group[]>('/groups/available')
  return data
}

/**
 * Get current user's custom group rate multipliers
 * @returns Map of group_id to custom rate_multiplier
 */
export async function getUserGroupRates(): Promise<Record<number, number>> {
  const { data } = await apiClient.get<Record<number, number> | null>('/groups/rates')
  return data || {}
}

export async function getRoutingHealth(groupIDs: number[]): Promise<ApiKeyRoutingHealthResponse> {
  const uniqueGroupIDs = [...new Set(groupIDs.filter((groupID) => groupID > 0))]
  if (uniqueGroupIDs.length === 0) return { window_minutes: 30, items: [] }

  const batches: number[][] = []
  for (let offset = 0; offset < uniqueGroupIDs.length; offset += 200) {
    batches.push(uniqueGroupIDs.slice(offset, offset + 200))
  }
  const responses = await Promise.all(
    batches.map(async (batch) => {
      const { data } = await apiClient.get<ApiKeyRoutingHealthResponse>('/groups/routing-health', {
        params: { group_ids: batch.join(',') }
      })
      return data
    })
  )
  return {
    window_minutes: responses[0]?.window_minutes ?? 30,
    items: responses.flatMap((response) => response.items)
  }
}

export const userGroupsAPI = {
  getAvailable,
  getUserGroupRates,
  getRoutingHealth
}

export default userGroupsAPI
