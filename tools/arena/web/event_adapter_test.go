package web

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestAdapter_RegisterAndBroadcast(t *testing.T) {
	adapter := NewEventAdapter()

	// Register two clients
	ch1 := adapter.Register()
	ch2 := adapter.Register()

	// Create a fake event
	event := &events.Event{
		Type:        events.EventType("arena.run.started"),
		Timestamp:   time.Now(),
		ExecutionID: "run-1",
		Data: events.CustomEventData{
			EventName: "run_started",
			Data: map[string]interface{}{
				"scenario": "greeting",
				"provider": "openai",
				"region":   "default",
			},
		},
	}

	// Handle event (triggers broadcast)
	adapter.HandleEvent(event)

	// Both clients should receive the JSON message
	select {
	case msg := <-ch1:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal ch1: %v", err)
		}
		if got.Type != "arena.run.started" {
			t.Errorf("ch1 type = %q, want %q", got.Type, "arena.run.started")
		}
		if got.ExecutionID != "run-1" {
			t.Errorf("ch1 executionID = %q, want %q", got.ExecutionID, "run-1")
		}
	case <-time.After(time.Second):
		t.Fatal("ch1 timed out")
	}

	select {
	case msg := <-ch2:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal ch2: %v", err)
		}
		if got.Type != "arena.run.started" {
			t.Errorf("ch2 type = %q, want %q", got.Type, "arena.run.started")
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 timed out")
	}
}

func TestAdapter_Unregister(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()
	adapter.Unregister(ch)

	// After unregister, HandleEvent should not block or panic
	event := &events.Event{
		Type:      events.EventType("arena.run.started"),
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			EventName: "run_started",
			Data:      map[string]interface{}{},
		},
	}
	adapter.HandleEvent(event)

	select {
	case <-ch:
		t.Fatal("should not receive after unregister")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message
	}
}

func TestAdapter_MapProviderCallCompleted(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	event := &events.Event{
		Type:        events.EventProviderCallCompleted,
		Timestamp:   time.Now(),
		ExecutionID: "run-1",
		Data: &events.ProviderCallCompletedData{
			Provider: "openai",
			Model:    "gpt-4",
			Duration: 2500 * time.Millisecond,
			Cost:     0.0042,
		},
	}
	adapter.HandleEvent(event)

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != "provider.call.completed" {
			t.Errorf("type = %q, want %q", got.Type, "provider.call.completed")
		}
		data, ok := got.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("data is %T, want map", got.Data)
		}
		if data["provider"] != "openai" {
			t.Errorf("provider = %v, want openai", data["provider"])
		}
		if data["cost"] != 0.0042 {
			t.Errorf("cost = %v, want 0.0042", data["cost"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapMessageCreated(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	event := &events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      time.Now(),
		ConversationID: "conv-1",
		Data: events.MessageCreatedData{
			Role:    "assistant",
			Content: "Hello!",
			Index:   0,
		},
	}
	adapter.HandleEvent(event)

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != "message.created" {
			t.Errorf("type = %q, want %q", got.Type, "message.created")
		}
		if got.ConversationID != "conv-1" {
			t.Errorf("conversationId = %q, want %q", got.ConversationID, "conv-1")
		}
		data, ok := got.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("data is %T, want map", got.Data)
		}
		if data["role"] != "assistant" {
			t.Errorf("role = %v, want assistant", data["role"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_Subscribe(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	bus := events.NewEventBus()
	defer bus.Close()
	adapter.Subscribe(bus)

	// Publish an event through the real bus
	bus.Publish(&events.Event{
		Type:      events.EventType("arena.run.started"),
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			EventName: "run_started",
			Data:      map[string]interface{}{"scenario": "test"},
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != "arena.run.started" {
			t.Errorf("type = %q, want arena.run.started", got.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestAdapter_SubscribeNilBus(t *testing.T) {
	adapter := NewEventAdapter()
	// Should not panic
	adapter.Subscribe(nil)
}

func TestAdapter_MapProviderCallStarted(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventProviderCallStarted,
		Timestamp: time.Now(),
		Data: &events.ProviderCallStartedData{
			Provider: "anthropic",
			Model:    "claude-3",
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["provider"] != "anthropic" {
			t.Errorf("provider = %v, want anthropic", data["provider"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapProviderCallFailed(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventProviderCallFailed,
		Timestamp: time.Now(),
		Data: &events.ProviderCallFailedData{
			Provider: "openai",
			Model:    "gpt-4",
			Error:    fmt.Errorf("rate limit"),
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["error"] != "rate limit" {
			t.Errorf("error = %v, want 'rate limit'", data["error"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapMessageUpdated(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:           events.EventMessageUpdated,
		Timestamp:      time.Now(),
		ConversationID: "conv-2",
		Data: events.MessageUpdatedData{
			Index:        1,
			LatencyMs:    250,
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    0.001,
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ConversationID != "conv-2" {
			t.Errorf("conversationId = %q, want conv-2", got.ConversationID)
		}
		data := got.Data.(map[string]interface{})
		if data["latencyMs"] != float64(250) {
			t.Errorf("latencyMs = %v, want 250", data["latencyMs"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapConversationStarted(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventConversationStarted,
		Timestamp: time.Now(),
		Data: events.ConversationStartedData{
			SystemPrompt: "You are helpful.",
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["systemPrompt"] != "You are helpful." {
			t.Errorf("systemPrompt = %v", data["systemPrompt"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapMiddlewareCompleted(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventMiddlewareCompleted,
		Timestamp: time.Now(),
		Data: events.MiddlewareCompletedData{
			Name:     "guardrail",
			Duration: 100 * time.Millisecond,
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["name"] != "guardrail" {
			t.Errorf("name = %v, want guardrail", data["name"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapToolCallEvent(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	// Value receiver
	adapter.HandleEvent(&events.Event{
		Type:      events.EventToolCallStarted,
		Timestamp: time.Now(),
		Data: events.ToolCallEventData{
			ToolName: "memory__recall",
			CallID:   "call-1",
			Status:   "pending",
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["toolName"] != "memory__recall" {
			t.Errorf("toolName = %v, want memory__recall", data["toolName"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapToolCallEventPtr(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	// Pointer receiver
	adapter.HandleEvent(&events.Event{
		Type:      events.EventToolCallCompleted,
		Timestamp: time.Now(),
		Data: &events.ToolCallEventData{
			ToolName: "workflow__transition",
			CallID:   "call-2",
			Status:   "success",
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["toolName"] != "workflow__transition" {
			t.Errorf("toolName = %v, want workflow__transition", data["toolName"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapValidationEvent(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventValidationPassed,
		Timestamp: time.Now(),
		Data: events.ValidationEventData{
			ValidatorName: "output-guard",
			ValidatorType: "output",
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["validatorName"] != "output-guard" {
			t.Errorf("validatorName = %v, want output-guard", data["validatorName"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapValidationEventPtr(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	adapter.HandleEvent(&events.Event{
		Type:      events.EventValidationFailed,
		Timestamp: time.Now(),
		Data: &events.ValidationEventData{
			ValidatorName: "pii-filter",
			ValidatorType: "output",
			Error:         fmt.Errorf("PII detected"),
		},
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data := got.Data.(map[string]interface{})
		if data["validatorName"] != "pii-filter" {
			t.Errorf("validatorName = %v, want pii-filter", data["validatorName"])
		}
		if data["error"] != "PII detected" {
			t.Errorf("error = %v, want 'PII detected'", data["error"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_MapUnknownEventData(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	// Unknown/unhandled event data type — should still send the event with nil data
	adapter.HandleEvent(&events.Event{
		Type:      events.EventType("some.unknown.event"),
		Timestamp: time.Now(),
		Data:      nil,
	})

	select {
	case msg := <-ch:
		var got SSEEvent
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != "some.unknown.event" {
			t.Errorf("type = %q, want some.unknown.event", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAdapter_DropOnFullBuffer(t *testing.T) {
	adapter := NewEventAdapter()
	ch := adapter.Register()

	event := &events.Event{
		Type:      events.EventType("arena.run.started"),
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			EventName: "test",
			Data:      map[string]interface{}{},
		},
	}

	// Fill the buffer (clientBufferSize = 256)
	for i := 0; i < clientBufferSize+10; i++ {
		adapter.HandleEvent(event)
	}

	// Should not block or panic — extra events are dropped
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != clientBufferSize {
		t.Errorf("received %d events, want %d (buffer size)", count, clientBufferSize)
	}
}
