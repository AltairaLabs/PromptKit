package events

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEmitterPublishesSharedContext(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-1", "session-1", "conv-1")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventPipelineStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.PipelineStarted(3)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for pipeline started event")
	}

	if got.RunID != "run-1" || got.SessionID != "session-1" || got.ConversationID != "conv-1" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(PipelineStartedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.MiddlewareCount != 3 {
		t.Fatalf("unexpected middleware count: %d", data.MiddlewareCount)
	}
}

func TestEmitterPublishesVariousEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-2", "session-2", "conv-2")

	var seen []EventType
	var mu sync.Mutex
	var wg sync.WaitGroup

	bus.SubscribeAll(func(e *Event) {
		mu.Lock()
		seen = append(seen, e.Type)
		mu.Unlock()
		wg.Done()
	})

	tests := []func(){
		func() { emitter.PipelineCompleted(time.Second, 1.23, 10, 20, 1) },
		func() { emitter.PipelineFailed(errors.New("boom"), time.Second) },
		func() { emitter.MiddlewareStarted("mw", 0) },
		func() { emitter.MiddlewareCompleted("mw", 0, time.Millisecond) },
		func() { emitter.MiddlewareFailed("mw", 0, errors.New("oops"), time.Millisecond) },
		func() { emitter.ProviderCallStarted("provider", "model", 2, 1) },
		func() {
			emitter.ProviderCallCompleted(&ProviderCallCompletedData{
				Provider:      "provider",
				Model:         "model",
				Duration:      time.Millisecond,
				InputTokens:   5,
				OutputTokens:  6,
				CachedTokens:  0,
				Cost:          0.1,
				FinishReason:  "stop",
				ToolCallCount: 0,
			})
		},
		func() { emitter.ProviderCallFailed("provider", "model", errors.New("fail"), time.Millisecond) },
		func() { emitter.ToolCallStarted("tool", "call", map[string]interface{}{"k": "v"}) },
		func() { emitter.ToolCallCompleted("tool", "call", time.Millisecond, "success") },
		func() { emitter.ToolCallFailed("tool", "call", errors.New("fail"), time.Millisecond) },
		func() { emitter.ValidationStarted("validator", "input") },
		func() { emitter.ValidationPassed("validator", "input", time.Millisecond) },
		func() {
			emitter.ValidationFailed("validator", "input", errors.New("fail"), time.Millisecond, []string{"x"})
		},
		func() { emitter.ContextBuilt(1, 2, 3, false) },
		func() { emitter.TokenBudgetExceeded(5, 3, 2) },
		func() { emitter.StateLoaded("conv", 1) },
		func() { emitter.StateSaved("conv", 1) },
		func() { emitter.StreamInterrupted("reason") },
		func() {
			emitter.EmitCustom(EventType("middleware.custom.event"), "mw", "custom", map[string]interface{}{"a": 1}, "msg")
		},
		func() { emitter.WorkflowTransitioned("s1", "s2", "Next", "p2") },
		func() { emitter.WorkflowCompleted("done", 1) },
	}

	wg.Add(len(tests))
	for _, fn := range tests {
		fn()
	}

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatalf("timed out waiting for %d events, saw %d", len(tests), len(seen))
	}

	if len(seen) != len(tests) {
		t.Fatalf("expected %d events, got %d", len(tests), len(seen))
	}
}

func TestEmitterHandlesNilBus(t *testing.T) {
	t.Parallel()

	emitter := NewEmitter(nil, "run", "session", "conv")
	// Should not panic even without a bus.
	emitter.PipelineStarted(1)
}

func TestEmitterHandlesNilEmitter(t *testing.T) {
	t.Parallel()

	var emitter *Emitter
	// Should not panic when emitter is nil
	emitter.PipelineStarted(1)
	emitter.MessageCreated("user", "hello", 0, nil, nil)
	emitter.MessageUpdated(0, 100, 10, 20, 0.001)
	emitter.ConversationStarted("system prompt")
	emitter.AudioInput(&AudioInputData{Actor: "user"})
	emitter.AudioOutput(&AudioOutputData{GeneratedFrom: "model"})
}

func TestEmitter_MessageCreated(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-mc", "session-mc", "conv-mc")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMessageCreated, func(e *Event) {
		got = e
		wg.Done()
	})

	toolCalls := []MessageToolCall{
		{Name: "test_tool", Args: `{"key":"value"}`},
	}
	emitter.MessageCreated("assistant", "Hello!", 1, toolCalls, nil)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event")
	}

	if got.RunID != "run-mc" || got.SessionID != "session-mc" || got.ConversationID != "conv-mc" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(MessageCreatedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.Role != "assistant" || data.Content != "Hello!" || data.Index != 1 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if len(data.ToolCalls) != 1 || data.ToolCalls[0].Name != "test_tool" {
		t.Fatalf("unexpected tool calls: %+v", data.ToolCalls)
	}
}

func TestEmitter_MessageCreated_WithToolResult(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-tr", "session-tr", "conv-tr")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMessageCreated, func(e *Event) {
		got = e
		wg.Done()
	})

	toolResult := &MessageToolResult{
		Name:    "weather_tool",
		Content: `{"temp": 72}`,
	}
	emitter.MessageCreated("tool", "", 2, nil, toolResult)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event with tool result")
	}

	data, ok := got.Data.(MessageCreatedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.Role != "tool" || data.Index != 2 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if data.ToolResult == nil || data.ToolResult.Name != "weather_tool" {
		t.Fatalf("unexpected tool result: %+v", data.ToolResult)
	}
}

func TestEmitter_MessageUpdated(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-mu", "session-mu", "conv-mu")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMessageUpdated, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.MessageUpdated(3, 150, 100, 50, 0.0025)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.updated event")
	}

	if got.RunID != "run-mu" {
		t.Fatalf("unexpected run ID: %s", got.RunID)
	}

	data, ok := got.Data.(MessageUpdatedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.Index != 3 || data.LatencyMs != 150 || data.InputTokens != 100 || data.OutputTokens != 50 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if data.TotalCost != 0.0025 {
		t.Fatalf("unexpected total cost: %f", data.TotalCost)
	}
}

func TestEmitter_ConversationStarted(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-cs", "session-cs", "conv-cs")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventConversationStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.ConversationStarted("You are a helpful AI assistant.")

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for conversation.started event")
	}

	if got.RunID != "run-cs" || got.SessionID != "session-cs" || got.ConversationID != "conv-cs" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(ConversationStartedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.SystemPrompt != "You are a helpful AI assistant." {
		t.Fatalf("unexpected system prompt: %s", data.SystemPrompt)
	}
}

func TestEmitter_ProviderCallCompleted_NilData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-pcc", "session-pcc", "conv-pcc")

	// Should not panic when data is nil
	emitter.ProviderCallCompleted(nil)
}

func TestEmitter_AudioInput(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ai", "session-ai", "conv-ai")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventAudioInput, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.AudioInput(&AudioInputData{
		Actor:      "user",
		ChunkIndex: 0,
		Payload: BinaryPayload{
			InlineData: []byte{0x01, 0x02, 0x03},
			MIMEType:   "audio/pcm",
			Size:       3,
		},
		Metadata: AudioMetadata{
			SampleRate: 16000,
			Channels:   1,
			Encoding:   "pcm_linear16",
			DurationMs: 100,
		},
		IsFinal: false,
	})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for audio.input event")
	}

	if got.RunID != "run-ai" || got.SessionID != "session-ai" || got.ConversationID != "conv-ai" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(*AudioInputData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.Actor != "user" || data.ChunkIndex != 0 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if data.Metadata.SampleRate != 16000 || data.Metadata.Channels != 1 {
		t.Fatalf("unexpected metadata: %+v", data.Metadata)
	}
}

func TestEmitter_AudioInput_NilData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ain", "session-ain", "conv-ain")

	// Should not panic when data is nil
	emitter.AudioInput(nil)
}

func TestEmitter_AudioOutput(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ao", "session-ao", "conv-ao")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventAudioOutput, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.AudioOutput(&AudioOutputData{
		ChunkIndex: 5,
		Payload: BinaryPayload{
			InlineData: []byte{0xAA, 0xBB, 0xCC, 0xDD},
			MIMEType:   "audio/pcm",
			Size:       4,
		},
		Metadata: AudioMetadata{
			SampleRate: 24000,
			Channels:   1,
			Encoding:   "pcm_linear16",
			DurationMs: 50,
		},
		GeneratedFrom: "model",
	})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for audio.output event")
	}

	if got.RunID != "run-ao" || got.SessionID != "session-ao" || got.ConversationID != "conv-ao" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(*AudioOutputData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.ChunkIndex != 5 || data.GeneratedFrom != "model" {
		t.Fatalf("unexpected data: %+v", data)
	}
	if data.Metadata.SampleRate != 24000 || data.Metadata.DurationMs != 50 {
		t.Fatalf("unexpected metadata: %+v", data.Metadata)
	}
}

func TestEmitter_AudioOutput_NilData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-aon", "session-aon", "conv-aon")

	// Should not panic when data is nil
	emitter.AudioOutput(nil)
}

func TestEmitter_WorkflowTransitioned(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-wt", "session-wt", "conv-wt")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventWorkflowTransitioned, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.WorkflowTransitioned("intake", "processing", "InfoComplete", "process")

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for workflow.transitioned event")
	}

	data, ok := got.Data.(*WorkflowTransitionedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.FromState != "intake" || data.ToState != "processing" {
		t.Fatalf("unexpected states: from=%s to=%s", data.FromState, data.ToState)
	}
	if data.Event != "InfoComplete" || data.PromptTask != "process" {
		t.Fatalf("unexpected event/task: event=%s task=%s", data.Event, data.PromptTask)
	}
}

func TestEmitter_WorkflowCompleted(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-wc", "session-wc", "conv-wc")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventWorkflowCompleted, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.WorkflowCompleted("done", 3)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for workflow.completed event")
	}

	data, ok := got.Data.(*WorkflowCompletedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.FinalState != "done" || data.TransitionCount != 3 {
		t.Fatalf("unexpected data: state=%s count=%d", data.FinalState, data.TransitionCount)
	}
}
