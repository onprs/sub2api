import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'
import { collectStaticLocaleCalls, flattenLocaleKeys } from './localeContractUtils'

const sourceRoot = resolve(process.cwd(), 'src')
const enKeys = new Set(flattenLocaleKeys(en))
const zhKeys = new Set(flattenLocaleKeys(zh))

function difference(left: Set<string>, right: Set<string>): string[] {
  return [...left].filter(key => !right.has(key)).sort()
}

describe('locale contracts', () => {
  it('keeps English and Chinese leaf-key sets identical', () => {
    expect({
      onlyEnglish: difference(enKeys, zhKeys),
      onlyChinese: difference(zhKeys, enKeys)
    }).toEqual({ onlyEnglish: [], onlyChinese: [] })
  })

  it('defines every static translation call in both locales', () => {
    const missing = new Map<string, string>()

    for (const call of collectStaticLocaleCalls(sourceRoot)) {
      const locales = [
        !enKeys.has(call.key) ? 'en' : '',
        !zhKeys.has(call.key) ? 'zh' : ''
      ].filter(Boolean)

      if (locales.length > 0 && !missing.has(call.key)) {
        missing.set(call.key, `${call.key} [${locales.join(', ')}] at ${call.file}:${call.line}`)
      }
    }

    expect([...missing.values()]).toEqual([])
  })
})
