import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar ticket navigation', () => {
  it('places user tickets between affiliate and profile', () => {
    const affiliate = componentSource.indexOf("path: '/affiliate'")
    const tickets = componentSource.indexOf("path: '/tickets'")
    const profile = componentSource.indexOf("path: '/profile'")
    expect(affiliate).toBeGreaterThan(-1)
    expect(tickets).toBeGreaterThan(affiliate)
    expect(profile).toBeGreaterThan(tickets)
  })

  it('places the admin queue immediately after user management', () => {
    const users = componentSource.indexOf("path: '/admin/users'")
    const tickets = componentSource.indexOf("path: '/admin/tickets'")
    const groups = componentSource.indexOf("path: '/admin/groups'")
    expect(tickets).toBeGreaterThan(users)
    expect(groups).toBeGreaterThan(tickets)
  })

  it('binds user and admin ticket navigation to the deployment capability', () => {
    expect(componentSource).toContain('const flagTicketing = () => ticketNotificationsStore.capabilities?.enabled')
    expect(componentSource.match(/featureFlag: flagTicketing/g)).toHaveLength(2)
  })

  it('renders 99+ badges when expanded and a red dot when collapsed', () => {
    expect(componentSource).toContain("return value > 99 ? '99+' : String(value)")
    expect(componentSource).toContain("sidebarCollapsed ? 'absolute right-2 top-2 h-2 w-2 rounded-full bg-rose-500'")
    expect(componentSource).toContain('ticketNotificationsStore.userUnread')
    expect(componentSource).toContain('ticketNotificationsStore.adminActionRequired')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })

  it('keeps long version badges inside the sidebar brand row', () => {
    expect(componentSource).toContain('<VersionBadge :version="siteVersion" class="min-w-0 max-w-full" />')
  })
})
