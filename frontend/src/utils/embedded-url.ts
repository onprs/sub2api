/**
 * Shared URL builder for iframe-embedded pages.
 *
 * Security contract:
 *   - Never accept or emit authentication tokens (the user JWT lives in
 *     localStorage and must not leak into admin-configured external URLs).
 *   - Only attach non-sensitive tracking parameters: user_id, theme, lang,
 *     ui_mode, src_host and a truncated src_url (pathname only, no query
 *     string or hash, to avoid leaking sensitive query parameters).
 *   - Only HTTPS targets are considered embeddable; http:// is rejected by the
 *     view layer via `isSecureEmbeddableUrl`.
 */

const EMBEDDED_USER_ID_QUERY_KEY = 'user_id'
const EMBEDDED_THEME_QUERY_KEY = 'theme'
const EMBEDDED_LANG_QUERY_KEY = 'lang'
const EMBEDDED_UI_MODE_QUERY_KEY = 'ui_mode'
const EMBEDDED_UI_MODE_VALUE = 'embedded'
const EMBEDDED_SRC_HOST_QUERY_KEY = 'src_host'
const EMBEDDED_SRC_QUERY_KEY = 'src_url'

export interface EmbeddedUrlOptions {
  userId?: number | null
  theme?: 'light' | 'dark'
  lang?: string
}

/**
 * Build an embed URL carrying only non-sensitive tracking parameters.
 *
 * This function intentionally does NOT accept any authentication token. Legacy
 * callers that previously forwarded the user Bearer JWT must migrate here.
 */
export function buildEmbeddedUrlSafe(
  baseUrl: string,
  options: EmbeddedUrlOptions = {},
): string {
  const { userId, theme = 'light', lang } = options
  if (!baseUrl) return baseUrl
  try {
    const url = new URL(baseUrl)
    if (userId) {
      url.searchParams.set(EMBEDDED_USER_ID_QUERY_KEY, String(userId))
    }
    url.searchParams.set(EMBEDDED_THEME_QUERY_KEY, theme)
    if (lang) {
      url.searchParams.set(EMBEDDED_LANG_QUERY_KEY, lang)
    }
    url.searchParams.set(EMBEDDED_UI_MODE_QUERY_KEY, EMBEDDED_UI_MODE_VALUE)
    // Source tracking: let the embedded page know which host embeds it.
    // We only forward the origin (host) and a truncated pathname so that any
    // sensitive query parameters or fragment identifiers in the parent URL
    // never reach the third-party page.
    if (typeof window !== 'undefined') {
      url.searchParams.set(EMBEDDED_SRC_HOST_QUERY_KEY, window.location.origin)
      url.searchParams.set(EMBEDDED_SRC_QUERY_KEY, window.location.pathname)
    }
    return url.toString()
  } catch {
    return baseUrl
  }
}

/**
 * Determine whether a URL may be loaded inside an admin-configured embed.
 *
 * Only HTTPS targets are allowed. `http://` is rejected to prevent leaking
 * tracking parameters over plaintext transport and to flag legacy insecure
 * URLs at the view layer.
 */
export function isSecureEmbeddableUrl(url: string): boolean {
  return typeof url === 'string' && url.startsWith('https://')
}

export function detectTheme(): 'light' | 'dark' {
  if (typeof document === 'undefined') return 'light'
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}