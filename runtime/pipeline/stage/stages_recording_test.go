package stage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRecordingStage(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	assert.Equal(t, "recording_input", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())
}

func TestRecordingStage_TextElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:       RecordingPositionInput,
		SessionID:      "test-session",
		ConversationID: "conv-1",
	}

	stage := NewRecordingStage(bus, config)

	// Capture published events
	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	// Run stage with text element
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "Hello, world!"
	input <- StreamElement{
		Text:      &text,
		Timestamp: time.Now(),
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Verify element passed through
	elem := <-output
	assert.Equal(t, "Hello, world!", *elem.Text)

	// Wait for async event delivery
	time.Sleep(50 * time.Millisecond)

	// Verify event was published
	mu.Lock()
	require.Len(t, captured, 1)
	assert.Equal(t, events.EventMessageCreated, captured[0].Type)
	assert.Equal(t, "test-session", captured[0].SessionID)
	assert.Equal(t, "conv-1", captured[0].ConversationID)

	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "user", data.Role) // Input position = user
	assert.Equal(t, "Hello, world!", data.Content)
	mu.Unlock()
}

func TestRecordingStage_OutputPosition(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "Response"
	input <- StreamElement{Text: &text, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role) // Output position = assistant
	mu.Unlock()
}

func TestRecordingStage_MessageElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := &types.Message{
		Role:    "assistant",
		Content: "Here's the result",
		ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "search", Args: json.RawMessage(`{"query":"test"}`)},
		},
	}
	input <- StreamElement{Message: msg, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Equal(t, "Here's the result", data.Content)
	require.Len(t, data.ToolCalls, 1)
	assert.Equal(t, "call_1", data.ToolCalls[0].ID)
	assert.Equal(t, "search", data.ToolCalls[0].Name)
	mu.Unlock()
}

func TestRecordingStage_ToolCallElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventToolCallStarted, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	tc := &types.MessageToolCall{
		ID:   "call_123",
		Name: "get_weather",
		Args: json.RawMessage(`{"location":"SF"}`),
	}
	input <- StreamElement{ToolCall: tc, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.ToolCallStartedData)
	assert.Equal(t, "call_123", data.CallID)
	assert.Equal(t, "get_weather", data.ToolName)
	mu.Unlock()
}

func TestRecordingStage_ErrorElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventStreamInterrupted, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- NewErrorElement(assert.AnError)
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.StreamInterruptedData)
	assert.Contains(t, data.Reason, "assert.AnError")
	mu.Unlock()
}

func TestRecordingStage_SkipsEndOfStream(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var eventCount int
	var mu sync.Mutex
	bus.Subscribe("*", func(e *events.Event) {
		mu.Lock()
		eventCount++
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- NewEndOfStreamElement()
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Element should pass through
	elem := <-output
	assert.True(t, elem.EndOfStream)

	time.Sleep(50 * time.Millisecond)

	// No events should be published for end-of-stream
	mu.Lock()
	assert.Equal(t, 0, eventCount)
	mu.Unlock()
}

func TestRecordingStage_ContextCancellation(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan StreamElement)
	output := make(chan StreamElement)

	// Start processing in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- stage.Process(ctx, input, output)
	}()

	// Cancel context
	cancel()

	// Send element after cancellation
	text := "test"
	select {
	case input <- StreamElement{Text: &text}:
	case <-time.After(100 * time.Millisecond):
		// Expected - context cancelled
	}

	close(input)

	// Should return context error
	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRecordingStage_NilEventBus(t *testing.T) {
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	// Create stage with nil event bus
	stage := NewRecordingStage(nil, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "test"
	input <- StreamElement{Text: &text, Timestamp: time.Now()}
	close(input)

	// Should not panic, just skip recording
	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Element should still pass through
	elem := <-output
	assert.Equal(t, "test", *elem.Text)
}

func TestRecordingStage_MultipleElements(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Send multiple text elements
	for i := 0; i < 3; i++ {
		text := "message"
		input <- StreamElement{Text: &text, Timestamp: time.Now()}
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Drain output
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 3, count)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Len(t, captured, 3)
	mu.Unlock()
}

func TestRecordingStage_WithSessionID(t *testing.T) {
	bus := events.NewEventBus()
	config := DefaultRecordingStageConfig()

	stage := NewRecordingStage(bus, config).
		WithSessionID("my-session").
		WithConversationID("my-conv")

	assert.Equal(t, "my-session", stage.config.SessionID)
	assert.Equal(t, "my-conv", stage.config.ConversationID)
}

func TestDefaultRecordingStageConfig(t *testing.T) {
	cfg := DefaultRecordingStageConfig()

	assert.Equal(t, RecordingPositionInput, cfg.Position)
	assert.True(t, cfg.IncludeAudio)
	assert.False(t, cfg.IncludeVideo)
	assert.True(t, cfg.IncludeImages)
}

func TestRecordingStage_AudioElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:     RecordingPositionInput,
		SessionID:    "test-session",
		IncludeAudio: true,
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	audio := &AudioData{
		Samples:    make([]byte, 1024),
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
		Duration:   100 * time.Millisecond,
	}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "user", data.Role)
	assert.Contains(t, data.Content, "sample_rate")
	assert.Contains(t, data.Content, "16000")
	mu.Unlock()
}

func TestRecordingStage_AudioExcluded(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:     RecordingPositionInput,
		SessionID:    "test-session",
		IncludeAudio: false, // Audio disabled
	}

	stage := NewRecordingStage(bus, config)

	var eventCount int
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		eventCount++
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	audio := &AudioData{Samples: make([]byte, 1024)}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Element still passes through
	<-output

	time.Sleep(50 * time.Millisecond)

	// But no event recorded
	mu.Lock()
	assert.Equal(t, 0, eventCount)
	mu.Unlock()
}

func TestRecordingStage_ImageElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:      RecordingPositionOutput,
		SessionID:     "test-session",
		IncludeImages: true,
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	img := &ImageData{
		Data:     make([]byte, 512),
		MIMEType: "image/png",
		Width:    100,
		Height:   100,
		Format:   "png",
	}
	input <- StreamElement{Image: img, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Contains(t, data.Content, "image/png")
	mu.Unlock()
}

func TestRecordingStage_VideoElement(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:     RecordingPositionOutput,
		SessionID:    "test-session",
		IncludeVideo: true,
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	video := &VideoData{
		Data:      make([]byte, 1024),
		MIMEType:  "video/mp4",
		Width:     640,
		Height:    480,
		FrameRate: 30,
		Duration:  time.Second,
		Format:    "mp4",
	}
	input <- StreamElement{Video: video, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Contains(t, data.Content, "video/mp4")
	assert.Contains(t, data.Content, "640")
	mu.Unlock()
}

func TestRecordingStage_MessageWithToolResult(t *testing.T) {
	bus := events.NewEventBus()
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(bus, config)

	var captured []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := &types.Message{
		Role:    "tool",
		Content: "Weather result",
		ToolResult: &types.MessageToolResult{
			ID:      "call_123",
			Name:    "get_weather",
			Content: "Sunny, 72°F",
		},
	}
	input <- StreamElement{Message: msg, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "tool", data.Role)
	require.NotNil(t, data.ToolResult)
	assert.Equal(t, "call_123", data.ToolResult.ID)
	assert.Equal(t, "get_weather", data.ToolResult.Name)
	mu.Unlock()
}
