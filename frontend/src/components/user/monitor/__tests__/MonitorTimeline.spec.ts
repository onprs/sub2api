import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../MonitorTimeline.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('MonitorTimeline compact layout', () => {
  it('lets sixty timeline bars shrink inside narrow monitor cards', () => {
    expect(componentSource).toContain('class="monitor-timeline-bars')
    expect(componentSource).toContain('class="monitor-timeline-bar')
    expect(componentSource).not.toContain('min-w-[3px]')
  })
})
