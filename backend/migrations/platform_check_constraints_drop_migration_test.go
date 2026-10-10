package migrations

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropPlatformCheckConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("242_drop_platform_check_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;")
	require.Contains(t, sql,
		"ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;")
	require.NotContains(t, sql, "ADD CONSTRAINT")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitors_provider_check")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check")
}

// 平台集合由应用清单管理，按迁移顺序核对两个约束最终均处于移除状态。
// 已发布的本地 243 会恢复旧约束，必须由后续迁移消除。
func TestLaterMigrationsDoNotRestorePlatformCheckConstraints(t *testing.T) {
	const droppedAt = "242_drop_platform_check_constraints.sql"
	entries, err := fs.ReadDir(FS, ".")
	require.NoError(t, err)
	comments := regexp.MustCompile(`(?ms)/\*.*?\*/|--[^\n]*`)
	constraintOperation := regexp.MustCompile(`(?i)\b(ADD|DROP)\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?"?(user_platform_quotas_platform_check|composite_model_routes_target_platform_check)\b`)
	lastOperation := map[string]string{
		"user_platform_quotas_platform_check":          "DROP",
		"composite_model_routes_target_platform_check": "DROP",
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() <= droppedAt {
			continue
		}
		content, err := FS.ReadFile(entry.Name())
		require.NoError(t, err)
		for _, operation := range constraintOperation.FindAllSubmatch(comments.ReplaceAll(content, nil), -1) {
			lastOperation[strings.ToLower(string(operation[2]))] = strings.ToUpper(string(operation[1]))
		}
	}
	for name, operation := range lastOperation {
		require.Equal(t, "DROP", operation, "%s 最终必须由应用清单校验", name)
	}
}
