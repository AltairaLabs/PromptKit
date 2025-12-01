package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestConversationExecutorEmitsTurnEventsToBus(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "", "conv-1")

	var mu sync.Mutex
	var seen []events.EventType
	var wg sync.WaitGroup
	wg.Add(2)

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		seen = append(seen, e.Type)
		mu.Unlock()
		wg.Done()
	})

	ce := &DefaultConversationExecutor{}
	ce.notifyTurnStarted(emitter, 0, "user", "scenario-1")
	ce.notifyTurnCompleted(emitter, 0, "user", "scenario-1", nil)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for events, saw %v", seen)
	}

	if len(seen) != 2 {
		t.Fatalf("expected 2 events, got %d", len(seen))
	}
}

func TestConversationExecutorEmitsFailureEvent(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-2", "", "conv-2")

	var got events.EventType
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventType("arena.turn.failed"), func(e *events.Event) {
		got = e.Type
		wg.Done()
	})

	ce := &DefaultConversationExecutor{}
	ce.notifyTurnCompleted(emitter, 0, "user", "scenario-2", assertErr{})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failure event")
	}

	if got != events.EventType("arena.turn.failed") {
		t.Fatalf("expected failure event, got %s", got)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "failed" }

func TestEngineSetEventBus(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	e := &Engine{}

	e.SetEventBus(bus)
	if e.eventBus != bus {
		t.Fatalf("expected eventBus to be set")
	}
}

func TestBuildTurnRequestSetsEventFields(t *testing.T) {
	t.Parallel()

	ce := &DefaultConversationExecutor{}
	scenario := &config.Scenario{
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "hi"},
		},
	}
	req := ConversationRequest{
		Scenario:       scenario,
		Config:         &config.Config{},
		Region:         "us-east",
		RunID:          "run-evt",
		ConversationID: "conv-evt",
		EventBus:       events.NewEventBus(),
	}

	turnReq := ce.buildTurnRequest(req, scenario.Turns[0])

	if turnReq.EventBus == nil {
		t.Fatalf("expected event bus on turn request")
	}
	if turnReq.RunID != "run-evt" {
		t.Fatalf("expected run id propagated, got %s", turnReq.RunID)
	}
	if turnReq.ConversationID != "conv-evt" {
		t.Fatalf("expected conversation id propagated, got %s", turnReq.ConversationID)
	}
}
