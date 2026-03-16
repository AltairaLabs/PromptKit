package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// 1.3 — Event emission end-to-end (detailed)
// ---------------------------------------------------------------------------

func TestEvents_SequenceOrder(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Wait for all events we're about to assert on.
	expectedTypes := []events.EventType{
		events.EventPipelineStarted,
		events.EventPipelineCompleted,
		events.EventProviderCallStarted,
		events.EventProviderCallCompleted,
	}
	require.True(t, ec.waitForEvents(expectedTypes, 2*time.Second), "not all expected events arrived")

	seq := ec.typeSequence()

	// pipeline.started must come before pipeline.completed
	startIdx := indexOf(seq, events.EventPipelineStarted)
	completeIdx := indexOf(seq, events.EventPipelineCompleted)
	assert.Less(t, startIdx, completeIdx, "pipeline.started should precede pipeline.completed")

	// provider.call.started must come before provider.call.completed
	provStartIdx := indexOf(seq, events.EventProviderCallStarted)
	provCompleteIdx := indexOf(seq, events.EventProviderCallCompleted)
	assert.Less(t, provStartIdx, provCompleteIdx, "provider.call.started should precede provider.call.completed")

	// Provider events should be nested within pipeline events.
	assert.Less(t, startIdx, provStartIdx, "provider.call should be after pipeline.started")
	assert.Less(t, provCompleteIdx, completeIdx, "provider.call should be before pipeline.completed")
}

func TestEvents_FieldPopulation(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Hi")
	require.NoError(t, err)

	ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second)

	for _, e := range ec.all() {
		assert.False(t, e.Timestamp.IsZero(), "event %s should have non-zero timestamp", e.Type)
		assert.NotEmpty(t, e.ConversationID, "event %s should have ConversationID", e.Type)
	}
}

func TestEvents_DataPointerTypes(t *testing.T) {
	// This test verifies that event Data fields use pointer types so that
	// type assertions in MetricContext.OnEvent succeed. This is the test
	// that would have caught the pointer/value bug (#709).
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Test pointer types")
	require.NoError(t, err)

	// Wait for ALL event types we're about to assert on — not just PipelineCompleted.
	// Provider events are emitted asynchronously and may arrive after PipelineCompleted.
	require.True(t, ec.waitForEvents([]events.EventType{
		events.EventPipelineCompleted,
		events.EventProviderCallCompleted,
		events.EventPipelineStarted,
	}, 2*time.Second), "not all expected events arrived")

	// Verify PipelineCompleted data is a pointer type (not value).
	pipeCompleted := ec.ofType(events.EventPipelineCompleted)
	require.Len(t, pipeCompleted, 1)
	_, ok := pipeCompleted[0].Data.(*events.PipelineCompletedData)
	assert.True(t, ok, "pipeline.completed Data should be *PipelineCompletedData, got %T", pipeCompleted[0].Data)

	// Verify ProviderCallCompleted data is a pointer type.
	provCompleted := ec.ofType(events.EventProviderCallCompleted)
	require.NotEmpty(t, provCompleted)
	_, ok = provCompleted[0].Data.(*events.ProviderCallCompletedData)
	assert.True(t, ok, "provider.call.completed Data should be *ProviderCallCompletedData, got %T", provCompleted[0].Data)

	// Verify PipelineStarted data is a pointer type.
	pipeStarted := ec.ofType(events.EventPipelineStarted)
	require.NotEmpty(t, pipeStarted)
	_, ok = pipeStarted[0].Data.(*events.PipelineStartedData)
	assert.True(t, ok, "pipeline.started Data should be *PipelineStartedData, got %T", pipeStarted[0].Data)
}

func TestEvents_PipelineCompletedHasDuration(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Check metrics in event data")
	require.NoError(t, err)

	ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second)

	pipeCompleted := ec.ofType(events.EventPipelineCompleted)
	require.Len(t, pipeCompleted, 1)

	data := pipeCompleted[0].Data.(*events.PipelineCompletedData)
	assert.Greater(t, data.Duration, time.Duration(0), "pipeline duration should be > 0")

	// NOTE: InputTokens/OutputTokens on PipelineCompletedData are currently
	// not populated by the stage-based pipeline (hardcoded to 0). Token counts
	// are available per-call via ProviderCallCompletedData. Tracked as a future
	// enhancement to aggregate costs at the pipeline level.
}

func TestEvents_ProviderCallCompletedHasDetails(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Check provider details")
	require.NoError(t, err)

	// Wait for PipelineCompleted (the last event emitted) to avoid a race
	// between bus.Close() in cleanup and the pipeline goroutine still publishing.
	ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second)

	provCompleted := ec.ofType(events.EventProviderCallCompleted)
	require.NotEmpty(t, provCompleted)

	data := provCompleted[0].Data.(*events.ProviderCallCompletedData)
	assert.NotEmpty(t, data.Provider, "provider name should be set")
	assert.NotEmpty(t, data.Model, "model name should be set")
	assert.Greater(t, data.Duration, time.Duration(0), "duration should be > 0")
	assert.Greater(t, data.InputTokens, 0, "input tokens should be > 0")
	assert.Greater(t, data.OutputTokens, 0, "output tokens should be > 0")
}

// indexOf returns the first index of eventType in the sequence, or -1.
func indexOf(seq []events.EventType, et events.EventType) int {
	for i, e := range seq {
		if e == et {
			return i
		}
	}
	return -1
}
