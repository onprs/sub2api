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
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         source,
			IntendedTarget: target,
			UpstreamModel:  sourceModel,
		},
		Options: protocolconv.Options{
			SourceModel: sourceModel,
			LossPolicy:  protocolconv.LossError,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	converted, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, pipeline.Warnings(), err
	}
	return converted.Body, converted.Warnings, nil
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
