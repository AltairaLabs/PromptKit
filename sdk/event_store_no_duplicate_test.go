package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// countingEventStore records how many events of each type it was asked to
// persist, by either route.
type countingEventStore struct {
	mu     sync.Mutex
	counts map[events.EventType]int
}

func newCountingEventStore() *countingEventStore {
	return &countingEventStore{counts: map[events.EventType]int{}}
}

func (c *countingEventStore) record(e *events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[e.Type]++
}

func (c *countingEventStore) Append(_ context.Context, e *events.Event) error {
	c.record(e)
	return nil
}
func (c *countingEventStore) OnEvent(e *events.Event) { c.record(e) }
func (c *countingEventStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (c *countingEventStore) QueryRaw(
	_ context.Context, _ *events.EventFilter,
) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (c *countingEventStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (c *countingEventStore) Close() error { return nil }

func (c *countingEventStore) count(t events.EventType) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[t]
}

// TestEventStore_NotSubscribedToMessageCreatedWhenRecording pins that an event
// store wired for recording does not also receive message.created off the bus.
//
// conversation.go sets RecordingStore to the SAME object initEventBus
// SubscribeAll's, so once message.created gained a bus producer every message
// would be persisted twice — once by RecordingStage with full binary, once off
// the bus with binary stripped. Replay would show each message twice.
func TestEventStore_NotSubscribedToMessageCreatedWhenRecording(t *testing.T) {
	store := newCountingEventStore()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	cfg := &config{
		eventBus:        bus,
		eventStore:      store,
		recordingConfig: &RecordingConfig{},
	}
	initEventBus(cfg)

	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	emitter.MessageCreatedCtx(context.Background(), &events.MessageCreatedData{
		Role: "user", Content: "hi", Index: 0,
	})
	emitter.PipelineStarted(1)

	require.Eventually(t, func() bool {
		return store.count(events.EventPipelineStarted) > 0
	}, 2*time.Second, 10*time.Millisecond, "the store should still receive other event types")

	assert.Zero(t, store.count(events.EventMessageCreated),
		"RecordingStage already writes message.created to this store; the bus copy "+
			"would duplicate it with a stripped payload")
}

// TestEventStore_ReceivesMessageCreatedWithoutRecording is the other half: a
// store supplied WITHOUT WithRecording has no RecordingStage writing to it, so
// the bus is its only source of messages and it must still get them.
func TestEventStore_ReceivesMessageCreatedWithoutRecording(t *testing.T) {
	store := newCountingEventStore()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	cfg := &config{eventBus: bus, eventStore: store}
	initEventBus(cfg)

	events.NewEmitter(bus, "run", "sess", "conv").
		MessageCreatedCtx(context.Background(), &events.MessageCreatedData{Role: "user", Content: "hi"})

	require.Eventually(t, func() bool {
		return store.count(events.EventMessageCreated) == 1
	}, 2*time.Second, 10*time.Millisecond,
		"without recording the bus is the store's only source of messages")
}
