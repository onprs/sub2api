package protocolconv

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestStandardProtocols(t *testing.T) {
	protocols := StandardProtocols()
	require.Equal(t, []Protocol{
		ProtocolOpenAIChat,
		ProtocolOpenAIResponses,
		ProtocolAnthropic,
		ProtocolGoogleGenAI,
	}, protocols)

	protocols[0] = "changed"
	require.Equal(t, ProtocolOpenAIChat, StandardProtocols()[0])
}

func TestParseProtocolDoesNotInferAliases(t *testing.T) {
	parsed, err := ParseProtocol("  OPENAI_RESPONSES ")
	require.NoError(t, err)
	require.Equal(t, ProtocolOpenAIResponses, parsed)

	_, err = ParseProtocol("/v1/responses")
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorUnsupportedProtocol, conversionErr.Code)
}

func TestRegistryIdentityPreservesOriginalBytes(t *testing.T) {
	registry := NewRegistry()
	body := []byte(" {\n  \"model\" : \"test\", \"unknown\": [1, 2]\n}\n")

	converted, warnings, err := registry.ConvertRequest(body, ProtocolOpenAIChat, ProtocolOpenAIChat, Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, body, converted)
}

func TestRegistryIdentityRejectsInvalidJSON(t *testing.T) {
	registry := NewRegistry()
	_, _, err := registry.ConvertResponse([]byte(`{"broken":`), ProtocolAnthropic, ProtocolAnthropic, Options{})
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorInvalidJSON, conversionErr.Code)
	require.Equal(t, ProtocolAnthropic, conversionErr.Protocol)
}

func TestRegistryRejectsDuplicateConverter(t *testing.T) {
	registry := NewRegistry()
	converter := &stubConverter{protocol: ProtocolOpenAIChat}
	require.NoError(t, registry.Register(converter))

	err := registry.Register(converter)
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorConversion, conversionErr.Code)
}

func TestRegistryConvertsThroughIR(t *testing.T) {
	registry := NewRegistry()
	source := &stubConverter{
		protocol: ProtocolOpenAIChat,
		decodeRequest: func(body []byte) (*ir.Request, []Warning, error) {
			return &ir.Request{Model: "source", Messages: []ir.Message{{Role: ir.RoleUser}}}, []Warning{{Code: WarningNormalizedField}}, nil
		},
	}
	target := &stubConverter{
		protocol: ProtocolAnthropic,
		encodeRequest: func(request *ir.Request, _ Options) ([]byte, []Warning, error) {
			require.Equal(t, "source", request.Model)
			return []byte(`{"model":"target"}`), []Warning{{Code: WarningDroppedField}}, nil
		},
	}
	require.NoError(t, registry.Register(source))
	require.NoError(t, registry.Register(target))

	body, warnings, err := registry.ConvertRequest([]byte(`{"model":"source"}`), ProtocolOpenAIChat, ProtocolAnthropic, Options{})
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"target"}`, string(body))
	require.Len(t, warnings, 2)
}

func TestRegistryWrapsUntypedConverterError(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(&stubConverter{
		protocol: ProtocolOpenAIChat,
		decodeRequest: func([]byte) (*ir.Request, []Warning, error) {
			return nil, nil, errors.New("bad source")
		},
	}))
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolAnthropic}))

	_, _, err := registry.ConvertRequest([]byte(`{}`), ProtocolOpenAIChat, ProtocolAnthropic, Options{})
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorConversion, conversionErr.Code)
	require.Equal(t, ProtocolOpenAIChat, conversionErr.Protocol)
	require.ErrorContains(t, err, "bad source")
}

func TestCheckCapabilityStrictAndWarningPolicies(t *testing.T) {
	caps := CapabilitySet{CapabilityText: SupportFull, CapabilityDeveloper: SupportLossy}

	warnings, err := checkCapability(ProtocolAnthropic, caps, CapabilityText, "messages[0]", Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)

	warnings, err = checkCapability(ProtocolAnthropic, caps, CapabilityDeveloper, "messages[0].role", Options{})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Equal(t, WarningNormalizedField, warnings[0].Code)

	_, err = checkCapability(ProtocolAnthropic, caps, CapabilityAudio, "messages[0].content[0]", Options{})
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorUnsupportedCapability, conversionErr.Code)

	warnings, err = checkCapability(ProtocolAnthropic, caps, CapabilityAudio, "messages[0].content[0]", Options{LossPolicy: LossWarn})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Equal(t, WarningUnsupportedCapability, warnings[0].Code)
}

type stubConverter struct {
	protocol       Protocol
	decodeRequest  func([]byte) (*ir.Request, []Warning, error)
	encodeRequest  func(*ir.Request, Options) ([]byte, []Warning, error)
	decodeResponse func([]byte) (*ir.Response, []Warning, error)
	encodeResponse func(*ir.Response, Options) ([]byte, []Warning, error)
}

func (s *stubConverter) Protocol() Protocol          { return s.protocol }
func (s *stubConverter) Capabilities() CapabilitySet { return nil }
func (s *stubConverter) DecodeRequest(body []byte, _ Options) (*ir.Request, []Warning, error) {
	if s.decodeRequest != nil {
		return s.decodeRequest(body)
	}
	var request ir.Request
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, nil, err
	}
	return &request, nil, nil
}
func (s *stubConverter) EncodeRequest(request *ir.Request, options Options) ([]byte, []Warning, error) {
	if s.encodeRequest != nil {
		return s.encodeRequest(request, options)
	}
	body, err := json.Marshal(request)
	return body, nil, err
}
func (s *stubConverter) DecodeResponse(body []byte, _ Options) (*ir.Response, []Warning, error) {
	if s.decodeResponse != nil {
		return s.decodeResponse(body)
	}
	var response ir.Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, nil, err
	}
	return &response, nil, nil
}
func (s *stubConverter) EncodeResponse(response *ir.Response, options Options) ([]byte, []Warning, error) {
	if s.encodeResponse != nil {
		return s.encodeResponse(response, options)
	}
	body, err := json.Marshal(response)
	return body, nil, err
}
func (s *stubConverter) NewStreamDecoder() StreamDecoder { return nil }
func (s *stubConverter) NewStreamEncoder() StreamEncoder { return nil }
