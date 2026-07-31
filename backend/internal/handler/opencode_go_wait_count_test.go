package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenCodeGoAcquireUserSlotCountsEachWaiterOnce(t *testing.T) {
	cache := &helperConcurrencyCacheStub{
		userSeq:     []bool{false, true},
		waitAllowed: true,
	}
	concurrency := service.NewConcurrencyService(cache)
	handler := &OpenCodeGoGatewayHandler{
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Millisecond),
	}
	c, _ := newHelperTestContext(http.MethodPost, "/v1/chat/completions")
	streamStarted := false

	release, acquired := handler.acquireUserSlot(c, 42, 1, false, &streamStarted, zap.NewNop(), openCodeGoHandlerErrorChat)

	require.True(t, acquired)
	require.NotNil(t, release)
	require.Equal(t, 1, cache.waitIncrementCalls)
	require.Equal(t, 1, cache.waitDecrementCalls)
	release()
}
