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

func TestEngineEmitsRunStartedEvent(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	eng := &Engine{eventBus: bus}

	var mu sync.Mutex
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventType("arena.run.started"), func(e *events.Event) {
		mu.Lock()
		receivedEvent = e
		mu.Unlock()
		wg.Done()
	})

	combo := RunCombination{
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Region:     "us-east",
	}
	runID := generateRunID(combo)
	emitter := eng.createRunEmitter(runID, combo)

	// Wait for async event delivery
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for arena.run.started event")
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedEvent == nil {
		t.Fatal("no event received")
	}
	if receivedEvent.Type != events.EventType("arena.run.started") {
		t.Fatalf("expected arena.run.started, got %s", receivedEvent.Type)
	}
	if receivedEvent.RunID != runID {
		t.Fatalf("expected RunID %s, got %s", runID, receivedEvent.RunID)
	}

	// Verify event data
	if emitter != nil {
		data, ok := receivedEvent.Data.(events.CustomEventData)
		if !ok {
			t.Fatal("expected CustomEventData")
		}
		if data.Data["scenario"] != "test-scenario" {
			t.Errorf("expected scenario=test-scenario, got %v", data.Data["scenario"])
		}
		if data.Data["provider"] != "test-provider" {
			t.Errorf("expected provider=test-provider, got %v", data.Data["provider"])
		}
		if data.Data["region"] != "us-east" {
			t.Errorf("expected region=us-east, got %v", data.Data["region"])
		}
	}
}

func TestEngineEmitsRunCompletedEvent(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "", "test-run")

	var mu sync.Mutex
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventType("arena.run.completed"), func(e *events.Event) {
		mu.Lock()
		receivedEvent = e
		mu.Unlock()
		wg.Done()
	})

	eng := &Engine{}
	result := &ConversationResult{Error: ""} // No error means completed
	duration := 2 * time.Second
	cost := 0.05

	eng.notifyRunCompletion(emitter, result, "test-run", duration, cost)

	// Wait for async event delivery
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for arena.run.completed event")
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedEvent == nil {
		t.Fatal("no event received")
	}
	if receivedEvent.Type != events.EventType("arena.run.completed") {
		t.Fatalf("expected arena.run.completed, got %s", receivedEvent.Type)
	}

	// Verify event data
	data, ok := receivedEvent.Data.(events.CustomEventData)
	if !ok {
		t.Fatal("expected CustomEventData")
	}
	if dur, ok := data.Data["duration"].(time.Duration); !ok || dur != duration {
		t.Errorf("expected duration=%v, got %v", duration, data.Data["duration"])
	}
	if c, ok := data.Data["cost"].(float64); !ok || c != cost {
		t.Errorf("expected cost=%v, got %v", cost, data.Data["cost"])
	}
}

func TestEngineEmitsRunFailedEvent(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "", "test-run")

	var mu sync.Mutex
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventType("arena.run.failed"), func(e *events.Event) {
		mu.Lock()
		receivedEvent = e
		mu.Unlock()
		wg.Done()
	})

	eng := &Engine{}
	result := &ConversationResult{Error: "something went wrong"}
	duration := 1 * time.Second
	cost := 0.01

	eng.notifyRunCompletion(emitter, result, "test-run", duration, cost)

	// Wait for async event delivery
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for arena.run.failed event")
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedEvent == nil {
		t.Fatal("no event received")
	}
	if receivedEvent.Type != events.EventType("arena.run.failed") {
		t.Fatalf("expected arena.run.failed, got %s", receivedEvent.Type)
	}

	// Verify event data contains error
	data, ok := receivedEvent.Data.(events.CustomEventData)
	if !ok {
		t.Fatal("expected CustomEventData")
	}
	if errMsg, ok := data.Data["error"].(string); !ok || errMsg != "something went wrong" {
		t.Errorf("expected error='something went wrong', got %v", data.Data["error"])
	}
}
