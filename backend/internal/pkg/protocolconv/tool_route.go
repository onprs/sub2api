package protocolconv

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/toolrouting"
)

// ToolRoute records how a source-protocol tool is represented by the selected
// target protocol. Keys are target-visible names and values restore the source
// kind/name/namespace on the response path.
type ToolRoute struct {
	SourceKind string
	SourceName string
	Namespace  string
}

func buildToolRoutes(request *ir.Request, source, target Protocol) (map[string]ToolRoute, error) {
	if request == nil || source != ProtocolOpenAIResponses || (target != ProtocolOpenAIChat && target != ProtocolAnthropic) {
		return nil, nil
	}
	routes := make(map[string]ToolRoute)
	add := func(targetName string, route ToolRoute) error {
		if targetName == "" {
			return nil
		}
		if previous, ok := routes[targetName]; ok && previous != route {
			return fmt.Errorf("target tool name %q has ambiguous source routes", targetName)
		}
		routes[targetName] = route
		return nil
	}
	for _, tool := range request.Tools {
		switch tool.ProviderType {
		case "", "function":
			if err := add(tool.Name, ToolRoute{SourceKind: "function_call", SourceName: tool.Name}); err != nil {
				return nil, err
			}
		case "custom":
			if err := add(tool.Name, ToolRoute{SourceKind: "custom_tool_call", SourceName: tool.Name}); err != nil {
				return nil, err
			}
		case "tool_search":
			if err := add("tool_search", ToolRoute{SourceKind: "tool_search_call", SourceName: "tool_search"}); err != nil {
				return nil, err
			}
		case "namespace":
			for _, child := range tool.Children {
				targetName := toolrouting.FlattenNamespaceName(tool.Name, child.Name)
				if err := add(targetName, ToolRoute{SourceKind: "function_call", SourceName: child.Name, Namespace: tool.Name}); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(routes) == 0 {
		return nil, nil
	}
	return routes, nil
}

func cloneToolRoutes(routes map[string]ToolRoute) map[string]ToolRoute {
	if len(routes) == 0 {
		return nil
	}
	clone := make(map[string]ToolRoute, len(routes))
	for name, route := range routes {
		clone[name] = route
	}
	return clone
}
