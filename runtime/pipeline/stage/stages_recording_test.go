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

// fakeEventStore captures appended events for assertions.
// Implements events.EventStore.
type fakeEventStore struct {
	mu     sync.Mutex
	events []*events.Event
}

func (f *fakeEventStore) Append(_ context.Context, e *events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}
func (f *fakeEventStore) OnEvent(*events.Event) {}
func (f *fakeEventStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (f *fakeEventStore) QueryRaw(_ context.Context, _ *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (f *fakeEventStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (f *fakeEventStore) Close() error { return nil }

// snapshot returns a copy of recorded events safe for inspection.
func (f *fakeEventStore) snapshot() []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*events.Event, len(f.events))
	copy(out, f.events)
	return out
}

// filterByType returns events whose Type matches.
func (f *fakeEventStore) filterByType(t events.EventType) []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*events.Event
	for _, e := range f.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func TestNewRecordingStage(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	assert.Equal(t, "recording_input", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())
}

func TestRecordingStage_TextElement_SkipsChunks(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:       RecordingPositionInput,
		SessionID:      "test-session",
		ConversationID: "conv-1",
	}

	stage := NewRecordingStage(store, config)

	// Run stage with text element (streaming chunk)
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

	// Verify element passed through (recording doesn't filter downstream)
	elem := <-output
	assert.Equal(t, "Hello, world!", *elem.Text)

	// Text elements (streaming chunks) should NOT produce message.created events.
	// The complete message arrives as a Message element after streaming finishes.
	assert.Empty(t, store.snapshot(), "text chunks should not produce message.created events")
}

func TestRecordingStage_OutputPosition(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Use a Message element (not Text) to verify output position → assistant role
	input <- StreamElement{
		Message:   &types.Message{Role: "assistant", Content: "Response"},
		Timestamp: time.Now(),
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
}

func TestRecordingStage_MessageElement(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

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

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Equal(t, "Here's the result", data.Content)
	require.Len(t, data.ToolCalls, 1)
	assert.Equal(t, "call_1", data.ToolCalls[0].ID)
	assert.Equal(t, "search", data.ToolCalls[0].Name)
}

func TestRecordingStage_ToolCallElement(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

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

	captured := store.filterByType(events.EventToolCallStarted)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.ToolCallStartedData)
	assert.Equal(t, "call_123", data.CallID)
	assert.Equal(t, "get_weather", data.ToolName)
}

func TestRecordingStage_ErrorElement(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionOutput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- NewErrorElement(assert.AnError)
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	captured := store.filterByType(events.EventStreamInterrupted)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.StreamInterruptedData)
	assert.Contains(t, data.Reason, "assert.AnError")
}

func TestRecordingStage_SkipsEndOfStream(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- NewEndOfStreamElement()
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Element should pass through
	elem := <-output
	assert.True(t, elem.EndOfStream)

	// No events should be persisted for end-of-stream
	assert.Empty(t, store.snapshot())
}

func TestRecordingStage_ContextCancellation(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

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

func TestRecordingStage_NilEventStore(t *testing.T) {
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	// Create stage with nil event store
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

func TestRecordingStage_MultipleChunksProduceNoEvents(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Send multiple text elements (streaming chunks)
	for i := 0; i < 3; i++ {
		text := "chunk"
		input <- StreamElement{Text: &text, Timestamp: time.Now()}
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// All chunks pass through downstream
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 3, count)

	// No message.created events — chunks are skipped
	assert.Empty(t, store.snapshot(), "text chunks should not produce events")
}

func TestRecordingStage_StreamingTextOptIn(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:             RecordingPositionOutput,
		SessionID:            "test-session",
		ConversationID:       "conv-1",
		IncludeStreamingText: true, // opt in to chunk recording
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	for _, chunk := range []string{"Hello", " world", "!"} {
		c := chunk
		input <- StreamElement{Text: &c, Timestamp: time.Now()}
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	for range output {
	}

	// With IncludeStreamingText enabled, each chunk produces an event
	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 3, "each chunk should produce an event")
	// Verify all are assistant role with non-empty content (order is FIFO append)
	contents := make(map[string]bool)
	for _, e := range captured {
		data := e.Data.(*events.MessageCreatedData)
		assert.Equal(t, "assistant", data.Role)
		contents[data.Content] = true
	}
	assert.True(t, contents["Hello"])
	assert.True(t, contents[" world"])
	assert.True(t, contents["!"])
}

func TestRecordingStage_WithSessionID(t *testing.T) {
	store := &fakeEventStore{}
	config := DefaultRecordingStageConfig()

	stage := NewRecordingStage(store, config).
		WithSessionID("my-session").
		WithConversationID("my-conv")

	assert.Equal(t, "my-session", stage.config.SessionID)
	assert.Equal(t, "my-conv", stage.config.ConversationID)
}

func TestDefaultRecordingStageConfig(t *testing.T) {
	cfg := DefaultRecordingStageConfig()

	assert.Equal(t, RecordingPositionInput, cfg.Position)
	assert.False(t, cfg.IncludeStreamingText, "streaming text off by default")
	assert.True(t, cfg.IncludeAudio)
	assert.False(t, cfg.IncludeVideo)
	assert.True(t, cfg.IncludeImages)
}

func TestRecordingStage_AudioElement_InputProducesAudioInputEvent(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:     RecordingPositionInput,
		SessionID:    "test-session",
		IncludeAudio: true,
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	samples := make([]byte, 1024)
	audio := &AudioData{
		Samples:    samples,
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
		Duration:   100 * time.Millisecond,
	}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Recording-stage tap should produce EventAudioInput, not EventMessageCreated.
	assert.Empty(t, store.filterByType(events.EventMessageCreated),
		"audio recording must not be emitted as message.created")

	captured := store.filterByType(events.EventAudioInput)
	require.Len(t, captured, 1)
	data, ok := captured[0].Data.(*events.AudioEventData)
	require.True(t, ok, "expected *events.AudioEventData, got %T", captured[0].Data)

	assert.Equal(t, "input", data.Direction)
	assert.Equal(t, "user", data.Actor)
	assert.Empty(t, data.GeneratedFrom, "input events should not set GeneratedFrom")
	assert.Equal(t, samples, data.Payload.InlineData)
	assert.Equal(t, "audio/pcm", data.Payload.MIMEType)
	assert.Equal(t, int64(len(samples)), data.Payload.Size)
	assert.Equal(t, 16000, data.Metadata.SampleRate)
	assert.Equal(t, 1, data.Metadata.Channels)
	assert.Equal(t, "pcm_linear16", data.Metadata.Encoding)
	assert.Equal(t, int64(100), data.Metadata.DurationMs)
	assert.Equal(t, "test-session", captured[0].SessionID)
}

func TestRecordingStage_AudioElement_OutputProducesAudioOutputEvent(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:     RecordingPositionOutput,
		SessionID:    "test-session",
		IncludeAudio: true,
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	samples := make([]byte, 2048)
	audio := &AudioData{
		Samples:    samples,
		SampleRate: 24000,
		Channels:   1,
		Format:     AudioFormatPCM16,
		Duration:   200 * time.Millisecond,
	}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	assert.Empty(t, store.filterByType(events.EventAudioInput),
		"output position should not produce audio.input events")
	captured := store.filterByType(events.EventAudioOutput)
	require.Len(t, captured, 1)
	data, ok := captured[0].Data.(*events.AudioEventData)
	require.True(t, ok, "expected *events.AudioEventData, got %T", captured[0].Data)

	assert.Equal(t, "output", data.Direction)
	assert.Empty(t, data.Actor, "output events should not set Actor")
	assert.Equal(t, "model", data.GeneratedFrom)
	assert.Equal(t, samples, data.Payload.InlineData)
	assert.Equal(t, 24000, data.Metadata.SampleRate)
	assert.Equal(t, "pcm_linear16", data.Metadata.Encoding)
	assert.Equal(t, int64(200), data.Metadata.DurationMs)
}

func TestRecordingStage_AudioElement_DurationDerivedFromBytes(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:     RecordingPositionInput,
		SessionID:    "test-session",
		IncludeAudio: true,
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// 16kHz mono 16-bit PCM, 3200 bytes = 1600 samples = 100ms.
	audio := &AudioData{
		Samples:    make([]byte, 3200),
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
		// Duration intentionally zero — recorder should derive it from byte count.
	}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	captured := store.filterByType(events.EventAudioInput)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.AudioEventData)
	assert.Equal(t, int64(100), data.Metadata.DurationMs,
		"duration should be derived from byte count when AudioData.Duration is zero")
}

func TestRecordingStage_AudioExcluded(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:     RecordingPositionInput,
		SessionID:    "test-session",
		IncludeAudio: false, // Audio disabled
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	audio := &AudioData{Samples: make([]byte, 1024)}
	input <- StreamElement{Audio: audio, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Element still passes through
	<-output

	// But no event recorded
	assert.Empty(t, store.snapshot())
}

func TestRecordingStage_ImageElement(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:      RecordingPositionOutput,
		SessionID:     "test-session",
		IncludeImages: true,
	}

	stage := NewRecordingStage(store, config)

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

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Contains(t, data.Content, "image/png")
}

func TestRecordingStage_VideoElement(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:     RecordingPositionOutput,
		SessionID:    "test-session",
		IncludeVideo: true,
	}

	stage := NewRecordingStage(store, config)

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

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "assistant", data.Role)
	assert.Contains(t, data.Content, "video/mp4")
	assert.Contains(t, data.Content, "640")
}

func TestRecordingStage_MessageWithToolResult(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	toolResult := types.NewTextToolResult("call_123", "get_weather", "Sunny, 72°F")
	msg := &types.Message{
		Role:       "tool",
		Content:    "Weather result",
		ToolResult: &toolResult,
	}
	input <- StreamElement{Message: msg, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "tool", data.Role)
	require.NotNil(t, data.ToolResult)
	assert.Equal(t, "call_123", data.ToolResult.ID)
	assert.Equal(t, "get_weather", data.ToolResult.Name)
}

func TestRecordingStage_MessageWithMultimodalToolResult(t *testing.T) {
	store := &fakeEventStore{}
	config := RecordingStageConfig{
		Position:  RecordingPositionInput,
		SessionID: "test-session",
	}

	stage := NewRecordingStage(store, config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create a multimodal tool result with text and image parts
	textPart := types.NewTextPart("Weather is sunny")
	imgPart := types.NewImagePartFromData("base64imgdata", "image/jpeg", nil)
	toolResult := types.MessageToolResult{
		ID:   "call_456",
		Name: "weather_visual",
		Parts: []types.ContentPart{
			textPart,
			imgPart,
		},
	}
	msg := &types.Message{
		Role:       "tool",
		Content:    "Weather is sunny",
		ToolResult: &toolResult,
	}
	input <- StreamElement{Message: msg, Timestamp: time.Now()}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	captured := store.filterByType(events.EventMessageCreated)
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.MessageCreatedData)
	assert.Equal(t, "tool", data.Role)
	require.NotNil(t, data.ToolResult)
	assert.Equal(t, "call_456", data.ToolResult.ID)
	assert.Equal(t, "weather_visual", data.ToolResult.Name)

	// Verify Parts are preserved in the recording
	require.Len(t, data.ToolResult.Parts, 2)
	assert.Equal(t, types.ContentTypeText, data.ToolResult.Parts[0].Type)
	assert.Equal(t, "Weather is sunny", *data.ToolResult.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, data.ToolResult.Parts[1].Type)
	require.NotNil(t, data.ToolResult.Parts[1].Media)
	assert.Equal(t, "image/jpeg", data.ToolResult.Parts[1].Media.MIMEType)
}
