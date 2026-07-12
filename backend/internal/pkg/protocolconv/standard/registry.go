// Package standard composes the four standard protocol converters.
package standard

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/anthropic"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/googlegenai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/openaichat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/openairesponses"
)

// NewRegistry registers all standard converters and no vendor adapters.
func NewRegistry() (*protocolconv.Registry, error) {
	registry := protocolconv.NewRegistry()
	converters := []protocolconv.Converter{
		openaichat.New(),
		openairesponses.New(),
		anthropic.New(),
		googlegenai.New(),
	}
	for _, converter := range converters {
		if err := registry.Register(converter); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
