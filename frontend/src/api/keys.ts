/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from './client'
import type {
  ApiKey,
  ApiKeyRoutingInput,
  CreateApiKeyRequest,
  UpdateApiKeyRequest,
  PaginatedResponse
} from '@/types'

export type CliImportScriptOS = 'windows' | 'linux'

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: {
    search?: string
    status?: string
    group_id?: number | string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * Get API key by ID
 * @param id - API key ID
 * @returns API key details
 */
export async function getById(id: number): Promise<ApiKey> {
  const { data } = await apiClient.get<ApiKey>(`/keys/${id}`)
  return data
}

/**
 * Create new API key
 * @param name - Key name
 * @param groupId - Optional group ID
 * @param customKey - Optional custom key value
 * @param ipWhitelist - Optional IP whitelist
 * @param ipBlacklist - Optional IP blacklist
 * @param quota - Optional quota limit in USD (0 = unlimited)
 * @param expiresInDays - Optional days until expiry (undefined = never expires)
 * @param rateLimitData - Optional rate limit fields
 * @returns Created API key
 */
export async function create(
  name: string,
  groupId?: number | null,
  customKey?: string,
  ipWhitelist?: string[],
  ipBlacklist?: string[],
  quota?: number,
  expiresInDays?: number,
  rateLimitData?: { rate_limit_5h?: number; rate_limit_1d?: number; rate_limit_7d?: number },
  routing?: ApiKeyRoutingInput
): Promise<ApiKey> {
  const payload: CreateApiKeyRequest = { name }
  if (groupId !== undefined) {
    payload.group_id = groupId
  }
  if (routing) {
    payload.routing = routing
  }
  if (customKey) {
    payload.custom_key = customKey
  }
  if (ipWhitelist && ipWhitelist.length > 0) {
    payload.ip_whitelist = ipWhitelist
  }
  if (ipBlacklist && ipBlacklist.length > 0) {
    payload.ip_blacklist = ipBlacklist
  }
  if (quota !== undefined && quota > 0) {
    payload.quota = quota
  }
  if (expiresInDays !== undefined && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }
  if (rateLimitData?.rate_limit_5h && rateLimitData.rate_limit_5h > 0) {
    payload.rate_limit_5h = rateLimitData.rate_limit_5h
  }
  if (rateLimitData?.rate_limit_1d && rateLimitData.rate_limit_1d > 0) {
    payload.rate_limit_1d = rateLimitData.rate_limit_1d
  }
  if (rateLimitData?.rate_limit_7d && rateLimitData.rate_limit_7d > 0) {
    payload.rate_limit_7d = rateLimitData.rate_limit_7d
  }

  const { data } = await apiClient.post<ApiKey>('/keys', payload)
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates)
  return data
}

export async function updateRouting(id: number, routing: ApiKeyRoutingInput): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}/routing`, routing)
  return data
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive'): Promise<ApiKey> {
  return update(id, { status })
}

/**
 * Download a generated one-click CLI import script for a user API key.
 */
export async function downloadCliImportScript(id: number, os: CliImportScriptOS): Promise<void> {
  let response
  try {
    response = await apiClient.get<Blob>(`/keys/${id}/cli-import/script`, {
      params: { os },
      responseType: 'blob',
      validateStatus: () => true
    })
  } catch (error) {
    throw await normalizeCliImportDownloadError(error)
  }
  if (response.status >= 400) {
    throw await normalizeCliImportDownloadError({ response })
  }
  const blob = response.data
  const disposition = String(response.headers?.['content-disposition'] || '')
  const fallback = os === 'windows' ? 'sub2api-cli-import.bat' : 'sub2api-cli-import.sh'
  const filename = parseContentDispositionFilename(disposition) || fallback
  const url = URL.createObjectURL(blob)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    link.remove()
  } finally {
    URL.revokeObjectURL(url)
  }
}

async function normalizeCliImportDownloadError(error: unknown): Promise<Error> {
  const responseData = (error as any)?.response?.data
  if (responseData instanceof Blob || typeof responseData?.text === 'function') {
    try {
      const text = await blobLikeToText(responseData)
      return cliImportDownloadErrorFromPayload(JSON.parse(text))
    } catch {
      return new Error((error as any)?.message || 'Download failed')
    }
  }
  if (typeof responseData === 'string' && responseData.trim()) {
    try {
      return cliImportDownloadErrorFromPayload(JSON.parse(responseData))
    } catch {
      return new Error(responseData)
    }
  }
  if (responseData && typeof responseData === 'object') {
    return cliImportDownloadErrorFromPayload(responseData)
  }
  const message = (error as any)?.metadata?.models
    ? `${(error as any).message || 'Download failed'}: ${(error as any).metadata.models}`
    : (error as any)?.message || 'Download failed'
  return Object.assign(new Error(message), {
    reason: (error as any)?.reason,
    metadata: (error as any)?.metadata
  })
}

async function blobLikeToText(value: Blob | { text?: () => Promise<string>; arrayBuffer?: () => Promise<ArrayBuffer> }): Promise<string> {
  if (typeof value.text === 'function') {
    return value.text()
  }
  if (typeof value.arrayBuffer === 'function') {
    return new TextDecoder().decode(await value.arrayBuffer())
  }
  if (value instanceof Blob && typeof FileReader !== 'undefined') {
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result || ''))
      reader.onerror = () => reject(reader.error || new Error('failed to read blob'))
      reader.readAsText(value)
    })
  }
  throw new Error('unsupported blob payload')
}

function cliImportDownloadErrorFromPayload(payload: any): Error {
  const models = payload?.metadata?.models
  const message = models
    ? `${payload?.message || 'Download failed'}: ${models}`
    : payload?.message || payload?.reason || 'Download failed'
  return Object.assign(new Error(message), {
    reason: payload?.reason,
    metadata: payload?.metadata
  })
}

function parseContentDispositionFilename(disposition: string): string | null {
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (encoded?.[1]) {
    try {
      return decodeURIComponent(encoded[1].trim())
    } catch {
      return encoded[1].trim()
    }
  }
  const quoted = disposition.match(/filename="([^"]+)"/i)
  if (quoted?.[1]) {
    return quoted[1].trim()
  }
  const plain = disposition.match(/filename=([^;]+)/i)
  return plain?.[1]?.trim() || null
}

export const keysAPI = {
  list,
  getById,
  create,
  update,
  updateRouting,
  delete: deleteKey,
  toggleStatus,
  downloadCliImportScript
}

export default keysAPI
