package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAnthropicSSEField(t *testing.T) {
	for _, tc := range []struct {
		name, line, field, want string
		ok                      bool
	}{
		{"space", "event: message_start", "event", "message_start", true},
		{"no space", "event:message_start", "event", "message_start", true},
		{"multiple spaces", "event:  message_delta", "event", "message_delta", true},
		{"data", "data:{\"type\":\"message_start\"}", "data", "{\"type\":\"message_start\"}", true},
		{"wrong field", "event: message_start", "data", "", false},
		{"empty", "", "event", "", false},
		{"no colon", "invalid line", "event", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, ok := parseAnthropicSSEField(tc.line, tc.field)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, value)
		})
	}
}
