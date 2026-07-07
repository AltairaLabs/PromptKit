package stage

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestPipelineCompleted_ReportsMessageCount verifies that the pipeline.completed
// event carries the real message/token/cost totals observed during the run,
// instead of the placeholder zeros that made "messageCount:0" show up in logs
// even for pipelines that produced messages.
func TestPipelineCompleted_ReportsMessageCount(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	got := make(chan *events.PipelineCompletedData, 1)
	bus.Subscribe(events.EventPipelineCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.PipelineCompletedData); ok {
			select {
			case got <- d:
			default:
			}
		}
	})
	emitter := events.NewEmitter(bus, "run", "sess", "conv")

	pipeline, err := NewPipelineBuilder().
		AddStage(&testPassthroughStage{name: "s"}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	pipeline.eventEmitter = emitter

	msg := types.Message{
		Role:     roleAssistant,
		Content:  "hello",
		CostInfo: &types.CostInfo{InputTokens: 5, OutputTokens: 7, TotalCost: 0.01},
	}
	if _, err := pipeline.ExecuteSync(context.Background(), StreamElement{Message: &msg}); err != nil {
		t.Fatalf("ExecuteSync: %v", err)
	}

	select {
	case d := <-got:
		if d.MessageCount != 1 {
			t.Errorf("MessageCount = %d, want 1", d.MessageCount)
		}
		if d.InputTokens != 5 {
			t.Errorf("InputTokens = %d, want 5", d.InputTokens)
		}
		if d.OutputTokens != 7 {
			t.Errorf("OutputTokens = %d, want 7", d.OutputTokens)
		}
		if d.TotalCost != 0.01 {
			t.Errorf("TotalCost = %v, want 0.01", d.TotalCost)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pipeline.completed event")
	}
}
