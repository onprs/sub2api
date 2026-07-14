package toolrouting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlattenNamespaceName(t *testing.T) {
	require.Equal(t, "gmail__send", FlattenNamespaceName("gmail", "send"))
	long := FlattenNamespaceName("very_long_namespace_prefix_for_testing_purposes", "and_a_rather_long_tool_name_too")
	require.LessOrEqual(t, len(long), ChatToolNameMaxLen)
	require.True(t, strings.Contains(long, "__"))
	require.Equal(t, long, FlattenNamespaceName("very_long_namespace_prefix_for_testing_purposes", "and_a_rather_long_tool_name_too"))
}
