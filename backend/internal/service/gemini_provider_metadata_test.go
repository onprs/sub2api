package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocolmetadata "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/metadata"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func geminiProviderMetadataContext(tenantID, apiKeyID, groupID int64) context.Context {
	return WithProtocolMetadataIdentity(context.Background(), tenantID, apiKeyID, groupID)
}

func newGeminiProviderMetadataTestService() *GeminiMessagesCompatService {
	return &GeminiMessagesCompatService{
		providerMetadataStore: protocolmetadata.NewStore(time.Minute, 32),
	}
}

func cacheGeminiProviderSignature(
	t *testing.T,
	svc *GeminiMessagesCompatService,
	ctx context.Context,
	account *Account,
	signature string,
) {
	t.Helper()
	pipeline, _, err := svc.newClaudeMessagesGooglePipeline(
		ctx,
		account,
		[]byte(`{"model":"claude-client","max_tokens":32,"messages":[{"role":"user","content":"call lookup"}]}`),
		"claude-client",
		"gemini-upstream",
	)
	require.NoError(t, err)
	_, err = pipeline.ConvertResponse([]byte(`{
		"responseId":"google-response","modelVersion":"gemini-upstream",
		"candidates":[{"content":{"role":"model","parts":[{
			"functionCall":{"id":"call-1","name":"lookup","args":{"q":"x"}},
			"thoughtSignature":"`+signature+`"
		}]} ,"finishReason":"STOP"}]
	}`), protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)
}

func convertedGeminiProviderSignature(
	t *testing.T,
	svc *GeminiMessagesCompatService,
	ctx context.Context,
	account *Account,
) string {
	t.Helper()
	_, converted, err := svc.newClaudeMessagesGooglePipeline(
		ctx,
		account,
		[]byte(`{
			"model":"claude-client","max_tokens":32,
			"messages":[
				{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"lookup","input":{"q":"x"}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"result"}]}
			]
		}`),
		"claude-client",
		"gemini-upstream",
	)
	require.NoError(t, err)
	return gjson.GetBytes(converted, "contents.0.parts.0.thoughtSignature").String()
}

func TestGeminiProviderMetadataBridgeRestoresSignatureAcrossRequests(t *testing.T) {
	svc := newGeminiProviderMetadataTestService()
	ctx := geminiProviderMetadataContext(10, 20, 30)
	account := &Account{ID: 40, Platform: PlatformGemini}

	cacheGeminiProviderSignature(t, svc, ctx, account, "real-google-signature")

	require.Equal(t, "real-google-signature", convertedGeminiProviderSignature(t, svc, ctx, account))
}

func TestGeminiProviderMetadataBridgeIsolatesOwnershipScope(t *testing.T) {
	svc := newGeminiProviderMetadataTestService()
	baseCtx := geminiProviderMetadataContext(11, 21, 31)
	baseAccount := &Account{ID: 41, Platform: PlatformGemini}
	cacheGeminiProviderSignature(t, svc, baseCtx, baseAccount, "scoped-google-signature")

	tests := []struct {
		name    string
		ctx     context.Context
		account *Account
	}{
		{name: "tenant", ctx: geminiProviderMetadataContext(99, 21, 31), account: baseAccount},
		{name: "api key", ctx: geminiProviderMetadataContext(11, 99, 31), account: baseAccount},
		{name: "group", ctx: geminiProviderMetadataContext(11, 21, 99), account: baseAccount},
		{name: "account", ctx: baseCtx, account: &Account{ID: 99, Platform: PlatformGemini}},
		{name: "missing authenticated scope", ctx: context.Background(), account: baseAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, geminiDummyThoughtSignature, convertedGeminiProviderSignature(t, svc, tt.ctx, tt.account))
		})
	}
}

func TestConfigureGoogleMetadataBridgeEnablesForeignSourceProtocols(t *testing.T) {
	svc := newGeminiProviderMetadataTestService()
	account := &Account{ID: 42, Platform: PlatformGemini}
	for _, source := range []protocolconv.Protocol{
		protocolconv.ProtocolAnthropic,
		protocolconv.ProtocolOpenAIChat,
		protocolconv.ProtocolOpenAIResponses,
	} {
		t.Run(source.String(), func(t *testing.T) {
			config := protocolconv.PipelineConfig{Route: protocolconv.Route{
				Source: source, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
				AccountID: account.ID,
			}}

			svc.configureGoogleMetadataBridge(geminiProviderMetadataContext(12, 22, 32), account, &config)

			require.Same(t, svc.providerMetadataStore, config.MetadataStore)
			require.Equal(t, protocolconv.MetadataScope{
				TenantID: 12, APIKeyID: 22, GroupID: 32, AccountID: 42,
				Protocol: protocolconv.ProtocolGoogleGenAI,
			}, config.MetadataScope)
		})
	}
}

func TestConfigureGoogleMetadataBridgeSkipsNativeGoogleIdentity(t *testing.T) {
	svc := newGeminiProviderMetadataTestService()
	account := &Account{ID: 42, Platform: PlatformGemini}
	config := protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolGoogleGenAI, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		AccountID: account.ID,
	}}

	svc.configureGoogleMetadataBridge(geminiProviderMetadataContext(12, 22, 32), account, &config)

	require.Nil(t, config.MetadataStore)
	require.Equal(t, protocolconv.MetadataScope{}, config.MetadataScope)
}
