package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
)

// ForwardGoogleGenAI converts one Google generateContent request to OpenAI
// Responses, runs the existing OpenAI/Codex provider core, and converts the
// structured actual upstream result back to Google. Scheduling, failover,
// concurrency, and billing remain owned by the handler and provider services.
func (s *OpenAIGatewayService) ForwardGoogleGenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	clientModel string,
	routingModel string,
	stream bool,
	body []byte,
) (*OpenAIForwardResult, error) {
	if c == nil || c.Writer == nil {
		return nil, errors.New("response writer is required for Google GenAI forwarding")
	}
	if account == nil || account.Platform != PlatformOpenAI {
		return nil, fmt.Errorf("an OpenAI account is required for Google GenAI forwarding")
	}

	upstreamModel := account.GetMappedModel(routingModel)
	pipelineConfig := protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolGoogleGenAI,
			IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel:    clientModel,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
	}
	pipeline, convertedBody, err := newGoogleGenAIResponsesAttempt(body, pipelineConfig, stream)
	if err != nil {
		return nil, err
	}
	output, err := newGoogleGenAIProtocolOutput(c.Writer, pipeline, stream)
	if err != nil {
		return nil, err
	}
	output.requestBody = append([]byte(nil), body...)
	output.pipelineConfig = pipelineConfig

	result, forwardErr := s.forwardWithProtocolOutput(ctx, c, account, convertedBody, output)
	if forwardErr != nil {
		return nil, forwardErr
	}
	if result != nil {
		result.Model = clientModel
		result.Stream = stream
		result.ClientDisconnect = result.ClientDisconnect || output.ClientDisconnected()
	}
	return result, nil
}

func newGoogleGenAIResponsesAttempt(body []byte, config protocolconv.PipelineConfig, stream bool) (*protocolconv.Pipeline, []byte, error) {
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, config)
	if err != nil {
		return nil, nil, err
	}
	converted, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, nil, fmt.Errorf("convert Google GenAI request to OpenAI Responses: %w", err)
	}
	convertedBody, err := setOpenAIResponsesStream(converted.Body, stream)
	if err != nil {
		return nil, nil, err
	}
	return pipeline, convertedBody, nil
}

func setOpenAIResponsesStream(body []byte, stream bool) ([]byte, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode converted OpenAI Responses request: %w", err)
	}
	if stream {
		value["stream"] = true
	} else {
		delete(value, "stream")
	}
	converted, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode converted OpenAI Responses request: %w", err)
	}
	return converted, nil
}

type googleGenAIProtocolOutput struct {
	writer   http.ResponseWriter
	pipeline *protocolconv.Pipeline
	renderer *protocolconv.Renderer
	session  *protocolconv.StreamSession
	stream   bool

	headersWritten     bool
	pendingStatus      int
	pendingHeaders     http.Header
	outputStarted      bool
	clientDisconnected bool
	actualProtocol     protocolconv.Protocol

	requestBody    []byte
	pipelineConfig protocolconv.PipelineConfig
}

func newGoogleGenAIProtocolOutput(writer http.ResponseWriter, pipeline *protocolconv.Pipeline, stream bool) (*googleGenAIProtocolOutput, error) {
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	return &googleGenAIProtocolOutput{writer: writer, pipeline: pipeline, renderer: renderer, stream: stream}, nil
}

func (o *googleGenAIProtocolOutput) NewRetryAttempt() (openAIProtocolOutput, error) {
	if len(o.requestBody) == 0 {
		return nil, errors.New("source request is missing for Google GenAI retry output")
	}
	pipeline, _, err := newGoogleGenAIResponsesAttempt(o.requestBody, o.pipelineConfig, o.stream)
	if err != nil {
		return nil, err
	}
	retry, err := newGoogleGenAIProtocolOutput(o.writer, pipeline, o.stream)
	if err != nil {
		return nil, err
	}
	retry.requestBody = append([]byte(nil), o.requestBody...)
	retry.pipelineConfig = o.pipelineConfig
	return retry, nil
}

func (o *googleGenAIProtocolOutput) WriteResponse(response protocoltransport.Response) error {
	if o.stream {
		return errors.New("streaming Google output received a complete response")
	}
	if err := response.Validate(); err != nil {
		return err
	}
	converted, err := o.pipeline.ConvertResponse(response.Body, response.ActualProtocol)
	if err != nil {
		return err
	}
	if err := o.renderer.RenderJSON(o.writer, response.StatusCode, response.Headers, converted.Body); err != nil {
		o.clientDisconnected = true
		return err
	}
	o.outputStarted = true
	return nil
}

func (o *googleGenAIProtocolOutput) WriteStreamHeaders(status int, headers http.Header, actual protocolconv.Protocol) error {
	if !o.stream {
		return errors.New("non-streaming Google output received stream headers")
	}
	if o.headersWritten {
		if actual != o.actualProtocol {
			return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
		}
		return nil
	}
	if err := actual.Validate(); err != nil {
		return err
	}
	session, err := o.pipeline.NewStreamProcessor(actual)
	if err != nil {
		return err
	}
	o.session = session
	o.actualProtocol = actual
	o.pendingStatus = status
	o.pendingHeaders = protocoltransport.CloneHeaders(headers)
	return nil
}

func (o *googleGenAIProtocolOutput) WriteStreamEvent(actual protocolconv.Protocol, payload []byte) error {
	if o.session == nil {
		return errors.New("stream event received before structured stream initialization")
	}
	if actual != o.actualProtocol {
		return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
	}
	payloads, _, err := o.session.Convert(payload)
	if err != nil {
		return err
	}
	if o.clientDisconnected {
		return nil
	}
	for _, converted := range payloads {
		if err := o.ensureStreamHeaders(); err != nil {
			return err
		}
		framed, err := o.renderer.FrameStreamEvent(converted)
		if err != nil {
			return err
		}
		if _, err := o.writer.Write(framed); err != nil {
			o.clientDisconnected = true
			return nil
		}
		o.outputStarted = true
	}
	if len(payloads) > 0 {
		if flusher, ok := o.writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return nil
}

func (o *googleGenAIProtocolOutput) WriteStreamKeepalive() error {
	// Google clients only receive source-protocol JSON events. The service may
	// still run its upstream timeout ticker, but no Responses comment is exposed.
	return nil
}

func (o *googleGenAIProtocolOutput) FinalizeStream(actual protocolconv.Protocol) error {
	if !o.stream || o.session == nil {
		return errors.New("structured stream was not initialized")
	}
	if actual != o.actualProtocol {
		return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
	}
	payloads, _, err := o.session.Finalize()
	if err != nil {
		return err
	}
	for _, converted := range payloads {
		if err := o.ensureStreamHeaders(); err != nil {
			return err
		}
		framed, err := o.renderer.FrameStreamEvent(converted)
		if err != nil {
			return err
		}
		if !o.clientDisconnected {
			if _, err := o.writer.Write(framed); err != nil {
				o.clientDisconnected = true
				continue
			}
			o.outputStarted = true
		}
	}
	if !o.clientDisconnected {
		if flusher, ok := o.writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return nil
}

func (o *googleGenAIProtocolOutput) ensureStreamHeaders() error {
	if o.headersWritten {
		return nil
	}
	if err := o.renderer.WriteStreamHeaders(o.writer, o.pendingStatus, o.pendingHeaders); err != nil {
		return err
	}
	o.headersWritten = true
	return nil
}

func (o *googleGenAIProtocolOutput) ClientOutputStarted() bool { return o.outputStarted }
func (o *googleGenAIProtocolOutput) ClientDisconnected() bool  { return o.clientDisconnected }

var _ openAIProtocolOutput = (*googleGenAIProtocolOutput)(nil)
