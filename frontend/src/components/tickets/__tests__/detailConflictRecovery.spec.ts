import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const testDir = dirname(fileURLToPath(import.meta.url))
const userDetailSource = readFileSync(resolve(testDir, '../../../views/user/TicketDetailView.vue'), 'utf8')
const adminDetailSource = readFileSync(resolve(testDir, '../../../views/admin/TicketDetailView.vue'), 'utf8')
const createSource = readFileSync(resolve(testDir, '../../../views/user/TicketCreateView.vue'), 'utf8')

describe('ticket mutation intent recovery', () => {
  it.each([
    ['user', userDetailSource],
    ['admin', adminDetailSource],
  ])('renews the %s mutation key after refreshing a version conflict', (_name, source) => {
    const conflictBranch = source.slice(source.indexOf("apiError.reason === 'TICKET_VERSION_CONFLICT'"))

    expect(conflictBranch).toContain('intentKey = null')
    expect(conflictBranch.indexOf('intentKey = null')).toBeLessThan(conflictBranch.indexOf('await load(true, true)'))
  })

  it('renews the create key when the user changes the request payload', () => {
    const payloadWatcher = createSource.slice(createSource.indexOf('watch(['), createSource.indexOf('async function loadResources'))

    expect(payloadWatcher).toContain('pendingAttachments')
    expect(payloadWatcher).toContain('selectedResourceId')
    expect(payloadWatcher).toContain('intentKey = null')
  })
})
