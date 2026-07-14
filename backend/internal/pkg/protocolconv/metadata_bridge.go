package protocolconv

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

const providerSignatureMetadataKey = "protocolconv.signature"

// MetadataScope isolates replay metadata across every scheduling and tenant
// boundary that can change the validity or ownership of a provider signature.
type MetadataScope struct {
	TenantID  int64
	APIKeyID  int64
	GroupID   int64
	AccountID int64
	Protocol  Protocol
}

// Validate rejects partial scopes. Callers must disable the bridge when the
// authenticated request does not provide every ownership dimension.
func (s MetadataScope) Validate() error {
	if s.TenantID <= 0 {
		return errors.New("metadata scope tenant ID is required")
	}
	if s.APIKeyID <= 0 {
		return errors.New("metadata scope API key ID is required")
	}
	if s.GroupID <= 0 {
		return errors.New("metadata scope group ID is required")
	}
	if s.AccountID <= 0 {
		return errors.New("metadata scope account ID is required")
	}
	return s.Protocol.Validate()
}

// MetadataKey identifies replay metadata for one provider tool call.
type MetadataKey struct {
	Scope      MetadataScope
	ToolCallID string
}

// MetadataStore is the narrow cross-request contract consumed by Pipeline.
// Implementations must return and retain detached copies.
type MetadataStore interface {
	Put(MetadataKey, map[string]json.RawMessage) error
	Get(MetadataKey) (map[string]json.RawMessage, bool)
}

type providerMetadataBridge struct {
	store MetadataStore
	scope MetadataScope
}

func newProviderMetadataBridge(store MetadataStore, scope MetadataScope, route Route) (*providerMetadataBridge, error) {
	if store == nil {
		return nil, nil
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if scope.AccountID != route.AccountID {
		return nil, fmt.Errorf("metadata scope account ID %d does not match route account ID %d", scope.AccountID, route.AccountID)
	}
	if scope.Protocol != route.IntendedTarget {
		return nil, fmt.Errorf("metadata scope protocol %s does not match intended target %s", scope.Protocol, route.IntendedTarget)
	}
	return &providerMetadataBridge{store: store, scope: scope}, nil
}

func (b *providerMetadataBridge) injectRequest(request *ir.Request) {
	if b == nil || request == nil {
		return
	}
	for messageIndex := range request.Messages {
		for partIndex := range request.Messages[messageIndex].Content {
			part := &request.Messages[messageIndex].Content[partIndex]
			if part.Type != ir.ContentToolCall || part.ToolCallID == "" {
				continue
			}
			metadata, ok := b.store.Get(MetadataKey{Scope: b.scope, ToolCallID: part.ToolCallID})
			if !ok {
				continue
			}
			if part.Signature == "" {
				_ = json.Unmarshal(metadata[providerSignatureMetadataKey], &part.Signature)
			}
			part.ProviderMetadata = mergeMissingMetadata(part.ProviderMetadata, metadata)
			delete(part.ProviderMetadata, providerSignatureMetadataKey)
			if len(part.ProviderMetadata) == 0 {
				part.ProviderMetadata = nil
			}
		}
	}
}

func (b *providerMetadataBridge) cacheResponse(response *ir.Response) error {
	if b == nil || response == nil {
		return nil
	}
	for _, choice := range response.Choices {
		for _, part := range choice.Message.Content {
			if part.Type != ir.ContentToolCall {
				continue
			}
			if err := b.cacheToolCall(part.ToolCallID, part.Signature, part.ProviderMetadata); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *providerMetadataBridge) cacheStreamEvent(event ir.StreamEvent) error {
	if b == nil || (event.Type != ir.EventToolCallStart && event.Type != ir.EventToolCallEnd) {
		return nil
	}
	return b.cacheToolCall(event.ToolCallID, event.Signature, event.ProviderMetadata)
}

func (b *providerMetadataBridge) cacheToolCall(toolCallID, signature string, providerMetadata map[string]json.RawMessage) error {
	if b == nil || toolCallID == "" {
		return nil
	}
	metadata := mergeMissingMetadata(nil, providerMetadata)
	if signature != "" {
		if metadata == nil {
			metadata = make(map[string]json.RawMessage, 1)
		}
		encoded, err := json.Marshal(signature)
		if err != nil {
			return err
		}
		metadata[providerSignatureMetadataKey] = encoded
	}
	if len(metadata) == 0 {
		return nil
	}
	return b.store.Put(MetadataKey{Scope: b.scope, ToolCallID: toolCallID}, metadata)
}

func mergeMissingMetadata(current, stored map[string]json.RawMessage) map[string]json.RawMessage {
	if len(current) == 0 && len(stored) == 0 {
		return nil
	}
	merged := make(map[string]json.RawMessage, len(current)+len(stored))
	for key, value := range current {
		merged[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range stored {
		if _, exists := merged[key]; !exists {
			merged[key] = append(json.RawMessage(nil), value...)
		}
	}
	return merged
}
