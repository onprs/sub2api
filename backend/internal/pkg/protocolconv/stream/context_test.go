package stream

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestContextCompleteTextLifecycle(t *testing.T) {
	ctx := NewContext()
	events := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp-1", Model: "model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentText},
		{Type: ir.EventTextDelta, BlockIndex: 0, Text: "hello"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
		{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
		{Type: ir.EventStreamEnd},
	}
	for _, event := range events {
		require.NoError(t, ctx.Apply(event), event.Type)
	}
	require.True(t, ctx.Ended())
	require.Equal(t, "resp-1", ctx.ResponseID())
	require.Equal(t, "model", ctx.Model())
}

func TestContextRejectsInvalidLifecycle(t *testing.T) {
	tests := []struct {
		name   string
		events []ir.StreamEvent
		match  string
	}{
		{
			name:   "delta before start",
			events: []ir.StreamEvent{{Type: ir.EventTextDelta, BlockIndex: 0}},
			match:  "before stream_start",
		},
		{
			name:   "duplicate start",
			events: []ir.StreamEvent{{Type: ir.EventStreamStart}, {Type: ir.EventStreamStart}},
			match:  "duplicate stream_start",
		},
		{
			name: "wrong block type",
			events: []ir.StreamEvent{
				{Type: ir.EventStreamStart},
				{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentReasoning},
				{Type: ir.EventTextDelta, BlockIndex: 0},
			},
			match: "want \"text\"",
		},
		{
			name: "finish with open block",
			events: []ir.StreamEvent{
				{Type: ir.EventStreamStart},
				{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentText},
				{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
			},
			match: "open content blocks",
		},
		{
			name:   "end before finish",
			events: []ir.StreamEvent{{Type: ir.EventStreamStart}, {Type: ir.EventStreamEnd}},
			match:  "before finish",
		},
		{
			name: "event after end",
			events: []ir.StreamEvent{
				{Type: ir.EventStreamStart},
				{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
				{Type: ir.EventStreamEnd},
				{Type: ir.EventUsage, Usage: &ir.Usage{}},
			},
			match: "after stream_end",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			var err error
			for _, event := range tt.events {
				err = ctx.Apply(event)
				if err != nil {
					break
				}
			}
			require.ErrorContains(t, err, tt.match)
		})
	}
}

func TestContextToolCallIdentity(t *testing.T) {
	ctx := NewContext()
	require.NoError(t, ctx.Apply(ir.StreamEvent{Type: ir.EventStreamStart}))
	require.NoError(t, ctx.Apply(ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: 2, BlockType: ir.ContentToolCall}))
	require.NoError(t, ctx.Apply(ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: 2, ToolCallID: "call-1", ToolName: "read"}))
	require.NoError(t, ctx.Apply(ir.StreamEvent{Type: ir.EventToolCallDelta, BlockIndex: 2, ToolCallID: "call-1", ArgumentsDelta: "{}"}))

	err := ctx.Apply(ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: 2, ToolCallID: "call-1", ToolName: "write"})
	require.ErrorContains(t, err, "changed name")
}

func TestContextsAreIsolatedAcrossConcurrentRequests(t *testing.T) {
	const requests = 64
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := NewContext()
			responseID := fmt.Sprintf("resp-%d", i)
			events := []ir.StreamEvent{
				{Type: ir.EventStreamStart, ResponseID: responseID, Model: "model"},
				{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentText},
				{Type: ir.EventTextDelta, BlockIndex: 0, Text: responseID},
				{Type: ir.EventContentBlockEnd, BlockIndex: 0},
				{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
				{Type: ir.EventStreamEnd},
			}
			for _, event := range events {
				if err := ctx.Apply(event); err != nil {
					errs <- err
					return
				}
			}
			if ctx.ResponseID() != responseID {
				errs <- fmt.Errorf("got response ID %q, want %q", ctx.ResponseID(), responseID)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
