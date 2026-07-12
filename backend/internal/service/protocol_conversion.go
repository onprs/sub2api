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

func convertStandardRequest(body []byte, source, target protocolconv.Protocol, sourceModel string) ([]byte, []protocolconv.Warning, error) {
	return standardProtocolRegistry.ConvertRequest(body, source, target, protocolconv.Options{
		SourceModel: sourceModel,
		LossPolicy:  protocolconv.LossError,
	})
}

func convertStandardResponse(body []byte, source, target protocolconv.Protocol, sourceModel, responseModel string) ([]byte, []protocolconv.Warning, error) {
	options := protocolconv.Options{SourceModel: sourceModel, LossPolicy: protocolconv.LossError}
	response, decodeWarnings, err := standardProtocolRegistry.DecodeResponse(body, source, options)
	if err != nil {
		return nil, decodeWarnings, err
	}
	if responseModel != "" {
		response.Model = responseModel
	}
	converted, encodeWarnings, err := standardProtocolRegistry.EncodeResponse(response, target, options)
	return converted, append(decodeWarnings, encodeWarnings...), err
}

func newStandardStreamSession(source, target protocolconv.Protocol) (*protocolconv.StreamSession, error) {
	return standardProtocolRegistry.NewStreamSession(source, target)
}
