import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const currentDir = dirname(fileURLToPath(import.meta.url))
const groupsViewSource = readFileSync(resolve(currentDir, '../GroupsView.vue'), 'utf8')
const editAccountSource = readFileSync(
  resolve(currentDir, '../../../components/account/EditAccountModal.vue'),
  'utf8'
)
const bulkEditAccountSource = readFileSync(
  resolve(currentDir, '../../../components/account/BulkEditAccountModal.vue'),
  'utf8'
)

describe('GPT-5.6 缓存写入推断配置层级', () => {
  it('在分组创建和编辑表单中绑定配置', () => {
    expect(groupsViewSource.match(/<GroupGPT56CacheWriteInferenceFields/g)).toHaveLength(2)
    expect(groupsViewSource).toContain('v-model:enabled="createForm.infer_gpt56_cache_write"')
    expect(groupsViewSource).toContain('v-model:enabled="editForm.infer_gpt56_cache_write"')
    expect(groupsViewSource).toContain(
      'v-model:min-tokens="createForm.infer_gpt56_cache_write_min_tokens"'
    )
    expect(groupsViewSource).toContain(
      'v-model:min-tokens="editForm.infer_gpt56_cache_write_min_tokens"'
    )
  })

  it('不再从账号编辑入口写入配置', () => {
    expect(editAccountSource).not.toContain('infer_gpt56_cache_write')
    expect(bulkEditAccountSource).not.toContain('infer_gpt56_cache_write')
  })
})
