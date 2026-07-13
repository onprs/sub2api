package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/standard"
)

var standardProtocolRegistry = mustStandardProtocolRegistry()

func mustStandardProtocolRegistry() *protocolconv.Registry {
	registry, err := standard.NewRegistry()
	if err != nil {
		panic(fmt.Sprintf("initialize protocol conversion registry: %v", err))
	}
	return registry
}
