const links = [
  { name: 'console', url: 'https://cdn.api.onprs.top/' },
  { name: 'health', url: 'https://api.onprs.top/health' },
  { name: 'keys', url: 'https://cdn.api.onprs.top/keys' },
  { name: 'channel monitor', url: 'https://cdn.api.onprs.top/monitor' },
  { name: 'usage', url: 'https://cdn.api.onprs.top/usage' },
  { name: 'subscriptions', url: 'https://cdn.api.onprs.top/subscriptions' },
  { name: 'orders', url: 'https://cdn.api.onprs.top/orders' },
  { name: 'terms', url: 'https://cdn.api.onprs.top/legal/terms-of-service' },
  { name: 'privacy', url: 'https://cdn.api.onprs.top/legal/privacy-policy' },
  { name: 'anthropic gateway docs', url: 'https://docs.anthropic.com/en/docs/claude-code/llm-gateway' },
  { name: 'codex config docs', url: 'https://developers.openai.com/codex/config-reference', allowed: [403] },
  { name: 'opencode provider docs', url: 'https://opencode.ai/docs/providers' },
  { name: 'gemini cli config docs', url: 'https://geminicli.com/docs/reference/configuration' }
]

const failures = []
const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds))

for (const link of links) {
  let failure = ''
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 20_000)
    try {
      const response = await fetch(link.url, {
        redirect: 'follow',
        signal: controller.signal,
        headers: {
          accept: 'text/html,application/json;q=0.9,*/*;q=0.8',
          'user-agent': 'sub2api-docs-link-check/1.0'
        }
      })
      const allowed = response.ok || link.allowed?.includes(response.status)
      console.log(`${allowed ? 'ok' : 'fail'} status=${response.status} attempt=${attempt} name=${link.name} url=${link.url}`)
      if (allowed) {
        failure = ''
        break
      }
      failure = `${link.name}: HTTP ${response.status}`
      if (response.status < 500) break
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      failure = `${link.name}: ${message}`
      console.error(`retry attempt=${attempt} name=${link.name} url=${link.url} error=${message}`)
    } finally {
      clearTimeout(timeout)
    }
    if (attempt < 3) await delay(attempt * 1_000)
  }
  if (failure) failures.push(failure)
}

if (failures.length > 0) {
  console.error(`external_link_check_failed=${failures.length}`)
  for (const failure of failures) console.error(failure)
  process.exit(1)
}

console.log(`external_link_check_ok=${links.length}`)
