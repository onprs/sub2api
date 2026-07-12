// Package stream contains request-scoped stream lifecycle state.
package stream

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

// Context validates and tracks one converted stream. A Context must never be
// reused across requests or accessed concurrently.
type Context struct {
	responseID string
	model      string
	started    bool
	finished   bool
	ended      bool

	blocks    map[int]ir.ContentType
	toolCalls map[string]string
}

// NewContext creates isolated stream state.
func NewContext() *Context {
	return &Context{
		blocks:    make(map[int]ir.ContentType),
		toolCalls: make(map[string]string),
	}
}

// Apply validates event ordering and updates this request's state.
func (c *Context) Apply(event ir.StreamEvent) error {
	if c == nil {
		return fmt.Errorf("stream context is nil")
	}
	if c.ended {
		return fmt.Errorf("event %q received after stream_end", event.Type)
	}

	switch event.Type {
	case ir.EventStreamStart:
		if c.started {
			return fmt.Errorf("duplicate stream_start")
		}
		if c.finished {
			return fmt.Errorf("stream_start received after finish")
		}
		c.started = true
		c.responseID = event.ResponseID
		c.model = event.Model
		return nil
	case ir.EventStreamEnd:
		if !c.started {
			return fmt.Errorf("stream_end received before stream_start")
		}
		if len(c.blocks) != 0 {
			return fmt.Errorf("stream_end received with %d open content blocks", len(c.blocks))
		}
		if !c.finished {
			return fmt.Errorf("stream_end received before finish")
		}
		c.ended = true
		return nil
	}

	if !c.started {
		return fmt.Errorf("event %q received before stream_start", event.Type)
	}
	if c.finished && event.Type != ir.EventUsage && event.Type != ir.EventError {
		return fmt.Errorf("event %q received after finish", event.Type)
	}

	switch event.Type {
	case ir.EventContentBlockStart:
		if _, exists := c.blocks[event.BlockIndex]; exists {
			return fmt.Errorf("content block %d already open", event.BlockIndex)
		}
		c.blocks[event.BlockIndex] = event.BlockType
	case ir.EventContentBlockEnd:
		if _, exists := c.blocks[event.BlockIndex]; !exists {
			return fmt.Errorf("content block %d is not open", event.BlockIndex)
		}
		delete(c.blocks, event.BlockIndex)
	case ir.EventTextDelta:
		if err := c.requireBlock(event.BlockIndex, ir.ContentText); err != nil {
			return err
		}
	case ir.EventReasoningDelta:
		if err := c.requireBlock(event.BlockIndex, ir.ContentReasoning); err != nil {
			return err
		}
	case ir.EventToolCallStart:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("tool_call_start requires ID and name")
		}
		if existing, exists := c.toolCalls[event.ToolCallID]; exists && existing != event.ToolName {
			return fmt.Errorf("tool call %q changed name from %q to %q", event.ToolCallID, existing, event.ToolName)
		}
		c.toolCalls[event.ToolCallID] = event.ToolName
		if err := c.requireBlock(event.BlockIndex, ir.ContentToolCall); err != nil {
			return err
		}
	case ir.EventToolCallDelta, ir.EventToolCallEnd:
		if _, exists := c.toolCalls[event.ToolCallID]; !exists {
			return fmt.Errorf("unknown tool call %q", event.ToolCallID)
		}
		if err := c.requireBlock(event.BlockIndex, ir.ContentToolCall); err != nil {
			return err
		}
	case ir.EventFinish:
		if c.finished {
			return fmt.Errorf("duplicate finish")
		}
		if len(c.blocks) != 0 {
			return fmt.Errorf("finish received with %d open content blocks", len(c.blocks))
		}
		if event.FinishReason == nil {
			return fmt.Errorf("finish requires a reason")
		}
		c.finished = true
	case ir.EventUsage:
		if event.Usage == nil {
			return fmt.Errorf("usage event requires usage")
		}
	case ir.EventError:
		if event.Error == nil {
			return fmt.Errorf("error event requires error details")
		}
	default:
		return fmt.Errorf("unsupported stream event %q", event.Type)
	}
	return nil
}

func (c *Context) requireBlock(index int, want ir.ContentType) error {
	got, exists := c.blocks[index]
	if !exists {
		return fmt.Errorf("content block %d is not open", index)
	}
	if got != want {
		return fmt.Errorf("content block %d has type %q, want %q", index, got, want)
	}
	return nil
}

// Ended reports whether the unique stream_end event was accepted.
func (c *Context) Ended() bool {
	return c != nil && c.ended
}

// ResponseID returns metadata captured at stream_start.
func (c *Context) ResponseID() string {
	if c == nil {
		return ""
	}
	return c.responseID
}

// Model returns metadata captured at stream_start.
func (c *Context) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}
