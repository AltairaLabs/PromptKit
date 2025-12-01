package tui

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestEventAdapterHandlesRunLifecycle(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	start := &events.Event{
		Type:      events.EventType("arena.run.started"),
		RunID:     "run-1",
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			Data: map[string]interface{}{
				"scenario": "sc-1",
				"provider": "prov-1",
				"region":   "us-east",
			},
		},
	}
	complete := &events.Event{
		Type:      events.EventType("arena.run.completed"),
		RunID:     "run-1",
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			Data: map[string]interface{}{
				"duration": time.Second,
				"cost":     0.1,
			},
		},
	}

	adapter.HandleEvent(start)
	adapter.HandleEvent(complete)

	if len(model.activeRuns) == 0 {
		t.Fatalf("expected active run to be created from events")
	}
	if model.completedCount == 0 {
		t.Fatalf("expected run completion to increment completed count")
	}
}

func TestEventAdapterLogsProviderEvents(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	adapter.HandleEvent(&events.Event{
		Type:      events.EventProviderCallStarted,
		Timestamp: time.Now(),
		Data: events.ProviderCallStartedData{
			Provider: "prov",
			Model:    "model",
		},
	})

	if len(model.logs) == 0 {
		t.Fatalf("expected logs to receive provider event")
	}
}
