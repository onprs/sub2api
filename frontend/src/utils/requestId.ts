const INTERNAL_REQUEST_ID_PREFIX = /^(?:client|local|generated):/

/** Hide billing provenance markers while preserving the request ID itself. */
export function formatRequestId(value: string | null | undefined): string {
  const requestId = value?.trim() ?? ''
  return requestId.replace(INTERNAL_REQUEST_ID_PREFIX, '')
}
