package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestShouldUseAntigravityStandardBridge(t *testing.T) {
	tests := []struct {
		name    string
		account *service.Account
		want    bool
	}{
		{name: "nil", account: nil, want: false},
		{name: "anthropic oauth", account: &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}, want: false},
		{name: "antigravity api key relay", account: &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeAPIKey}, want: false},
		{name: "antigravity oauth", account: &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth}, want: true},
		{name: "antigravity setup token", account: &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeSetupToken}, want: true},
		{name: "antigravity upstream relay", account: &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeUpstream}, want: false},
		{name: "antigravity legacy empty type", account: &service.Account{Platform: service.PlatformAntigravity}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, shouldUseAntigravityStandardBridge(test.account))
		})
	}
}
