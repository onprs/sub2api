import { beforeEach, describe, expect, it, vi } from 'vitest'

describe('initI18n', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
    Object.defineProperty(navigator, 'language', {
      configurable: true,
      value: 'zh-CN'
    })
  })

  it('loads the English fallback before the preferred Chinese locale', async () => {
    const { getLocale, i18n, initI18n } = await import('../index')

    await initI18n()

    expect(getLocale()).toBe('zh')
    expect(i18n.global.availableLocales).toEqual(expect.arrayContaining(['en', 'zh']))
    expect(document.documentElement.lang).toBe('zh')

    i18n.global.mergeLocaleMessage('en', {
      contractTest: {
        fallbackOnly: () => 'English fallback is available'
      }
    })

    expect(i18n.global.t('contractTest.fallbackOnly')).toBe('English fallback is available')
  })
})
