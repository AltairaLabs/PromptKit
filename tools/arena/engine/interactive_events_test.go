package engine

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/artifacts"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// TestBusPublishingStore_InterfaceMethods covers the no-op stub methods of
// busPublishingStore that satisfy the events.EventStore interface but never
// do meaningful work. This test exists purely to maintain coverage — the real
// production path is exercised by TestInteractiveSession_SendUserMessage_EmitsMessageEvents.
func TestBusPublishingStore_InterfaceMethods(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()

	store := &busPublishingStore{bus: bus}

	// Append publishes to bus — verify no error.
	if err := store.Append(context.Background(), &events.Event{Type: events.EventMessageCreated}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// OnEvent publishes to bus — no-op assertion; just must not panic.
	store.OnEvent(&events.Event{Type: events.EventMessageCreated})

	// Query returns empty.
	evts, err := store.Query(context.Background(), nil)
	if err != nil || len(evts) != 0 {
		t.Fatalf("Query: %v events, %v", len(evts), err)
	}

	// QueryRaw returns empty.
	raw, err := store.QueryRaw(context.Background(), nil)
	if err != nil || len(raw) != 0 {
		t.Fatalf("QueryRaw: %v events, %v", len(raw), err)
	}

	// Stream returns a closed channel.
	ch, err := store.Stream(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_, ok := <-ch
	if ok {
		t.Fatal("Stream channel should be closed immediately")
	}

	// Close is a no-op.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Nil bus should not panic.
	nilStore := &busPublishingStore{bus: nil}
	_ = nilStore.Append(context.Background(), &events.Event{})
	nilStore.OnEvent(&events.Event{})
}

// TestEngine_SimpleAccessors covers option setters and simple accessors on
// Engine that have no dedicated test home. These are pre-existing functions in
// engine.go that fell below the 80% coverage threshold after engine.go was
// touched by the busPublishingStore addition.
func TestEngine_SimpleAccessors(t *testing.T) {
	config.SchemaValidationDisabled.Store(true)

	cfg := filepath.Join("testdata", "interactive", "config.arena.yaml")
	eng, err := NewEngineFromConfigFile(filepath.Clean(cfg))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// WithOutputDir — simple setter.
	eng.WithOutputDir("/tmp/test-output")

	// WithArtifactStore — simple setter (nil is valid to test the path).
	eng.WithArtifactStore(artifacts.Store(nil))

	// AudioMonitorEnabled + AudioMonitorOptions — both false/zero before enable.
	if eng.AudioMonitorEnabled() {
		t.Error("AudioMonitorEnabled should be false before EnableAudioMonitor")
	}
	opts := eng.AudioMonitorOptions()
	if opts != (arenaaudio.Options{}) {
		t.Errorf("AudioMonitorOptions should be zero before EnableAudioMonitor, got %v", opts)
	}
}

func TestInteractiveSession_SendUserMessage_EmitsMessageEvents(t *testing.T) {
	// Disable schema validation for test fixtures
	config.SchemaValidationDisabled.Store(true)

	cfg := filepath.Join("testdata", "interactive", "config.arena.yaml")
	eng, err := NewEngineFromConfigFile(filepath.Clean(cfg))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}

	bus := events.NewEventBus()
	eng.SetEventBus(bus, WithMessageEvents())
	defer bus.Close()

	var count atomic.Int32
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		count.Add(1)
	})

	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock",
		TaskType:   "basic",
		Variables:  map[string]string{"company": "TestCo"},
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}

	ch, err := sess.SendUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	// Drain stream
	for range ch {
	}

	// Give event bus goroutines time to deliver
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count.Load() < 1 {
		t.Fatalf("expected ≥1 message.created event, got %d", count.Load())
	}
}
