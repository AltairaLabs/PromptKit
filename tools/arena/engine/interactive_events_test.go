package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/artifacts"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// TestEngine_SimpleAccessors covers option setters and simple accessors on
// Engine that have no dedicated test home.
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

// messageEventCollector subscribes to message.created events on the (async)
// event bus and accumulates their payloads under a mutex. The bus delivers on
// worker goroutines, so callers must waitFor the expected count before reading
// snapshot — reading the slice directly would race delivery.
type messageEventCollector struct {
	mu  sync.Mutex
	evs []events.MessageCreatedData
}

func collectMessageEvents(bus events.Bus) *messageEventCollector {
	c := &messageEventCollector{}
	bus.Subscribe(events.EventMessageCreated, func(ev *events.Event) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch d := ev.Data.(type) {
		case *events.MessageCreatedData:
			if d != nil {
				c.evs = append(c.evs, *d)
			}
		case events.MessageCreatedData:
			c.evs = append(c.evs, d)
		}
	})
	return c
}

func (c *messageEventCollector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.evs)
}

// snapshot returns a copy of the events collected so far, taken under lock.
func (c *messageEventCollector) snapshot() []events.MessageCreatedData {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.MessageCreatedData, len(c.evs))
	copy(out, c.evs)
	return out
}

// waitFor blocks until at least n events have been delivered or the timeout
// elapses, accommodating the bus's asynchronous worker delivery.
func (c *messageEventCollector) waitFor(n int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for c.len() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func TestInteractiveSession_EmitsLiveMessagesLikeARun(t *testing.T) {
	eng := newFixtureEngine(t)
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("mock: %v", err)
	}
	bus := events.NewEventBus()
	eng.SetEventBus(bus, WithMessageEvents())
	got := collectMessageEvents(bus)

	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock", TaskType: "basic", Variables: map[string]string{"company": "Acme"},
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	ctx := context.Background()

	drain := func(text string) {
		ch, err := sess.SendUserMessage(ctx, text)
		if err != nil {
			t.Fatalf("send %q: %v", text, err)
		}
		for range ch { //nolint:revive
		}
	}
	drain("ONE")
	drain("TWO")

	// Authoritative transcript from the store.
	msgs, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}

	// The bus delivers asynchronously on worker goroutines; wait until every
	// persisted message has had its event delivered before asserting.
	got.waitFor(len(msgs), 2*time.Second)
	events := got.snapshot()

	// EVERY persisted message — system included — must have been emitted exactly
	// once at its store index. Asserting system explicitly guards the index
	// derivation in the save stage (Finding 1): the system message lives at
	// transcript index 0 and the live offset must keep every later message
	// aligned with the persisted transcript. No message emitted more than once
	// (no re-fire of prior turns).
	type key struct {
		role    string
		content string
		index   int
	}
	seen := map[key]int{}
	for _, e := range events {
		seen[key{e.Role, e.Content, e.Index}]++
	}
	for i := range msgs {
		k := key{msgs[i].Role, msgs[i].GetContent(), i}
		if seen[k] != 1 {
			t.Fatalf("message #%d (%s) emitted %d times, want exactly 1; events=%+v",
				i, msgs[i].Role, seen[k], events)
		}
	}

	// Exactly one system event, and it must sit at index 0. This is the direct
	// regression guard for the index arithmetic: a stale prevLen-based offset
	// would either drop the system event or fire it at the wrong index.
	systemCount := 0
	for _, e := range events {
		if e.Role != "system" {
			continue
		}
		systemCount++
		if e.Index != 0 {
			t.Fatalf("system event at index %d, want 0; events=%+v", e.Index, events)
		}
	}
	if systemCount != 1 {
		t.Fatalf("got %d system events, want exactly 1; events=%+v", systemCount, events)
	}

	// And no event carries an index outside the transcript (no stale/duplicate indices).
	for _, e := range events {
		if e.Index < 0 || e.Index >= len(msgs) {
			t.Fatalf("event index %d out of range [0,%d): %+v", e.Index, len(msgs), e)
		}
	}
}

func TestInteractiveSession_NoBusNoEmission(t *testing.T) {
	eng := newFixtureEngine(t)
	_ = eng.EnableMockProviderMode("")
	// No SetEventBus → engine.eventBus nil → no emitter → no panic, no events.
	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock", TaskType: "basic", Variables: map[string]string{"company": "Acme"},
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	ch, err := sess.SendUserMessage(context.Background(), "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for range ch { //nolint:revive
	}
	// Reaching here without panic + a persisted transcript is the assertion.
	msgs, _ := sess.Messages(context.Background())
	if len(msgs) == 0 {
		t.Fatal("expected persisted messages")
	}
}
