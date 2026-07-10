package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

type openAISSEDataAccumulator struct {
	eventType string
	lines     []string
}

func (a *openAISSEDataAccumulator) AddLine(line string, fn func([]byte)) {
	if fn == nil {
		return
	}
	trimmedLine := strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(trimmedLine) == "" {
		a.Flush(fn)
		return
	}
	if strings.HasPrefix(trimmedLine, ":") {
		return
	}
	if eventType, ok := extractOpenAISSEEventLine(trimmedLine); ok {
		a.eventType = eventType
		return
	}
	if data, ok := extractOpenAISSEDataLine(trimmedLine); ok {
		a.lines = append(a.lines, data)
	}
}

func (a *openAISSEDataAccumulator) Flush(fn func([]byte)) {
	if fn == nil {
		a.eventType = ""
		a.lines = a.lines[:0]
		return
	}
	if len(a.lines) == 0 {
		a.eventType = ""
		return
	}
	emitOpenAISSEDataPayloads(a.lines, a.eventType, fn)
	a.eventType = ""
	a.lines = a.lines[:0]
}

func forEachOpenAISSEDataPayload(body string, fn func([]byte)) {
	if fn == nil || strings.TrimSpace(body) == "" {
		return
	}
	var acc openAISSEDataAccumulator
	for _, line := range strings.Split(body, "\n") {
		acc.AddLine(line, fn)
	}
	acc.Flush(fn)
}

func emitOpenAISSEDataPayloads(lines []string, eventType string, fn func([]byte)) {
	if fn == nil || len(lines) == 0 {
		return
	}
	if len(lines) == 1 {
		emitOpenAISSEDataPayload(lines[0], eventType, fn)
		return
	}
	joined := strings.Join(lines, "\n")
	if gjson.Valid(joined) {
		emitOpenAISSEDataPayload(joined, eventType, fn)
		return
	}
	for _, line := range lines {
		emitOpenAISSEDataPayload(line, eventType, fn)
	}
}

func emitOpenAISSEDataPayload(data string, eventType string, fn func([]byte)) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return
	}
	data = openAICompatPayloadWithEventType(data, eventType)
	fn([]byte(data))
}
