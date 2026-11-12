package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The round that rejects a tool's call a second time drops the tool and
// publishes tool.excluded once, naming the tool, the reason, the failure
// count and that round.
func TestToolLoop_SecondRejectionPublishesToolExcluded(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "blocked_tool", Description: "always blocked",
		InputSchema: json.RawMessage(`{"type": "object"}`), Mode: "mock",
	}))
	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{blockedTool: "blocked_tool", reason: "blocked"}))
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	got := make(chan events.ToolExcludedData, 4)
	bus.Subscribe(events.EventToolExcluded, func(e *events.Event) {
		if d, ok := e.Data.(*events.ToolExcludedData); ok {
			got <- *d
		}
	})
	stage := NewProviderStageWithHooks(mock.NewToolProvider("test", "model", false, nil), registry, nil,
		&ProviderConfig{}, events.NewEmitter(bus, "run", "sess", "conv"), hookReg)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"blocked_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.maxRounds = 10

	for round := 1; round <= 3; round++ {
		resp := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c" + string(rune('0'+round)), Name: "blocked_tool", Args: json.RawMessage(`{}`)},
		}}
		_, _, _ = loop.afterRound(context.Background(), []string{"blocked_tool"}, &resp, true, roundRef{round: round})
	}

	select {
	case d := <-got:
		assert.Equal(t, events.ToolExcludedData{
			ToolName: "blocked_tool", Reason: "rejected twice in one tool loop", Rejections: 2, Round: 2,
		}, d)
	case <-time.After(2 * time.Second):
		t.Fatal("no tool.excluded event")
	}
	select {
	case d := <-got:
		t.Fatalf("tool.excluded published again: %+v", d)
	case <-time.After(100 * time.Millisecond):
	}
}
