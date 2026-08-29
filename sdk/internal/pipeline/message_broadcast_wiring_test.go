package pipeline

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// collectBroadcast runs one turn through a built pipeline and returns the
// message.created payloads a bus subscriber received, ordered by Index. The
// bus dispatches through a worker pool, so arrival order is not publish order.
func collectBroadcast(t *testing.T, cfg *Config, bus *events.EventBus) []*events.MessageCreatedData {
	t.Helper()

	var mu sync.Mutex
	var got []*events.MessageCreatedData
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	})

	pipe, err := Build(cfg)
	require.NoError(t, err)

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart("Hello!")
	_, err = pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &userMsg})
	require.NoError(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	out := make([]*events.MessageCreatedData, len(got))
	copy(out, got)
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// TestMessageBroadcast_ReachesTheBusWithoutStoreOrRecording is the whole point
// of the live route.
//
// Before this, message.created was produced only by RecordingStage, which needs
// both WithRecording and an EventStore, so a consumer that supplied a bus and
// subscribed to message.created received nothing and had no supported
// alternative. Note this config sets NO StateStore and NO RecordingStore.
func TestMessageBroadcast_ReachesTheBusWithoutStoreOrRecording(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	cfg := &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       mock.NewProvider("mock-broadcast", "mock-model", false),
		EventEmitter:   events.NewEmitter(bus, "run", "sess", "conv"),
	}
	require.Nil(t, cfg.StateStore, "test premise: no state store")
	require.Nil(t, cfg.RecordingStore, "test premise: no recording store")

	got := collectBroadcast(t, cfg, bus)

	require.NotEmpty(t, got, "message.created never reached the bus")

	var sawUser, sawAssistant bool
	for _, d := range got {
		switch d.Role {
		case "user":
			sawUser = true
		case "assistant":
			sawAssistant = true
		}
	}
	assert.True(t, sawUser, "the user message should be broadcast")
	assert.True(t, sawAssistant, "the assistant reply should be broadcast")
}

// TestMessageBroadcast_NotWiredWithoutAnEmitter pins the gate. Without a bus
// there is no consumer, so the stage should not be in the pipeline at all.
func TestMessageBroadcast_NotWiredWithoutAnEmitter(t *testing.T) {
	cfg := &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       mock.NewProvider("mock-no-bus", "mock-model", false),
	}

	stages, err := collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)

	for _, s := range stages {
		assert.NotEqual(t, "message_broadcast", s.Name(),
			"the broadcast stage must not be added when no emitter is configured")
	}
}

// TestMessageBroadcast_WiredWhenEmitterPresent is the other half, and the
// mutation guard for the builder line itself.
func TestMessageBroadcast_WiredWhenEmitterPresent(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	cfg := &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       mock.NewProvider("mock-bus", "mock-model", false),
		EventEmitter:   events.NewEmitter(bus, "run", "sess", "conv"),
	}

	stages, err := collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)

	var found bool
	for _, s := range stages {
		if s.Name() == "message_broadcast" {
			found = true
		}
	}
	assert.True(t, found, "the broadcast stage must be added when an emitter is configured")
}
