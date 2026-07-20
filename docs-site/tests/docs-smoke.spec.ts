import { expect, test } from '@playwright/test'

async function expectNoPageOverflow(page: import('@playwright/test').Page) {
  const overflow = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: document.documentElement.clientWidth
  }))
  expect(overflow.documentWidth).toBeLessThanOrEqual(overflow.viewportWidth + 1)
}

test('homepage exposes search, tasks and production entry points', async ({ page }, testInfo) => {
  const response = await page.goto('/')
  expect(response?.status()).toBe(200)

  await expect(page.getByRole('heading', { level: 1, name: 'OnprsCodexApi 文档' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'OnprsCodexApi Logo' })).toBeVisible()
  await expect(page.getByText('https://cdn-api.onprs.online/v1', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('link', { name: /打开控制台/ })).toHaveAttribute('href', 'https://cdn-api.onprs.online/')
  await expect(page.getByRole('link', { name: '查看渠道监控' })).toHaveAttribute(
    'href',
    'https://cdn-api.onprs.online/monitor'
  )

  await page.getByRole('button', { name: '搜索文档内容' }).click()
  const search = page.locator('.VPLocalSearchBox input')
  await expect(search).toBeVisible()
  await search.fill('FIVE_HOUR_LIMIT_EXCEEDED')
  await expect(page.locator('.VPLocalSearchBox a').first()).toBeVisible()
  await page.keyboard.press('Escape')

  await expectNoPageOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('homepage.png'), fullPage: true })
})

test('documentation navigation, clean URLs, copy and dark mode work', async ({ page, isMobile }, testInfo) => {
  const response = await page.goto('/getting-started/first-request')
  expect(response?.status()).toBe(200)
  await expect(page.getByRole('heading', { level: 1, name: '发送第一条请求' })).toBeVisible()

  const sidebarSections = [
    '快速开始',
    'API 使用',
    '客户端',
    '套餐与额度',
    '错误排查',
    '账号与安全',
    '常见问题',
    '文档更新'
  ]

  if (isMobile) {
    const mobileMenuButton = page.locator('.VPNavBarHamburger')
    await mobileMenuButton.click()
    await expect(page.locator('.VPNavScreen')).toBeVisible()

    const mobileTopNav = page.locator('.VPNavScreenMenu')
    await expect(mobileTopNav.getByRole('link')).toHaveCount(2)
    await expect(mobileTopNav.getByRole('link', { name: '快速开始', exact: true })).toBeVisible()
    await expect(mobileTopNav.getByRole('link', { name: /返回控制台/ })).toHaveAttribute(
      'href',
      'https://cdn-api.onprs.online/'
    )

    await page.locator('.VPSwitchAppearance:visible').click()
    await expect(page.locator('html')).toHaveClass(/dark/)
    await mobileMenuButton.click()
    await expect(page.locator('.VPNavScreen')).toBeHidden()

    await page.getByRole('button', { name: '文档目录' }).click()
    const sidebar = page.locator('.VPSidebar')
    await expect(sidebar).toHaveClass(/open/)
    for (const section of sidebarSections) {
      await expect(sidebar.getByText(section, { exact: true })).toBeVisible()
    }
    await expect
      .poll(() => sidebar.evaluate((element) => getComputedStyle(element).transform))
      .toBe('matrix(1, 0, 0, 1, 0, 0)')
    await page.screenshot({ path: testInfo.outputPath('unified-sidebar.png') })
    await page.keyboard.press('Escape')
    await expect(sidebar).not.toHaveClass(/open/)
  } else {
    const topNav = page.locator('.VPNavBarMenu')
    await expect(topNav.getByRole('link')).toHaveCount(2)
    await expect(topNav.getByRole('link', { name: '快速开始', exact: true })).toBeVisible()
    await expect(topNav.getByRole('link', { name: /返回控制台/ })).toHaveAttribute(
      'href',
      'https://cdn-api.onprs.online/'
    )

    const sidebar = page.locator('.VPSidebar')
    await expect(sidebar).toBeVisible()
    for (const section of sidebarSections) {
      await expect(sidebar.getByText(section, { exact: true })).toBeVisible()
    }
    await expect(sidebar.getByRole('link', { name: '创建 API Key', exact: true })).toBeVisible()
    await page.locator('.VPSwitchAppearance:visible').click()
    await expect(page.locator('html')).toHaveClass(/dark/)
  }

  const copyButton = page.getByRole('button', { name: 'Copy Code' }).first()
  await copyButton.click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('ONPRS_API_KEY')

  await expectNoPageOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('first-request-dark.png'), fullPage: true })

  const cleanUrlResponse = await page.goto('/plans/rolling-quotas')
  expect(cleanUrlResponse?.status()).toBe(200)
  await expect(page.getByText('三个窗口同时检查')).toBeVisible()
  if (!isMobile) {
    await expect(page.locator('.VPSidebar').getByRole('link', { name: '5h / 7d / 30d 额度' })).toBeVisible()
  }
})

test('unknown routes return a real custom 404', async ({ page }, testInfo) => {
  const response = await page.goto('/this-page-does-not-exist')
  expect(response?.status()).toBe(404)
  await expect(page.getByRole('heading', { level: 1, name: '没有找到这个页面' })).toBeVisible()
  await expect(page.getByRole('link', { name: '进入错误排查' })).toBeVisible()
  expect((await page.request.get('/deployment')).status()).toBe(404)
  expect((await page.request.get('/readme')).status()).toBe(404)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('404.png'), fullPage: true })
})
