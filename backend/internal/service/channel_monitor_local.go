package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"time"
)

const (
	channelMonitorLocalEndpoint   = "http://sub2api.internal"
	channelMonitorUserConcurrency = 1000
)

type internalChannelMonitorContextKey struct{}

// WithInternalChannelMonitorRequest 只通过进程内 request.Context 注入身份，网络客户端无法伪造。
func WithInternalChannelMonitorRequest(ctx context.Context, apiKey *APIKey) context.Context {
	return context.WithValue(ctx, internalChannelMonitorContextKey{}, apiKey)
}

func InternalChannelMonitorAPIKey(ctx context.Context) (*APIKey, bool) {
	if ctx == nil {
		return nil, false
	}
	apiKey, ok := ctx.Value(internalChannelMonitorContextKey{}).(*APIKey)
	return apiKey, ok && apiKey != nil && apiKey.IsInternalChannelMonitor()
}

func (s *ChannelMonitorService) SetLocalRequestHandler(handler http.Handler) {
	if s == nil {
		return
	}
	s.localHandlerMu.Lock()
	s.localHandler = handler
	s.localHandlerMu.Unlock()
}

func (s *ChannelMonitorService) localRequestHandler() http.Handler {
	if s == nil {
		return nil
	}
	s.localHandlerMu.RLock()
	defer s.localHandlerMu.RUnlock()
	return s.localHandler
}

func (s *ChannelMonitorService) runLocalCheckForModel(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	opts *CheckOptions,
) *CheckResult {
	handler := s.localRequestHandler()
	if handler == nil {
		return &CheckResult{
			Model:     model,
			Status:    MonitorStatusError,
			Message:   "local gateway is unavailable",
			CheckedAt: time.Now(),
		}
	}
	apiKey := buildInternalChannelMonitorAPIKey(monitor)
	if apiKey == nil {
		return &CheckResult{
			Model:     model,
			Status:    MonitorStatusError,
			Message:   "local monitor group is unavailable",
			CheckedAt: time.Now(),
		}
	}
	requestCtx := WithInternalChannelMonitorRequest(ctx, apiKey)
	client := &localChannelMonitorHTTPDoer{handler: handler}
	return runCheckForModelWithClient(
		requestCtx,
		monitor.Provider,
		channelMonitorLocalBaseEndpoint(monitor.Provider),
		"",
		model,
		opts,
		client,
	)
}

func channelMonitorLocalBaseEndpoint(provider string) string {
	if provider == MonitorProviderOpenCodeGo {
		return channelMonitorLocalEndpoint + "/v1"
	}
	return channelMonitorLocalEndpoint
}

func buildInternalChannelMonitorAPIKey(monitor *ChannelMonitor) *APIKey {
	if monitor == nil || monitor.GroupID == nil || *monitor.GroupID <= 0 || monitor.Group == nil {
		return nil
	}
	internalID := -monitor.ID
	if internalID >= 0 {
		internalID = -1
	}
	groupID := *monitor.GroupID
	group := *monitor.Group
	group.FallbackGroupID = nil
	group.FallbackGroupIDOnInvalidRequest = nil
	user := &User{
		ID:            internalID,
		Role:          RoleAdmin,
		Status:        StatusActive,
		Balance:       math.MaxFloat64,
		Concurrency:   channelMonitorUserConcurrency,
		AllowedGroups: []int64{groupID},
	}
	return &APIKey{
		ID:                     internalID,
		UserID:                 internalID,
		Name:                   "channel-monitor",
		GroupID:                &groupID,
		RoutingPlatform:        group.Platform,
		RoutingStrategy:        APIKeyRoutingStrategyManual,
		Status:                 StatusAPIKeyActive,
		User:                   user,
		Group:                  &group,
		InternalChannelMonitor: true,
	}
}

type localChannelMonitorHTTPDoer struct {
	handler http.Handler
}

func (d *localChannelMonitorHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if d == nil || d.handler == nil || req == nil {
		return nil, fmt.Errorf("local gateway request handler is unavailable")
	}
	recorder := httptest.NewRecorder()
	req.RequestURI = req.URL.RequestURI()
	req.Host = "sub2api.internal"
	req.RemoteAddr = "127.0.0.1:0"
	d.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}
