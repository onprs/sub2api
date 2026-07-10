package admin

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestBuildOpenCodeGoConsoleSummaryResponseTreatsMissingMetadataAsEmpty(t *testing.T) {
	account := &service.Account{
		ID:          42,
		Platform:    service.PlatformOpenCodeGo,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-opencode-go"},
		Extra:       map[string]any{},
	}

	resp := buildOpenCodeGoConsoleSummaryResponse(account, nil, "opencode go console auth expired")

	if resp.Authorized {
		t.Fatal("Authorized = true, want false when console cookie/workspace are missing")
	}
	if resp.WorkspaceID != "" {
		t.Fatalf("WorkspaceID = %q, want empty", resp.WorkspaceID)
	}
	if resp.AuthStatus != "" {
		t.Fatalf("AuthStatus = %q, want empty", resp.AuthStatus)
	}
	if resp.AuthCheckedAt != "" {
		t.Fatalf("AuthCheckedAt = %q, want empty", resp.AuthCheckedAt)
	}
}

func TestBuildOpenCodeGoConsoleSummaryResponseUsesExtraWorkspaceFallback(t *testing.T) {
	account := &service.Account{
		ID:       42,
		Platform: service.PlatformOpenCodeGo,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":        "sk-opencode-go",
			"console_cookie": "auth=secret",
		},
		Extra: map[string]any{
			"console_workspace_id":                 "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ",
			"opencode_go_console_auth_status":      service.OpenCodeGoConsoleAuthStatusReady,
			"opencode_go_console_auth_checked_at":  "2026-06-22T10:00:00Z",
			"opencode_go_usage_source":             "official_console",
			"opencode_go_referral_available_count": 1,
		},
	}

	resp := buildOpenCodeGoConsoleSummaryResponse(account, nil, "")

	if !resp.Authorized {
		t.Fatal("Authorized = false, want true when cookie exists and workspace is available from extra fallback")
	}
	if resp.WorkspaceID != "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ" {
		t.Fatalf("WorkspaceID = %q", resp.WorkspaceID)
	}
	if resp.AuthStatus != service.OpenCodeGoConsoleAuthStatusReady {
		t.Fatalf("AuthStatus = %q", resp.AuthStatus)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperKeepsFilteredBrowserCandidatesAsArray(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if !strings.Contains(script, "$Candidates = @(@(") {
		t.Fatalf("helper script must array-wrap filtered browser candidates:\n%s", script)
	}
	if strings.Contains(script, "$env:ProgramFiles(x86)") {
		t.Fatalf("helper script must use braced ProgramFiles(x86) env var syntax:\n%s", script)
	}
	if !strings.Contains(script, "${env:ProgramFiles(x86)}") {
		t.Fatalf("helper script must include x86 Program Files candidates:\n%s", script)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperIsCompatibleWithWindowsPowerShell51(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if strings.Contains(script, "Add-Type -AssemblyName System.Net.WebSockets") {
		t.Fatalf("helper script must not require System.Net.WebSockets assembly loading on Windows PowerShell 5.1:\n%s", script)
	}
	if !strings.Contains(script, "[System.Net.WebSockets.ClientWebSocket]") {
		t.Fatalf("helper script must still use ClientWebSocket for CDP:\n%s", script)
	}
	if !strings.Contains(script, "Waiting for opencode.ai login cookie") {
		t.Fatalf("helper script must wait for login instead of reading cookies immediately:\n%s", script)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperSubmitsValidatedFullCookieHeader(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if strings.Contains(script, `auth_cookie = "auth=$($Auth.value)"`) {
		t.Fatalf("helper script must not submit only the auth cookie:\n%s", script)
	}
	for _, want := range []string{
		"$CookieHeader",
		"Build-OpenCodeCookieHeader",
		`Network.getCookies" @{ urls = @($GoUrl) }`,
		"Read-OpenCodeGoBrowserState",
		"Runtime.evaluate",
		"document.documentElement.outerHTML",
		"lite.subscription.get",
		"auth_cookie = $CookieHeader",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("helper script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperPreservesAllApplicableCookies(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	cookieFnStart := strings.Index(script, "function Build-OpenCodeCookieHeader")
	if cookieFnStart < 0 {
		t.Fatalf("helper script missing Build-OpenCodeCookieHeader:\n%s", script)
	}
	cookieFnEnd := strings.Index(script[cookieFnStart+1:], "function ")
	if cookieFnEnd > 0 {
		cookieFnEnd += cookieFnStart + 1
	} else {
		cookieFnEnd = len(script)
	}
	cookieFn := script[cookieFnStart:cookieFnEnd]
	if strings.Contains(cookieFn, "Sort-Object -Property name -Unique") {
		t.Fatalf("cookie header builder must not drop same-name cookies by sorting unique names:\n%s", cookieFn)
	}
	if !strings.Contains(cookieFn, "ForEach-Object { \"$($_.name)=$($_.value)\" }") {
		t.Fatalf("cookie header builder must submit every applicable cookie pair:\n%s", cookieFn)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperPrintsNonSensitiveWaitDiagnostics(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	for _, want := range []string{
		"cookie_count=",
		"has_auth=",
		"browser_url=",
		"go_page_loaded=",
		"marker_lite_subscription=",
		"marker_referral=",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("helper script missing diagnostic %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "Write-Host $CookieHeader") {
		t.Fatalf("helper script must not print cookie header values:\n%s", script)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperKeepsDiagnosticsAccurateAndSanitized(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if !strings.Contains(script, "function Format-OpenCodeSafeUrl") {
		t.Fatalf("helper script must sanitize diagnostic browser URLs:\n%s", script)
	}
	if !strings.Contains(script, `$Uri.Scheme + "://" + $Uri.Host + $Uri.AbsolutePath`) {
		t.Fatalf("helper script must strip query/fragment from diagnostic browser URLs:\n%s", script)
	}
	resetIndex := strings.Index(script, "$Auth = $null\n  if (($i % 15) -eq 0) { Write-OpenCodeWaitDiagnostics")
	if resetIndex >= 0 {
		t.Fatalf("helper script must print diagnostics before clearing auth state:\n%s", script[resetIndex:])
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperAcceptsProviderRedirectedGoPage(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if strings.Contains(script, `$Href -like "$GoUrl*"`) {
		t.Fatalf("helper script must not wait forever when login redirects to another valid Go page:\n%s", script)
	}
	for _, want := range []string{
		`workspaceID: workspaceMatch ? workspaceMatch[1] : ""`,
		`href.match(/\/workspace\/(wrk_[A-Za-z0-9]+)(?:\/go)?(?:[/?#]|$)/)`,
		`$OnWorkspacePage = $Href -match "/workspace/wrk_[A-Za-z0-9]+(?:/go)?"`,
		"current_workspace=",
		"target_workspace=",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("helper script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperSubmitsObservedWorkspace(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	for _, want := range []string{
		"$ResolvedWorkspaceID = [string]$State.workspaceID",
		`if (-not $ResolvedWorkspaceID) { $ResolvedWorkspaceID = $WorkspaceID }`,
		"workspace_id = $ResolvedWorkspaceID",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("helper script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "workspace_id = $WorkspaceID") {
		t.Fatalf("helper script must not submit only the ticket workspace:\n%s", script)
	}
}

func TestBuildOpenCodeGoConsoleAuthHelperDoesNotNavigateDuringLogin(t *testing.T) {
	script := buildOpenCodeGoConsoleAuthHelperScript(
		"ticket-123",
		"wrk_01ABCDEF",
		"https://api.example.test/api/v1/opencode-go/console-auth/complete",
	)

	if strings.Contains(script, `Send-CDP "Page.navigate"`) {
		t.Fatalf("helper script must not navigate or refresh the browser while the user is logging in:\n%s", script)
	}
	if strings.Contains(script, "$LastNavigateAt") {
		t.Fatalf("helper script must not keep auto-navigation state while waiting for login:\n%s", script)
	}
}
