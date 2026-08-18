export interface TempUnschedRuleForm {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
}

export interface TempUnschedPresetDefinition {
  id: string
  labelKey: string
  descriptionKey: string
  errorCode: number
  keywords: string
  durationMinutes: number
}

export interface TempUnschedPreset {
  id: string
  label: string
  rule: TempUnschedRuleForm
}

export const TEMP_UNSCHED_PRESET_DEFINITIONS: readonly TempUnschedPresetDefinition[] = [
  {
    id: 'request-timeout',
    labelKey: 'admin.accounts.tempUnschedulable.presets.requestTimeoutLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.requestTimeoutDesc',
    errorCode: 408,
    keywords: 'request timeout, request_timeout, request timed out, timed out',
    durationMinutes: 5
  },
  {
    id: 'rate-limit',
    labelKey: 'admin.accounts.tempUnschedulable.presets.rateLimitLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.rateLimitDesc',
    errorCode: 429,
    keywords: 'rate limit, rate_limit, too many requests, too_many_requests, resource exhausted, resource_exhausted, resource has been exhausted, quota exceeded, quota_exceeded',
    durationMinutes: 10
  },
  {
    id: 'internal-error',
    labelKey: 'admin.accounts.tempUnschedulable.presets.internalErrorLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.internalErrorDesc',
    errorCode: 500,
    keywords: 'internal server error, internal error, internal_error, server error, server_error',
    durationMinutes: 10
  },
  {
    id: 'bad-gateway',
    labelKey: 'admin.accounts.tempUnschedulable.presets.badGatewayLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.badGatewayDesc',
    errorCode: 502,
    keywords: 'bad gateway, bad_gateway, upstream error, upstream_error, upstream connect error, upstream reset, connection reset, upstream service temporarily unavailable',
    durationMinutes: 10
  },
  {
    id: 'service-unavailable',
    labelKey: 'admin.accounts.tempUnschedulable.presets.unavailableLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.unavailableDesc',
    errorCode: 503,
    keywords: 'service unavailable, service_unavailable, temporarily unavailable, maintenance, capacity exhausted, capacity_exhausted, no healthy upstream',
    durationMinutes: 30
  },
  {
    id: 'gateway-timeout',
    labelKey: 'admin.accounts.tempUnschedulable.presets.gatewayTimeoutLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.gatewayTimeoutDesc',
    errorCode: 504,
    keywords: 'gateway timeout, gateway_timeout, upstream timeout, upstream_timeout, upstream request timeout, timed out',
    durationMinutes: 10
  },
  {
    id: 'cloudflare-timeout',
    labelKey: 'admin.accounts.tempUnschedulable.presets.cloudflareTimeoutLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.cloudflareTimeoutDesc',
    errorCode: 524,
    keywords: 'a timeout occurred, error code: 524, error code 524, 524 gateway timeout, cloudflare, upstream timeout, request timed out',
    durationMinutes: 10
  },
  {
    id: 'overload',
    labelKey: 'admin.accounts.tempUnschedulable.presets.overloadLabel',
    descriptionKey: 'admin.accounts.tempUnschedulable.presets.overloadDesc',
    errorCode: 529,
    keywords: 'overload, overloaded_error, too many requests, capacity exceeded',
    durationMinutes: 60
  }
]

export function buildTempUnschedPresets(translate: (key: string) => string): TempUnschedPreset[] {
  return TEMP_UNSCHED_PRESET_DEFINITIONS.map((definition) => ({
    id: definition.id,
    label: translate(definition.labelKey),
    rule: {
      error_code: definition.errorCode,
      keywords: definition.keywords,
      duration_minutes: definition.durationMinutes,
      description: translate(definition.descriptionKey)
    }
  }))
}
