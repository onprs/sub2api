package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseClinePassCatalogFiltersAndSorts(t *testing.T) {
	models, err := parseClinePassCatalog([]byte(`{
		"free":["cline-pass/ignored"],
		"recommended":["cline-pass/also-ignored"],
		"clinePass":[" cline-pass/qwen3.7-plus ","bad","cline-pass/glm-5.2","cline-pass/GLM-5.2","cline-pass/qwen3.7-plus"]
	}`))
	require.NoError(t, err)
	require.Equal(t, []string{"cline-pass/glm-5.2", "cline-pass/qwen3.7-plus"}, models)
}

func TestParseClinePassCatalogRejectsEmptyOrMalformed(t *testing.T) {
	_, err := parseClinePassCatalog([]byte(`{"clinePass":[]}`))
	require.Error(t, err)
	_, err = parseClinePassCatalog([]byte(`{"clinePass":`))
	require.Error(t, err)
}

func TestClinePassCatalogFallbackAndLastKnownGood(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"clinePass":["cline-pass/test-model"]}`))
	}))
	defer server.Close()

	catalog := NewClinePassCatalog(server.Client())
	catalog.endpoint = server.URL
	models, err := catalog.ForceRefresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"cline-pass/test-model"}, models)

	fail.Store(true)
	models, err = catalog.ForceRefresh(context.Background())
	require.Error(t, err)
	require.Equal(t, []string{"cline-pass/test-model"}, models)
}

func TestClinePassCatalogConcurrentRefreshIsDeduplicated(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"clinePass":["cline-pass/test-model"]}`))
	}))
	defer server.Close()

	catalog := NewClinePassCatalog(server.Client())
	catalog.endpoint = server.URL
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = catalog.ForceRefresh(context.Background())
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
}
