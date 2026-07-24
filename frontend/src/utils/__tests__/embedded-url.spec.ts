import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildEmbeddedUrlSafe,
  detectTheme,
  isSecureEmbeddableUrl,
} from '../embedded-url'

describe('embedded-url', () => {
  const originalLocation = window.location

  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      value: {
        origin: 'https://app.example.com',
        href: 'https://app.example.com/user/purchase?session=secret&token=leak',
        pathname: '/user/purchase',
      },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('attaches non-sensitive tracking params (user_id, theme, lang, ui_mode, src_host) without a token', () => {
    const result = buildEmbeddedUrlSafe('https://pay.example.com/checkout?plan=pro', {
      userId: 42,
      theme: 'dark',
      lang: 'zh-CN',
    })

    const url = new URL(result)
    expect(url.searchParams.get('plan')).toBe('pro')
    expect(url.searchParams.get('user_id')).toBe('42')
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('lang')).toBe('zh-CN')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.get('src_host')).toBe('https://app.example.com')
    // The auth token query key must never be emitted.
    expect(url.searchParams.has('token')).toBe(false)
  })

  it('truncates src_url to the pathname and never leaks query-string secrets', () => {
    const result = buildEmbeddedUrlSafe('https://pay.example.com/checkout', {
      userId: 42,
    })

    const url = new URL(result)
    expect(url.searchParams.get('src_url')).toBe('/user/purchase')
    // The parent-side sensitive query params must not be forwarded.
    expect(url.searchParams.get('src_url')).not.toContain('session=')
    expect(url.searchParams.get('src_url')).not.toContain('token=')
  })

  it('does not accept or emit any authentication token', () => {
    // The Safe builder has no authToken parameter; build with only tracking fields.
    const result = buildEmbeddedUrlSafe('https://pay.example.com/checkout', {
      userId: 1,
      theme: 'light',
    })

    const url = new URL(result)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.has('auth_token')).toBe(false)
  })

  it('omits optional params when they are empty', () => {
    const result = buildEmbeddedUrlSafe('https://pay.example.com/checkout')

    const url = new URL(result)
    expect(url.searchParams.get('theme')).toBe('light')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(url.searchParams.has('lang')).toBe(false)
  })

  it('returns original string for invalid url input', () => {
    expect(buildEmbeddedUrlSafe('not a url', { userId: 1 })).toBe('not a url')
  })

  describe('isSecureEmbeddableUrl', () => {
    it('accepts https URLs', () => {
      expect(isSecureEmbeddableUrl('https://example.com/page')).toBe(true)
    })

    it('rejects http URLs (view-layer blocks insecure embeds)', () => {
      expect(isSecureEmbeddableUrl('http://example.com/page')).toBe(false)
    })

    it('rejects empty, relative and javascript: URLs', () => {
      expect(isSecureEmbeddableUrl('')).toBe(false)
      expect(isSecureEmbeddableUrl('/relative/path')).toBe(false)
      expect(isSecureEmbeddableUrl('javascript:alert(1)')).toBe(false)
      expect(isSecureEmbeddableUrl('data:text/html,<b>')).toBe(false)
    })
  })

  it('detects dark mode from document root class', () => {
    document.documentElement.classList.add('dark')
    expect(detectTheme()).toBe('dark')
  })
})