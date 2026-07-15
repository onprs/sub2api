import { readdirSync, readFileSync } from 'node:fs'
import { extname, join } from 'node:path'

export { flattenLocaleKeys } from '../localeUtils'

export interface StaticLocaleCall {
  file: string
  key: string
  line: number
}

const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx', '.vue'])
const TEST_FILE_PATTERN = /(?:__tests__|\.(?:spec|test)\.[^.]+$)/
const STATIC_TRANSLATION_CALL = /(?:^|[^\w$])(?:\$t|t)\(\s*(['"`])([^'"`\r\n]+)\1(?=\s*(?:,|\)))/gm

export function collectStaticLocaleCalls(sourceRoot: string): StaticLocaleCall[] {
  const calls: StaticLocaleCall[] = []

  function visit(path: string): void {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      const fullPath = join(path, entry.name)
      if (entry.isDirectory()) {
        if (entry.name !== '__tests__') visit(fullPath)
        continue
      }

      if (!SOURCE_EXTENSIONS.has(extname(entry.name)) || TEST_FILE_PATTERN.test(fullPath)) continue

      const source = readFileSync(fullPath, 'utf8')
      for (const match of source.matchAll(STATIC_TRANSLATION_CALL)) {
        const key = match[2]
        if (key.includes('${')) continue

        const matchIndex = match.index ?? 0
        calls.push({
          file: fullPath.slice(sourceRoot.length + 1).replaceAll('\\', '/'),
          key,
          line: source.slice(0, matchIndex).split('\n').length
        })
      }
    }
  }

  visit(sourceRoot)
  return calls.sort((left, right) =>
    left.key.localeCompare(right.key) || left.file.localeCompare(right.file) || left.line - right.line
  )
}
