import en from '../locales/en'
import zh from '../locales/zh'

export type TestLocale = 'en' | 'zh'

export const testLocale: { value: TestLocale } = { value: 'en' }

const locales = { en, zh } as const

export function translateLocaleMessage(
  key: string,
  params?: Record<string, string | number>,
  locale: TestLocale = testLocale.value
): string {
  const message = key.split('.').reduce<unknown>((value, part) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[part]
  }, locales[locale])

  if (typeof message !== 'string') return key

  return message.replace(/\{(\w+)\}/g, (_match, name: string) =>
    String(params?.[name] ?? `{${name}}`)
  )
}
