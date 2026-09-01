package protocolconv

import (
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

// PipelineConfig defines one request's explicit protocol route. Provider and
// transport policy stay outside this package.
type PipelineConfig struct {
	Route   Route
	Options Options

	// ResponseOptions optionally overrides Options for buffered and streaming
	// response conversion. Nil preserves the historical shared policy.
	ResponseOptions *Options

	// MetadataStore and MetadataScope are optional. When configured together,
	// they bridge provider replay metadata only across this fully isolated scope.
	MetadataStore MetadataStore
	MetadataScope MetadataScope
}

// ConvertedRequest is the target-protocol body and route metadata produced for
// one upstream attempt.
type ConvertedRequest struct {
	Body           []byte
	Source         Protocol
	IntendedTarget Protocol
	ClientModel    string
	UpstreamModel  string
	Warnings       []Warning
}

// ConvertedResponse is a source-protocol response converted from the protocol
// actually used by the successful upstream attempt.
type ConvertedResponse struct {
	Body           []byte
	Source         Protocol
	ActualUpstream Protocol
	Warnings       []Warning
}

// Pipeline binds request and response conversion state to exactly one client
// request. ConvertRequest is one-shot. Response and stream conversion require
// an explicit actual upstream protocol so transport fallback cannot be inferred
// from an account, endpoint, or model name.
type Pipeline struct {
	registry *Registry
	config   PipelineConfig

	mu               sync.Mutex
	requestAttempted bool
	requestConverted bool
	toolRoutes       map[string]ToolRoute
	warnings         []Warning
	metadataBridge   *providerMetadataBridge
}

// NewPipeline validates an explicit route and creates a request-scoped
// conversion pipeline.
func NewPipeline(registry *Registry, config PipelineConfig) (*Pipeline, error) {
	if registry == nil {
		return nil, &Error{Code: ErrorConverterUnavailable, Message: "nil converter registry"}
	}
	if err := config.Route.Validate(); err != nil {
		return nil, err
	}
	if _, err := registry.Converter(config.Route.Source); err != nil {
		return nil, err
	}
	if _, err := registry.Converter(config.Route.IntendedTarget); err != nil {
		return nil, err
	}
	metadataBridge, err := newProviderMetadataBridge(config.MetadataStore, config.MetadataScope, config.Route)
	if err != nil {
		return nil, err
	}
	if config.ResponseOptions != nil {
		responseOptions := *config.ResponseOptions
		config.ResponseOptions = &responseOptions
	}
	return &Pipeline{registry: registry, config: config, metadataBridge: metadataBridge}, nil
}

// ConvertRequest converts the client request once. A failed conversion still
// consumes the pipeline because converter context must never be reused after a
// partial request phase.
func (p *Pipeline) ConvertRequest(body []byte) (ConvertedRequest, error) {
	if p == nil {
		return ConvertedRequest{}, &Error{Code: ErrorConversion, Message: "nil conversion pipeline"}
	}

	p.mu.Lock()
	if p.requestAttempted {
		p.mu.Unlock()
		return ConvertedRequest{}, &Error{Code: ErrorConversion, Protocol: p.config.Route.Source, Message: "request conversion already attempted"}
	}
	p.requestAttempted = true
	p.mu.Unlock()

	options := p.options()
	var converted []byte
	var warnings []Warning
	var routes map[string]ToolRoute
	var err error
	if p.config.Route.Source == p.config.Route.IntendedTarget {
		converted, warnings, err = identityJSON(body, p.config.Route.Source, "request")
	} else {
		var request *ir.Request
		var decodeWarnings []Warning
		request, decodeWarnings, err = p.registry.DecodeRequest(body, p.config.Route.Source, options)
		warnings = append(warnings, decodeWarnings...)
		if err == nil {
			p.metadataBridge.injectRequest(request)
			routes, err = buildToolRoutes(request, p.config.Route.Source, p.config.Route.IntendedTarget)
		}
		if err == nil {
			var encodeWarnings []Warning
			converted, encodeWarnings, err = p.registry.EncodeRequest(request, p.config.Route.IntendedTarget, options)
			warnings = append(warnings, encodeWarnings...)
		}
	}

	p.mu.Lock()
	p.warnings = append(p.warnings, warnings...)
	if err == nil {
		p.requestConverted = true
		p.toolRoutes = cloneToolRoutes(routes)
	}
	p.mu.Unlock()
	if err != nil {
		return ConvertedRequest{}, err
	}

	return ConvertedRequest{
		Body:           converted,
		Source:         p.config.Route.Source,
		IntendedTarget: p.config.Route.IntendedTarget,
		ClientModel:    p.config.Route.ClientModel,
		UpstreamModel:  p.config.Route.UpstreamModel,
		Warnings:       cloneWarnings(warnings),
	}, nil
}

// ConvertResponse converts a complete response from the protocol actually used
// by the upstream attempt. Identity conversion validates JSON and preserves the
// exact response bytes.
func (p *Pipeline) ConvertResponse(body []byte, actualUpstream Protocol) (ConvertedResponse, error) {
	if err := p.requireConvertedRequest(); err != nil {
		return ConvertedResponse{}, err
	}
	if err := actualUpstream.Validate(); err != nil {
		return ConvertedResponse{}, err
	}

	options := p.responseOptionsFor(actualUpstream)
	var converted []byte
	var warnings []Warning
	var err error
	if actualUpstream == p.config.Route.Source {
		converted, warnings, err = identityJSON(body, actualUpstream, "response")
	} else {
		var responseWarnings []Warning
		response, decodeWarnings, decodeErr := p.registry.DecodeResponse(body, actualUpstream, options)
		warnings = append(warnings, decodeWarnings...)
		if decodeErr != nil {
			err = decodeErr
		} else {
			if p.metadataBridge != nil && actualUpstream == p.config.MetadataScope.Protocol {
				err = p.metadataBridge.cacheResponse(response)
			}
			if err == nil && p.config.Route.ClientModel != "" {
				response.Model = p.config.Route.ClientModel
			}
			if err == nil {
				converted, responseWarnings, err = p.registry.EncodeResponse(response, p.config.Route.Source, options)
				warnings = append(warnings, responseWarnings...)
			}
		}
	}
	p.appendWarnings(warnings)
	if err != nil {
		return ConvertedResponse{}, err
	}
	return ConvertedResponse{
		Body:           converted,
		Source:         p.config.Route.Source,
		ActualUpstream: actualUpstream,
		Warnings:       cloneWarnings(warnings),
	}, nil
}

// NewStreamProcessor creates isolated decoder, encoder, and lifecycle state for
// one upstream response stream.
func (p *Pipeline) NewStreamProcessor(actualUpstream Protocol) (*StreamSession, error) {
	if err := p.requireConvertedRequest(); err != nil {
		return nil, err
	}
	if err := actualUpstream.Validate(); err != nil {
		return nil, err
	}
	if actualUpstream == p.config.Route.Source {
		return newIdentityStreamSession(actualUpstream), nil
	}
	session, err := p.registry.NewStreamSessionWithOptions(actualUpstream, p.config.Route.Source, p.responseOptionsFor(actualUpstream))
	if err != nil {
		return nil, err
	}
	if p.metadataBridge != nil && actualUpstream == p.config.MetadataScope.Protocol {
		session.metadataBridge = p.metadataBridge
	}
	session.warningSink = p.appendWarnings
	return session, nil
}

// Warnings returns a snapshot of warnings accumulated across completed phases.
func (p *Pipeline) Warnings() []Warning {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneWarnings(p.warnings)
}

func (p *Pipeline) options() Options {
	return p.optionsWithBase(p.config.Options)
}

func (p *Pipeline) responseOptions() Options {
	if p.config.ResponseOptions == nil {
		return p.options()
	}
	return p.optionsWithBase(*p.config.ResponseOptions)
}

func (p *Pipeline) responseOptionsFor(actualUpstream Protocol) Options {
	options := p.responseOptions()
	if p.config.Route.Source == ProtocolAnthropic && actualUpstream != ProtocolAnthropic {
		options.GenerateAnthropicResponseID = true
	}
	return options
}

func (p *Pipeline) optionsWithBase(options Options) Options {
	if options.SourceModel == "" {
		options.SourceModel = p.config.Route.UpstreamModel
	}
	if options.ResponseModel == "" {
		options.ResponseModel = p.config.Route.ClientModel
	}
	p.mu.Lock()
	options.ToolRoutes = cloneToolRoutes(p.toolRoutes)
	p.mu.Unlock()
	return options
}

func (p *Pipeline) requireConvertedRequest() error {
	if p == nil {
		return &Error{Code: ErrorConversion, Message: "nil conversion pipeline"}
	}
	p.mu.Lock()
	converted := p.requestConverted
	p.mu.Unlock()
	if !converted {
		return &Error{Code: ErrorConversion, Protocol: p.config.Route.Source, Message: "request conversion has not completed"}
	}
	return nil
}

func (p *Pipeline) appendWarnings(warnings []Warning) {
	p.mu.Lock()
	p.warnings = append(p.warnings, warnings...)
	p.mu.Unlock()
}

func cloneWarnings(warnings []Warning) []Warning {
	if len(warnings) == 0 {
		return nil
	}
	return append([]Warning(nil), warnings...)
}
