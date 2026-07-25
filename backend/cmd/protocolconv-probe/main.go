// Command protocolconv-probe validates Go conversion behavior against an
// explicitly selected HTTP endpoint. Credentials are read only from the
// SUB2API_PROBE_API_KEY environment variable.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/standard"
)

type report struct {
	HTTPStatus      int    `json:"http_status"`
	Source          string `json:"source_protocol"`
	Target          string `json:"target_protocol"`
	Model           string `json:"model"`
	Stream          bool   `json:"stream"`
	LatencyMS       int64  `json:"latency_ms"`
	InputTokens     int    `json:"input_tokens,omitempty"`
	OutputTokens    int    `json:"output_tokens,omitempty"`
	CacheReadTokens int    `json:"cache_read_tokens,omitempty"`
	Warnings        int    `json:"warnings"`
	Error           string `json:"error,omitempty"`
}

func main() {
	var sourceID, targetID, endpoint, model, input string
	var stream bool
	flag.StringVar(&sourceID, "source", string(protocolconv.ProtocolOpenAIChat), "source protocol ID")
	flag.StringVar(&targetID, "target", "", "target/upstream protocol ID")
	flag.StringVar(&endpoint, "url", "", "explicit target endpoint URL")
	flag.StringVar(&model, "model", "", "explicit model")
	flag.StringVar(&input, "input", "Reply with exactly: probe-ok", "text input")
	flag.BoolVar(&stream, "stream", false, "request and validate a stream")
	flag.Parse()

	result := run(sourceID, targetID, endpoint, model, input, stream)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(result)
	if result.Error != "" || result.HTTPStatus >= 400 {
		os.Exit(1)
	}
}

func run(sourceID, targetID, endpoint, model, input string, stream bool) report {
	result := report{Source: sourceID, Target: targetID, Model: model, Stream: stream}
	key := strings.TrimSpace(os.Getenv("SUB2API_PROBE_API_KEY"))
	if key == "" {
		result.Error = "SUB2API_PROBE_API_KEY is not set"
		return result
	}
	if endpoint == "" || targetID == "" || model == "" {
		result.Error = "-url, -target, and -model are required"
		return result
	}
	source, err := protocolconv.ParseProtocol(sourceID)
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	target, err := protocolconv.ParseProtocol(targetID)
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	registry, err := standard.NewRegistry()
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}

	fixture, err := sourceFixture(source, model, input, stream)
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	request, warnings, err := registry.DecodeRequest(fixture, source, protocolconv.Options{SourceModel: model})
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	request.Model = model
	request.Stream.Enabled = stream
	request.Stream.IncludeUsage = stream
	body, encodeWarnings, err := registry.EncodeRequest(request, target, protocolconv.Options{SourceModel: model, LossPolicy: protocolconv.LossError})
	warnings = append(warnings, encodeWarnings...)
	result.Warnings = len(warnings)
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	if target == protocolconv.ProtocolAnthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		result.Error = sanitize(string(raw))
		return result
	}
	if stream {
		parseStream(resp.Body, registry, target, &result)
		return result
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	decoded, _, err := registry.DecodeResponse(raw, target, protocolconv.Options{SourceModel: model})
	if err != nil {
		result.Error = sanitize(err.Error())
		return result
	}
	if decoded.Usage != nil {
		result.InputTokens = decoded.Usage.InputTokens
		result.OutputTokens = decoded.Usage.OutputTokens
		result.CacheReadTokens = decoded.Usage.CacheReadTokens
	}
	return result
}

func parseStream(reader io.Reader, registry *protocolconv.Registry, protocol protocolconv.Protocol, result *report) {
	converter, err := registry.Converter(protocol)
	if err != nil {
		result.Error = sanitize(err.Error())
		return
	}
	decoder := converter.NewStreamDecoder()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		events, _, err := decoder.Decode([]byte(data))
		if err != nil {
			result.Error = sanitize(err.Error())
			return
		}
		for _, event := range events {
			if event.Usage != nil {
				result.InputTokens = event.Usage.InputTokens
				result.OutputTokens = event.Usage.OutputTokens
				result.CacheReadTokens = event.Usage.CacheReadTokens
			}
		}
	}
	if err := scanner.Err(); err != nil {
		result.Error = sanitize(err.Error())
		return
	}
	if _, _, err := decoder.Finalize(); err != nil {
		result.Error = sanitize(err.Error())
	}
}

func sourceFixture(protocol protocolconv.Protocol, model, input string, stream bool) ([]byte, error) {
	var value any
	switch protocol {
	case protocolconv.ProtocolOpenAIChat:
		value = map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": input}}, "stream": stream, "stream_options": map[string]any{"include_usage": stream}}
	case protocolconv.ProtocolOpenAIResponses:
		value = map[string]any{"model": model, "input": input, "stream": stream}
	case protocolconv.ProtocolAnthropic:
		value = map[string]any{"model": model, "max_tokens": 128, "messages": []any{map[string]any{"role": "user", "content": input}}, "stream": stream}
	case protocolconv.ProtocolGoogleGenAI:
		value = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": input}}}}}
	default:
		return nil, fmt.Errorf("unsupported source protocol %q", protocol)
	}
	return json.Marshal(value)
}
func sanitize(value string) string {
	if key := strings.TrimSpace(os.Getenv("SUB2API_PROBE_API_KEY")); key != "" {
		value = strings.ReplaceAll(value, key, "[redacted]")
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
