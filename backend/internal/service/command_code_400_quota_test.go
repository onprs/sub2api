//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const commandCodeCredits400Body = `{"error":{"message":"You have insufficient credits to make this request. Please purchase more credits to continue using the service.","type":"invalid_request_error","code":"BAD_REQUEST"}}`

func TestCommandCodeInsufficientCredits400Classification(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"实测余额耗尽", commandCodeCredits400Body, true},
		{"明确错误类型", `{"error":{"type":"insufficient_credits","message":"No credits left"}}`, true},
		{"明确错误码", `{"error":{"code":"insufficient_credits"}}`, true},
		{"普通参数错误", `{"error":{"type":"invalid_request_error","message":"Invalid option","code":"BAD_REQUEST"}}`, false},
		{"回显错误正文", `{"error":{"message":"Invalid input"},"input":"You have insufficient credits to make this request. Please purchase more credits"}`, false},
		{"参数错误回显内容", `{"error":{"message":"Invalid input: You have insufficient credits to make this request. Please purchase more credits"}}`, false},
		{"缺少购买提示", `{"error":{"message":"You have insufficient credits to make this request."}}`, false},
		{"纯文本", `You have insufficient credits to make this request. Please purchase more credits`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, commandCodeInsufficientCredits400(http.StatusBadRequest, []byte(tc.body)))
			require.False(t, commandCodeInsufficientCredits400(http.StatusForbidden, []byte(tc.body)))
		})
	}
}

func TestCommandCode400CreditsStopsSchedulingAndRecovers(t *testing.T) {
	account := commandCodeUsageAccount()
	account.Schedulable = true
	repo := &commandCodeStatefulRepo{account: account}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	before := time.Now()
	disable := service.HandleUpstreamError(context.Background(), account, http.StatusBadRequest, nil, []byte(commandCodeCredits400Body))
	require.True(t, disable)
	require.False(t, account.IsSchedulable())
	require.NotNil(t, account.TempUnschedulableUntil)
	require.WithinDuration(t, before.Add(20*time.Minute), *account.TempUnschedulableUntil, time.Second)
	require.True(t, strings.HasPrefix(account.TempUnschedulableReason, cnBalanceLowReasonPrefix))

	balance := &CNProviderBalanceCheckService{accountRepo: repo}
	require.Equal(t, cnBalanceNoChange, balance.applyBalanceResult(context.Background(), account, &CNProviderBalanceResult{Available: false}, 0.01))
	require.False(t, account.IsSchedulable())
	require.Equal(t, cnBalanceCleared, balance.applyBalanceResult(context.Background(), account, &CNProviderBalanceResult{Available: true, Balance: 5}, 0.01))
	require.True(t, account.IsSchedulable())
}

func TestCommandCode400DoesNotPauseOrdinaryErrorsOrOtherPlatforms(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		body     string
	}{
		{"参数错误", PlatformCommandCode, `{"error":{"type":"invalid_request_error","message":"Invalid option","code":"BAD_REQUEST"}}`},
		{"其他平台", PlatformOpenAI, commandCodeCredits400Body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := commandCodeUsageAccount()
			account.Platform = tc.platform
			require.False(t, service.HandleUpstreamError(context.Background(), account, http.StatusBadRequest, nil, []byte(tc.body)))
			require.Zero(t, repo.tempCalls)
			require.Zero(t, repo.setErrorCalls)
		})
	}
}

func TestCommandCode400RespectsAccountErrorPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials map[string]any
	}{
		{"账号池", map[string]any{"pool_mode": true}},
		{"自定义错误码排除 400", map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(http.StatusForbidden)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := commandCodeUsageAccount()
			for key, value := range tc.credentials {
				account.Credentials[key] = value
			}
			repo := &rateLimitAccountRepoStub{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			require.False(t, svc.HandleUpstreamError(context.Background(), account, http.StatusBadRequest, nil, []byte(commandCodeCredits400Body)))
			require.Zero(t, repo.tempCalls)
			require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(account, http.StatusBadRequest, "", []byte(commandCodeCredits400Body)))
		})
	}
}

func TestCommandCode400CreditsFailsOverStandardAnthropic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := commandCodeUsageAccount()
	repo := &rateLimitAccountRepoStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(commandCodeCredits400Body))}}
	svc := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	_, err := svc.forwardStandardProtocolToAnthropic(context.Background(), c, account, body, nil, "claude-sonnet-4-6", nil)
	var failover *UpstreamFailoverError
	require.True(t, errors.As(err, &failover), "应返回换号错误：%v", err)
	require.Equal(t, http.StatusBadRequest, failover.StatusCode)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, cnBalanceLowReasonPrefix)
	require.False(t, c.Writer.Written())
}

func TestCommandCode400CreditsFailsOverAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name    string
		path    string
		body    string
		forward func(*OpenAIGatewayService, *gin.Context, *Account, []byte) error
	}{
		{"chat", "/v1/chat/completions", `{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`, func(s *OpenAIGatewayService, c *gin.Context, a *Account, body []byte) error {
			_, err := s.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
			return err
		}},
		{"responses", "/v1/responses", `{"model":"gpt-5.5","input":"hi"}`, func(s *OpenAIGatewayService, c *gin.Context, a *Account, body []byte) error {
			_, err := s.Forward(context.Background(), c, a, body)
			return err
		}},
		{"messages_native", "/v1/messages", `{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`, func(s *OpenAIGatewayService, c *gin.Context, a *Account, body []byte) error {
			_, err := s.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
			return err
		}},
		{"messages_to_chat", "/v1/messages", `{"model":"deepseek/deepseek-v4-flash","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`, func(s *OpenAIGatewayService, c *gin.Context, a *Account, body []byte) error {
			_, err := s.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := commandCodeUsageAccount()
			account.Schedulable = true
			repo := &rateLimitAccountRepoStub{}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(commandCodeCredits400Body))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			body := []byte(tc.body)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))

			err := tc.forward(svc, c, account, body)
			var failover *UpstreamFailoverError
			require.True(t, errors.As(err, &failover), "应返回换号错误：%v", err)
			require.Equal(t, http.StatusBadRequest, failover.StatusCode)
			require.False(t, failover.RetryableOnSameAccount)
			require.Equal(t, 1, repo.tempCalls)
			require.Contains(t, repo.lastTempReason, cnBalanceLowReasonPrefix)
			require.Zero(t, repo.setErrorCalls)
			require.False(t, c.Writer.Written())
		})
	}
}
