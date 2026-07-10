package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEncodeOpenCodeGoSolidStringArgsMatchesFixture(t *testing.T) {
	got, err := EncodeOpenCodeGoSolidStringArgs("wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T")
	if err != nil {
		t.Fatalf("EncodeOpenCodeGoSolidStringArgs() error = %v", err)
	}
	want := `{"t":{"t":9,"i":0,"l":2,"a":[{"t":1,"s":"wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ"},{"t":1,"s":"ref_01KVD1PJPG6B1GWZYZNAQZBQ2T"}],"o":0},"f":31,"m":[]}`
	if string(got) != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestOpenCodeGoReferralActionClientPreviewResolvesHashAndPostsServerAction(t *testing.T) {
	t.Parallel()

	expectedBody, err := EncodeOpenCodeGoSolidStringArgs("wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T")
	if err != nil {
		t.Fatalf("EncodeOpenCodeGoSolidStringArgs() error = %v", err)
	}
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload"></head></html>`))
		case "/_build/assets/index-DtPYjwk4.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		case "/_server":
			posted = true
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if got := r.Header.Get("X-Server-Id"); got != "46625df0aecf05f270f7ae4612cde374d11350c8abaf8649027572228b8af150" {
				t.Fatalf("X-Server-Id = %q", got)
			}
			if got := r.Header.Get("Cookie"); got != "auth=secret" {
				t.Fatalf("Cookie = %q", got)
			}
			if got := r.Header.Get("Origin"); got != srvURLOrigin(r) {
				t.Fatalf("Origin = %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != string(expectedBody) {
				t.Fatalf("body = %s, want %s", body, expectedBody)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"rollingUsage":{"beforePercent":19,"afterPercent":0,"resetInSec":5590},"weeklyUsage":{"beforePercent":7,"afterPercent":0,"resetInSec":588490},"monthlyUsage":{"beforePercent":10,"afterPercent":2,"resetInSec":2265176}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	preview, err := client.Preview(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T", "auth=secret")

	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if !posted {
		t.Fatal("expected _server POST")
	}
	if preview.RollingUsage.BeforePercent != 19 || preview.RollingUsage.AfterPercent != 0 {
		t.Fatalf("unexpected rolling preview: %#v", preview.RollingUsage)
	}
	if preview.MonthlyUsage.AfterPercent != 2 {
		t.Fatalf("monthly after = %v", preview.MonthlyUsage.AfterPercent)
	}
}

func TestOpenCodeGoReferralActionClientApplyMapsAlreadyApplied(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			_, _ = w.Write([]byte(`<link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload">`))
		case "/_build/assets/index-DtPYjwk4.js":
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		case "/_server":
			if got := r.Header.Get("X-Server-Id"); got != "f386778c1b78eade3e6acff87c9284e02fcd86826463c080526143c4fe8fff23" {
				t.Fatalf("X-Server-Id = %q", got)
			}
			w.Header().Set("X-Error", "1")
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("already applied"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	err := client.Apply(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T", "auth=secret")

	if !errors.Is(err, ErrOpenCodeGoReferralRewardAlreadyApplied) {
		t.Fatalf("Apply() error = %v, want already applied", err)
	}
}

func TestOpenCodeGoReferralActionClientCachesResolvedHashesAcrossClients(t *testing.T) {
	var pageFetches int32
	var assetFetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			atomic.AddInt32(&pageFetches, 1)
			_, _ = w.Write([]byte(`<link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload">`))
		case "/_build/assets/index-DtPYjwk4.js":
			atomic.AddInt32(&assetFetches, 1)
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	first := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	if _, err := first.ResolveActions(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret"); err != nil {
		t.Fatalf("first ResolveActions() error = %v", err)
	}
	second := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	if _, err := second.ResolveActions(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret"); err != nil {
		t.Fatalf("second ResolveActions() error = %v", err)
	}

	if got := atomic.LoadInt32(&pageFetches); got != 1 {
		t.Fatalf("page fetches = %d, want shared cache hit after first client", got)
	}
	if got := atomic.LoadInt32(&assetFetches); got != 1 {
		t.Fatalf("asset fetches = %d, want shared cache hit after first client", got)
	}
}

func TestOpenCodeGoReferralActionClientStopsFetchingAssetsAfterActionsResolved(t *testing.T) {
	var unusedAssetFetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			_, _ = w.Write([]byte(`<link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload"><link href="/_build/assets/unused.js" rel="modulepreload">`))
		case "/_build/assets/index-DtPYjwk4.js":
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		case "/_build/assets/unused.js":
			atomic.AddInt32(&unusedAssetFetches, 1)
			_, _ = w.Write([]byte(`console.log("unused")`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	actions, err := client.ResolveActions(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret")

	if err != nil {
		t.Fatalf("ResolveActions() error = %v", err)
	}
	if actions.ReferralUsagePreview == "" || actions.ReferralRewardApply == "" {
		t.Fatalf("expected referral action hashes, got %#v", actions)
	}
	if got := atomic.LoadInt32(&unusedAssetFetches); got != 0 {
		t.Fatalf("unused asset fetches = %d, want 0 after action hashes are resolved", got)
	}
}

func TestOpenCodeGoReferralActionClientPrioritizesIndexAssetsForServerActions(t *testing.T) {
	var unusedAssetFetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			_, _ = w.Write([]byte(`<link href="/_build/assets/i18n.js" rel="modulepreload"><link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload">`))
		case "/_build/assets/i18n.js":
			atomic.AddInt32(&unusedAssetFetches, 1)
			_, _ = w.Write([]byte(`console.log("i18n")`))
		case "/_build/assets/index-DtPYjwk4.js":
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	actions, err := client.ResolveActions(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "auth=secret")

	if err != nil {
		t.Fatalf("ResolveActions() error = %v", err)
	}
	if actions.ReferralUsagePreview == "" || actions.ReferralRewardApply == "" {
		t.Fatalf("expected referral action hashes, got %#v", actions)
	}
	if got := atomic.LoadInt32(&unusedAssetFetches); got != 0 {
		t.Fatalf("unused asset fetches = %d, want index action asset to be tried first", got)
	}
}

func TestParseOpenCodeGoReferralUsagePreviewResponseFromSerovalObjectStream(t *testing.T) {
	raw := []byte(`;0x00000139;((self.$R=self.$R||{})["server-fn:test"]=[],($R=>$R[0]={rollingUsage:$R[1]={beforePercent:19,afterPercent:0,resetInSec:3153},weeklyUsage:$R[2]={beforePercent:31,afterPercent:14,resetInSec:549964},monthlyUsage:$R[3]={beforePercent:35,afterPercent:26,resetInSec:1886267}})($R["server-fn:test"]))`)

	preview, err := parseOpenCodeGoReferralUsagePreviewResponse(raw)

	if err != nil {
		t.Fatalf("parseOpenCodeGoReferralUsagePreviewResponse() error = %v", err)
	}
	if preview.RollingUsage.BeforePercent != 19 || preview.RollingUsage.AfterPercent != 0 || preview.RollingUsage.ResetInSec != 3153 {
		t.Fatalf("unexpected rolling preview: %#v", preview.RollingUsage)
	}
	if preview.WeeklyUsage.BeforePercent != 31 || preview.WeeklyUsage.AfterPercent != 14 || preview.WeeklyUsage.ResetInSec != 549964 {
		t.Fatalf("unexpected weekly preview: %#v", preview.WeeklyUsage)
	}
	if preview.MonthlyUsage.BeforePercent != 35 || preview.MonthlyUsage.AfterPercent != 26 || preview.MonthlyUsage.ResetInSec != 1886267 {
		t.Fatalf("unexpected monthly preview: %#v", preview.MonthlyUsage)
	}
}

func TestOpenCodeGoReferralActionClientRefreshesHashAfterServer404(t *testing.T) {
	var pageFetches int32
	var assetFetches int32
	var serverPosts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go":
			atomic.AddInt32(&pageFetches, 1)
			_, _ = w.Write([]byte(`<link href="/_build/assets/index-DtPYjwk4.js" rel="modulepreload">`))
		case "/_build/assets/index-DtPYjwk4.js":
			fetch := atomic.AddInt32(&assetFetches, 1)
			if fetch == 1 {
				_, _ = w.Write([]byte(strings.ReplaceAll(opencodeGoConsoleJSFixture, "46625df0aecf05f270f7ae4612cde374d11350c8abaf8649027572228b8af150", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")))
				return
			}
			_, _ = w.Write([]byte(opencodeGoConsoleJSFixture))
		case "/_server":
			post := atomic.AddInt32(&serverPosts, 1)
			if post == 1 {
				if got := r.Header.Get("X-Server-Id"); got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
					t.Fatalf("first X-Server-Id = %q", got)
				}
				http.Error(w, "server function not found", http.StatusNotFound)
				return
			}
			if got := r.Header.Get("X-Server-Id"); got != "46625df0aecf05f270f7ae4612cde374d11350c8abaf8649027572228b8af150" {
				t.Fatalf("second X-Server-Id = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"rollingUsage":{"beforePercent":19,"afterPercent":0,"resetInSec":5590},"weeklyUsage":{"beforePercent":7,"afterPercent":0,"resetInSec":588490},"monthlyUsage":{"beforePercent":10,"afterPercent":2,"resetInSec":2265176}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewOpenCodeGoReferralActionClient(srv.URL, srv.Client())
	preview, err := client.Preview(context.Background(), "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T", "auth=secret")

	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.MonthlyUsage.AfterPercent != 2 {
		t.Fatalf("monthly after = %v", preview.MonthlyUsage.AfterPercent)
	}
	if got := atomic.LoadInt32(&pageFetches); got != 2 {
		t.Fatalf("page fetches = %d, want re-resolve after 404", got)
	}
	if got := atomic.LoadInt32(&assetFetches); got != 2 {
		t.Fatalf("asset fetches = %d, want re-resolve after 404", got)
	}
	if got := atomic.LoadInt32(&serverPosts); got != 2 {
		t.Fatalf("server posts = %d, want retry after 404", got)
	}
}

func srvURLOrigin(r *http.Request) string {
	host := r.Host
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "http://" + host
}
