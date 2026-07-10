import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VersionBadge.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('VersionBadge compact sidebar styles', () => {
  it('truncates long admin version text without losing the full hover title', () => {
    expect(componentSource).toContain('class="version-badge-button')
    expect(componentSource).toContain(':title="versionBadgeTitle"')
    expect(componentSource).toContain('class="version-badge-text ')
    expect(componentSource).toContain('const versionBadgeTitle = computed')
  })
})
