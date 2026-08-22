package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenCodeGoForwardMayFailover(t *testing.T) {
	t.Run("unwritten response", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
		before := c.Writer.Size()
		require.True(t, openCodeGoForwardMayFailover(c, before, &service.UpstreamFailoverError{}))
	})

	t.Run("semantic output blocks replay", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
		before := c.Writer.Size()
		_, err := c.Writer.Write([]byte("semantic output"))
		require.NoError(t, err)
		require.False(t, openCodeGoForwardMayFailover(c, before, &service.UpstreamFailoverError{}))
	})

	t.Run("attempt local writes allow safe replay", func(t *testing.T) {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
		before := c.Writer.Size()
		_, err := c.Writer.Write([]byte(": ping\n\n"))
		require.NoError(t, err)
		require.True(t, openCodeGoForwardMayFailover(c, before, &service.UpstreamFailoverError{SafeToFailoverAfterWrite: true}))
	})
}

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
