package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserPlatformQuotasCNProvidersMigration 校验 224 号迁移保留全部既有平台，
// 并把 kimi/zhipu/deepseek 加入 user_platform_quotas.platform 的 CHECK 约束。
// 若重建约束时遗漏既有平台，ADD CONSTRAINT 会被存量配额行阻断；若遗漏新增平台，
// 注册预填充 12 平台默认配额会整条 INSERT 中止，造成新用户零配额行。
func TestUserPlatformQuotasCNProvidersMigration(t *testing.T) {
	content, err := FS.ReadFile("224_user_platform_quotas_add_cn_providers.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
	require.Contains(t, sql,
		"CHECK (platform IN ( 'anthropic', 'openai', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek' ))")
}
