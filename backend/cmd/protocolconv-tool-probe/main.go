// Command protocolconv-tool-probe validates a complete tool call/result round trip.
package main

import (
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
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/standard"
)

type report struct {
	HTTPStatus1         int    `json:"first_http_status"`
	HTTPStatus2         int    `json:"second_http_status"`
	Protocol            string `json:"protocol"`
	Model               string `json:"model"`
	ToolCall            bool   `json:"tool_call"`
	ToolCallIDPreserved bool   `json:"tool_call_id_preserved"`
	FinalText           bool   `json:"final_text"`
	InputTokens         int    `json:"input_tokens,omitempty"`
	OutputTokens        int    `json:"output_tokens,omitempty"`
	LatencyMS           int64  `json:"latency_ms"`
	Warnings            int    `json:"warnings"`
	Error               string `json:"error,omitempty"`
}

func main() {
	var protocolID, endpoint, model string
	flag.StringVar(&protocolID, "protocol", "", "target protocol ID")
	flag.StringVar(&endpoint, "url", "", "explicit target endpoint URL")
	flag.StringVar(&model, "model", "", "explicit model")
	flag.Parse()
	result := run(protocolID, endpoint, model)
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if result.Error != "" || result.HTTPStatus1 >= 400 || result.HTTPStatus2 >= 400 {
		os.Exit(1)
	}
}

func run(protocolID, endpoint, model string) report {
	out := report{Protocol: protocolID, Model: model}
	key := strings.TrimSpace(os.Getenv("SUB2API_PROBE_API_KEY"))
	if key == "" {
		out.Error = "SUB2API_PROBE_API_KEY is not set"
		return out
	}
	if endpoint == "" || model == "" || protocolID == "" {
		out.Error = "-protocol, -url, and -model are required"
		return out
	}
	protocol, err := protocolconv.ParseProtocol(protocolID)
	if err != nil {
		out.Error = sanitize(err.Error())
		return out
	}
	registry, err := standard.NewRegistry()
	if err != nil {
		out.Error = sanitize(err.Error())
		return out
	}
	maxTokens := 512
	request := &ir.Request{Model: model, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "Use read_file with path demo.txt. Do not answer directly."}}}}, Tools: []ir.ToolDefinition{{Type: "function", Name: "read_file", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}, ToolChoice: &ir.ToolChoice{Mode: "tool", Name: "read_file"}, Generation: ir.GenerationConfig{MaxTokens: &maxTokens}}
	started := time.Now()
	first, status, warnings, err := send(registry, protocol, endpoint, key, model, request)
	out.LatencyMS = time.Since(started).Milliseconds()
	out.HTTPStatus1 = status
	out.Warnings += warnings
	if err != nil {
		out.Error = sanitize(err.Error())
		return out
	}
	var call *ir.ContentPart
	for i := range first.Choices {
		for j := range first.Choices[i].Message.Content {
			part := &first.Choices[i].Message.Content[j]
			if part.Type == ir.ContentToolCall {
				copyPart := *part
				call = &copyPart
				break
			}
		}
	}
	if call == nil {
		out.Error = "first response did not contain a tool call"
		return out
	}
	out.ToolCall = true
	request.Messages = append(request.Messages, ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{*call}}, ir.Message{Role: ir.RoleTool, Content: []ir.ContentPart{{Type: ir.ContentToolResult, ToolCallID: call.ToolCallID, ToolName: call.ToolName, ToolResult: json.RawMessage(`"demo content"`)}}}, ir.Message{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "Summarize the tool result in one short sentence."}}})
	request.ToolChoice = &ir.ToolChoice{Mode: "auto"}
	preserved, validationWarnings, err := validateToolCorrelation(registry, protocol, model, request, call.ToolCallID, call.ToolName)
	out.Warnings += validationWarnings
	if err != nil {
		out.Error = sanitize(err.Error())
		return out
	}
	out.ToolCallIDPreserved = preserved
	if !preserved {
		out.Error = "encoded second request did not preserve tool call/result correlation"
		return out
	}
	second, status, warnings, err := send(registry, protocol, endpoint, key, model, request)
	out.HTTPStatus2 = status
	out.Warnings += warnings
	if err != nil {
		out.Error = sanitize(err.Error())
		return out
	}
	for _, choice := range second.Choices {
		for _, part := range choice.Message.Content {
			if part.Type == ir.ContentText && strings.TrimSpace(part.Text) != "" {
				out.FinalText = true
			}
		}
	}
	if !out.FinalText {
		out.Error = "second response did not contain final text"
		return out
	}
	if second.Usage != nil {
		out.InputTokens = second.Usage.InputTokens
		out.OutputTokens = second.Usage.OutputTokens
	}
	return out
}

func validateToolCorrelation(registry *protocolconv.Registry, protocol protocolconv.Protocol, model string, request *ir.Request, callID, toolName string) (bool, int, error) {
	body, warnings, err := registry.EncodeRequest(request, protocol, protocolconv.Options{SourceModel: model, LossPolicy: protocolconv.LossWarn})
	if err != nil {
		return false, len(warnings), err
	}
	decoded, decodeWarnings, err := registry.DecodeRequest(body, protocol, protocolconv.Options{SourceModel: model, LossPolicy: protocolconv.LossWarn})
	warnings = append(warnings, decodeWarnings...)
	if err != nil {
		return false, len(warnings), err
	}
	var hasCall, hasResult bool
	for _, message := range decoded.Messages {
		for _, part := range message.Content {
			switch part.Type {
			case ir.ContentToolCall:
				hasCall = hasCall || part.ToolCallID == callID && part.ToolName == toolName
			case ir.ContentToolResult:
				hasResult = hasResult || part.ToolCallID == callID
			}
		}
	}
	return callID != "" && hasCall && hasResult, len(warnings), nil
}

func send(registry *protocolconv.Registry, protocol protocolconv.Protocol, endpoint, key, model string, request *ir.Request) (*ir.Response, int, int, error) {
	body, warnings, err := registry.EncodeRequest(request, protocol, protocolconv.Options{SourceModel: model, LossPolicy: protocolconv.LossWarn})
	if err != nil {
		return nil, 0, len(warnings), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, len(warnings), err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	if protocol == protocolconv.ProtocolAnthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, len(warnings), err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return nil, resp.StatusCode, len(warnings), readErr
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, len(warnings), fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, string(raw))
	}
	decoded, decodeWarnings, err := registry.DecodeResponse(raw, protocol, protocolconv.Options{SourceModel: model})
	warnings = append(warnings, decodeWarnings...)
	return decoded, resp.StatusCode, len(warnings), err
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
