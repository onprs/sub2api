package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenCodeGoConsoleClientFetchSummarySendsCookieAndParsesPage(t *testing.T) {
	t.Parallel()

	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(opencodeGoConsoleHTMLFixture))
	}))
	defer srv.Close()

	client := NewOpenCodeGoConsoleClient(srv.URL, srv.Client())
	summary, err := client.FetchSummary(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret")

	if err != nil {
		t.Fatalf("FetchSummary() error = %v", err)
	}
	if gotCookie != "auth=secret" {
		t.Fatalf("Cookie header = %q", gotCookie)
	}
	if summary.WorkspaceID != "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ" {
		t.Fatalf("workspace = %q", summary.WorkspaceID)
	}
	if summary.Usage.FiveHour.UsagePercent != 19 {
		t.Fatalf("5h percent = %v", summary.Usage.FiveHour.UsagePercent)
	}
}

func TestOpenCodeGoConsoleClientFetchSummaryMarksLoginRedirectAsExpired(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/auth/authorize", http.StatusFound)
	}))
	defer srv.Close()

	noRedirect := srv.Client()
	noRedirect.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client := NewOpenCodeGoConsoleClient(srv.URL, noRedirect)

	_, err := client.FetchSummary(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=expired")

	if !errors.Is(err, ErrOpenCodeGoConsoleAuthExpired) {
		t.Fatalf("FetchSummary() error = %v, want ErrOpenCodeGoConsoleAuthExpired", err)
	}
}

func TestOpenCodeGoConsoleClientFetchSummaryParsesSSRBodyFromAuthRedirect(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/auth/authorize")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte(opencodeGoConsoleHTMLFixture))
	}))
	defer srv.Close()

	noRedirect := srv.Client()
	noRedirect.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client := NewOpenCodeGoConsoleClient(srv.URL, noRedirect)

	summary, err := client.FetchSummary(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret")

	if err != nil {
		t.Fatalf("FetchSummary() error = %v", err)
	}
	if summary.Usage.FiveHour.UsagePercent != 19 {
		t.Fatalf("5h percent = %v", summary.Usage.FiveHour.UsagePercent)
	}
	if summary.Referral.ReferralCode != "M5N4TCC0GA" {
		t.Fatalf("referral code = %q", summary.Referral.ReferralCode)
	}
}
