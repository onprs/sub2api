package migrations

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairMissingOpsAlertSilencesPrecedesUnifiedOpenCodeMigration(t *testing.T) {
	const repairMigration = "240a_repair_missing_ops_alert_silences.sql"
	const unifiedMigration = "241_unify_opencode_platform.sql"

	files, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(files)

	repairIndex := sort.SearchStrings(files, repairMigration)
	unifiedIndex := sort.SearchStrings(files, unifiedMigration)
	require.Less(t, repairIndex, len(files))
	require.Less(t, unifiedIndex, len(files))
	require.Equal(t, repairMigration, files[repairIndex])
	require.Equal(t, unifiedMigration, files[unifiedIndex])
	require.Less(t, repairIndex, unifiedIndex)
}

func TestRepairMissingOpsAlertSilencesRecreatesThe037SchemaIdempotently(t *testing.T) {
	content, err := FS.ReadFile("240a_repair_missing_ops_alert_silences.sql")
	require.NoError(t, err)
	normalizedSQL := strings.Join(strings.Fields(string(content)), " ")

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS ops_alert_silences",
		"id BIGSERIAL PRIMARY KEY",
		"rule_id BIGINT NOT NULL",
		"platform VARCHAR(64) NOT NULL",
		"group_id BIGINT",
		"region VARCHAR(64)",
		"until TIMESTAMPTZ NOT NULL",
		"reason TEXT",
		"created_by BIGINT",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"CREATE INDEX IF NOT EXISTS idx_ops_alert_silences_lookup",
		"ON ops_alert_silences (rule_id, platform, group_id, region, until)",
	} {
		require.Contains(t, normalizedSQL, fragment)
	}
}
