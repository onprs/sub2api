package protocolconv

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

type metadataBridgeMemoryStore struct {
	values map[MetadataKey]map[string]json.RawMessage
}

func (s *metadataBridgeMemoryStore) Put(key MetadataKey, data map[string]json.RawMessage) error {
	if s.values == nil {
		s.values = make(map[MetadataKey]map[string]json.RawMessage)
	}
	s.values[key] = mergeMissingMetadata(nil, data)
	return nil
}

func (s *metadataBridgeMemoryStore) Get(key MetadataKey) (map[string]json.RawMessage, bool) {
	data, ok := s.values[key]
	return mergeMissingMetadata(nil, data), ok
}

func TestProviderMetadataBridgeDoesNotOverwriteRequestMetadata(t *testing.T) {
	scope := MetadataScope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: ProtocolGoogleGenAI}
	key := MetadataKey{Scope: scope, ToolCallID: "call-1"}
	store := &metadataBridgeMemoryStore{values: map[MetadataKey]map[string]json.RawMessage{
		key: {
			providerSignatureMetadataKey: json.RawMessage(`"stored-signature"`),
			"stored":                     json.RawMessage(`"stored-value"`),
			"shared":                     json.RawMessage(`"stored-shared"`),
		},
	}}
	bridge := &providerMetadataBridge{store: store, scope: scope}
	request := &ir.Request{Messages: []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentPart{{
		Type: ir.ContentToolCall, ToolCallID: "call-1", ToolName: "lookup", Signature: "client-signature",
		ProviderMetadata: map[string]json.RawMessage{
			"client": json.RawMessage(`"client-value"`),
			"shared": json.RawMessage(`"client-shared"`),
		},
	}}}}}

	bridge.injectRequest(request)
	part := request.Messages[0].Content[0]
	require.Equal(t, "client-signature", part.Signature)
	require.JSONEq(t, `"client-value"`, string(part.ProviderMetadata["client"]))
	require.JSONEq(t, `"client-shared"`, string(part.ProviderMetadata["shared"]))
	require.JSONEq(t, `"stored-value"`, string(part.ProviderMetadata["stored"]))
	require.NotContains(t, part.ProviderMetadata, providerSignatureMetadataKey)
}

func TestNewProviderMetadataBridgeRejectsRouteScopeMismatch(t *testing.T) {
	store := &metadataBridgeMemoryStore{}
	route := Route{Source: ProtocolAnthropic, IntendedTarget: ProtocolGoogleGenAI, AccountID: 4}
	base := MetadataScope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: ProtocolGoogleGenAI}

	_, err := newProviderMetadataBridge(store, MetadataScope{}, route)
	require.ErrorContains(t, err, "tenant ID")

	wrongAccount := base
	wrongAccount.AccountID = 5
	_, err = newProviderMetadataBridge(store, wrongAccount, route)
	require.ErrorContains(t, err, "does not match route account")

	wrongProtocol := base
	wrongProtocol.Protocol = ProtocolAnthropic
	_, err = newProviderMetadataBridge(store, wrongProtocol, route)
	require.ErrorContains(t, err, "does not match intended target")
}
