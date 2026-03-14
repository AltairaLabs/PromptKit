package events

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
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

	data, ok := got.Data.(*PipelineStartedData)
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
		func() { emitter.ProviderCallStarted("provider", "model", 2, 1, nil) },
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
		func() { emitter.ProviderCallFailed("provider", "model", errors.New("fail"), time.Millisecond, nil) },
		func() { emitter.ToolCallStarted("tool", "call", map[string]interface{}{"k": "v"}, nil) },
		func() {
			emitter.ToolCallCompleted("tool", "call", time.Millisecond, "success", []types.ContentPart{types.NewTextPart("tool result")}, nil)
		},
		func() { emitter.ToolCallFailed("tool", "call", errors.New("fail"), time.Millisecond, nil) },
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
		func() {
			emitter.ClientToolRequest(&ClientToolRequestData{
				CallID: "call-1", ToolName: "test_tool",
			})
		},
		func() {
			emitter.GuardrailResult(&ValidationEventData{
				ValidatorName: "banned_words",
				ValidatorType: "banned_words",
				Enforced:      true,
				Violations:    []string{"bad word found"},
			})
		},
		func() {
			emitter.GuardrailResult(&ValidationEventData{
				ValidatorName: "length",
				ValidatorType: "length",
			})
		},
		func() {
			emitter.ClientToolResolved(&ClientToolResolvedData{
				CallID: "call-1", Status: "fulfilled",
			})
		},
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
	emitter.MessageCreated("user", "hello", 0, nil, nil, nil)
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
	emitter.MessageCreated("assistant", "Hello!", 1, nil, toolCalls, nil)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event")
	}

	if got.RunID != "run-mc" || got.SessionID != "session-mc" || got.ConversationID != "conv-mc" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(*MessageCreatedData)
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
		Name:  "weather_tool",
		Parts: []types.ContentPart{types.NewTextPart(`{"temp": 72}`)},
	}
	emitter.MessageCreated("tool", "", 2, nil, nil, toolResult)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event with tool result")
	}

	data, ok := got.Data.(*MessageCreatedData)
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

func TestEmitter_MessageCreated_WithParts(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-mp", "session-mp", "conv-mp")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMessageCreated, func(e *Event) {
		got = e
		wg.Done()
	})

	textVal := "Hello with image"
	imgURL := "https://example.com/img.png"
	parts := []types.ContentPart{
		{Type: "text", Text: &textVal},
		{Type: "image", Media: &types.MediaContent{MIMEType: "image/png", URL: &imgURL}},
	}
	emitter.MessageCreated("user", "Hello with image", 0, parts, nil, nil)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event with parts")
	}

	data, ok := got.Data.(*MessageCreatedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if len(data.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(data.Parts))
	}
	if data.Parts[0].Type != "text" || data.Parts[1].Type != "image" {
		t.Fatalf("unexpected part types: %v, %v", data.Parts[0].Type, data.Parts[1].Type)
	}
}

func TestEmitter_MessageCreated_StripsBinaryData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-sb", "session-sb", "conv-sb")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMessageCreated, func(e *Event) {
		got = e
		wg.Done()
	})

	base64Data := "aGVsbG8gd29ybGQ=" // "hello world" in base64
	filePath := "/tmp/audio.mp3"
	sizeKB := int64(500)
	textVal := "Check this out"
	parts := []types.ContentPart{
		{Type: "text", Text: &textVal},
		{
			Type: "image",
			Media: &types.MediaContent{
				Data:     &base64Data,
				MIMEType: "image/jpeg",
				SizeKB:   &sizeKB,
			},
		},
		{
			Type: "audio",
			Media: &types.MediaContent{
				FilePath: &filePath,
				MIMEType: "audio/mp3",
			},
		},
	}
	emitter.MessageCreated("user", "Check this out", 0, parts, nil, nil)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for message.created event")
	}

	data, ok := got.Data.(*MessageCreatedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if len(data.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(data.Parts))
	}

	// Text part should be unchanged
	if data.Parts[0].Type != "text" || *data.Parts[0].Text != textVal {
		t.Fatal("text part was modified")
	}

	// Image: Data should be stripped, metadata preserved
	imgPart := data.Parts[1]
	if imgPart.Media.Data != nil {
		t.Error("expected Data to be stripped from image part")
	}
	if imgPart.Media.MIMEType != "image/jpeg" {
		t.Errorf("expected MIMEType image/jpeg, got %s", imgPart.Media.MIMEType)
	}
	if imgPart.Media.SizeKB == nil || *imgPart.Media.SizeKB != sizeKB {
		t.Error("SizeKB metadata was not preserved")
	}

	// Audio: FilePath should be stripped
	audPart := data.Parts[2]
	if audPart.Media.FilePath != nil {
		t.Error("expected FilePath to be stripped from audio part")
	}
	if audPart.Media.MIMEType != "audio/mp3" {
		t.Errorf("expected MIMEType audio/mp3, got %s", audPart.Media.MIMEType)
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

	data, ok := got.Data.(*MessageUpdatedData)
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

	data, ok := got.Data.(*ConversationStartedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.SystemPrompt != "You are a helpful AI assistant." {
		t.Fatalf("unexpected system prompt: %s", data.SystemPrompt)
	}
}

func TestEmitter_ToolCallStarted_Labels(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-lbl", "session-lbl", "conv-lbl")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventToolCallStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	labels := map[string]string{"handler": "http", "team": "platform"}
	emitter.ToolCallStarted("search", "call-1", nil, labels)
	wg.Wait()

	data, ok := got.Data.(*ToolCallStartedData)
	if !ok {
		t.Fatalf("expected *ToolCallStartedData, got %T", got.Data)
	}
	if data.Labels["handler"] != "http" {
		t.Errorf("expected handler=http, got %q", data.Labels["handler"])
	}
	if data.Labels["team"] != "platform" {
		t.Errorf("expected team=platform, got %q", data.Labels["team"])
	}
}

func TestEmitter_ProviderCallStarted_Labels(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-lbl2", "session-lbl2", "conv-lbl2")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventProviderCallStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	labels := map[string]string{"tier": "premium"}
	emitter.ProviderCallStarted("openai", "gpt-4", 5, 2, labels)
	wg.Wait()

	data, ok := got.Data.(*ProviderCallStartedData)
	if !ok {
		t.Fatalf("expected *ProviderCallStartedData, got %T", got.Data)
	}
	if data.Labels["tier"] != "premium" {
		t.Errorf("expected tier=premium, got %q", data.Labels["tier"])
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

func TestEmitter_ClientToolRequest(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ctr", "session-ctr", "conv-ctr")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventClientToolRequest, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.ClientToolRequest(&ClientToolRequestData{
		CallID:     "call-1",
		ToolName:   "get_location",
		Args:       map[string]any{"accuracy": "fine"},
		ConsentMsg: "Allow location access?",
		Categories: []string{"location", "sensors"},
	})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for tool.client.request event")
	}

	if got.RunID != "run-ctr" || got.SessionID != "session-ctr" || got.ConversationID != "conv-ctr" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(*ClientToolRequestData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.CallID != "call-1" || data.ToolName != "get_location" {
		t.Fatalf("unexpected data: %+v", data)
	}
	if data.ConsentMsg != "Allow location access?" {
		t.Fatalf("unexpected consent msg: %s", data.ConsentMsg)
	}
	if len(data.Categories) != 2 || data.Categories[0] != "location" {
		t.Fatalf("unexpected categories: %v", data.Categories)
	}
}

func TestEmitter_ClientToolRequest_NilData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ctrn", "session-ctrn", "conv-ctrn")

	// Should not panic when data is nil
	emitter.ClientToolRequest(nil)
}

func TestEmitter_ClientToolResolved(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ctr2", "session-ctr2", "conv-ctr2")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventClientToolResolved, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.ClientToolResolved(&ClientToolResolvedData{
		CallID:   "call-42",
		ToolName: "get_location",
		Status:   "fulfilled",
	})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for tool.client.resolved event")
	}

	data, ok := got.Data.(*ClientToolResolvedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.CallID != "call-42" || data.ToolName != "get_location" || data.Status != "fulfilled" {
		t.Fatalf("unexpected data: %+v", data)
	}
}

func TestEmitter_ClientToolResolved_Rejected(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ctr3", "session-ctr3", "conv-ctr3")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventClientToolResolved, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.ClientToolResolved(&ClientToolResolvedData{
		CallID:          "call-99",
		Status:          "rejected",
		RejectionReason: "user denied",
	})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for tool.client.resolved event")
	}

	data := got.Data.(*ClientToolResolvedData)
	if data.Status != "rejected" || data.RejectionReason != "user denied" {
		t.Fatalf("unexpected rejection data: %+v", data)
	}
}

func TestEmitter_ClientToolResolved_NilData(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-ctr4", "session-ctr4", "conv-ctr4")

	// Should not panic when data is nil
	emitter.ClientToolResolved(nil)
}
