package stage_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTTSStageWithInterruption(t *testing.T) {
	s := stage.NewTTSStageWithInterruption(&mockTTSService{}, stage.DefaultTTSStageWithInterruptionConfig())

	assert.Equal(t, stage.StageTypeTransform, s.Type())
	assert.Equal(t, "tts_interruptible", s.Name())
}

func TestDefaultTTSStageWithInterruptionConfig(t *testing.T) {
	config := stage.DefaultTTSStageWithInterruptionConfig()

	assert.NotEmpty(t, config.Voice, "Voice should have a default value")
	assert.NotZero(t, config.Speed, "Speed should have a default value")
	assert.True(t, config.SkipEmpty, "SkipEmpty should default to true")
}

func TestTTSStageWithInterruption_SynthesizesText(t *testing.T) {
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("test audio data")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{makeTextElement("Hello, I am speaking")}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Audio, "Expected audio output")
	assert.NotEmpty(t, results[0].Audio.Samples)
}

func TestTTSStageWithInterruption_OutputFormat(t *testing.T) {
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{makeTextElement("Test output format")}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Audio)
	assert.Equal(t, 24000, results[0].Audio.SampleRate)
	assert.Equal(t, 1, results[0].Audio.Channels)
	assert.Equal(t, stage.AudioFormatPCM16, results[0].Audio.Format)
}

func TestTTSStageWithInterruption_MultipleTexts(t *testing.T) {
	synthesizeCount := 0
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesizeCount++
			return io.NopCloser(strings.NewReader("audio data")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{
		makeTextElement("First sentence"),
		makeTextElement("Second sentence"),
		makeTextElement("Third sentence"),
	}
	results := runStage(t, s, inputs, 5*time.Second)

	audioCount := 0
	for _, r := range results {
		if r.Audio != nil {
			audioCount++
		}
	}
	assert.Equal(t, 3, audioCount, "Expected 3 audio outputs")
	assert.Equal(t, 3, synthesizeCount, "Expected 3 synthesize calls")
}

func TestTTSStageWithInterruption_ExtractText_FromMessage(t *testing.T) {
	synthesized := false
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			assert.Equal(t, "Hello from message", text)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	msg := &types.Message{Role: "assistant", Content: "Hello from message"}
	inputs := []stage.StreamElement{{Message: msg}}
	results := runStage(t, s, inputs, 2*time.Second)

	require.GreaterOrEqual(t, len(results), 1)
	assert.True(t, synthesized, "should have called synthesize")
}

func TestTTSStageWithInterruption_ExtractText_FromMessageParts(t *testing.T) {
	synthesized := false
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			assert.Equal(t, "Hello from part", text)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	partText := "Hello from part"
	msg := &types.Message{
		Role:    "assistant",
		Content: "", // Empty content, should fall through to parts
		Parts:   []types.ContentPart{{Type: "text", Text: &partText}},
	}
	inputs := []stage.StreamElement{{Message: msg}}
	results := runStage(t, s, inputs, 2*time.Second)

	require.GreaterOrEqual(t, len(results), 1)
	assert.True(t, synthesized, "should have called synthesize")
}

func TestTTSStageWithInterruption_ExtractText_EmptyMessageContent(t *testing.T) {
	synthesized := false
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesized = true
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	msg := &types.Message{Role: "assistant", Content: ""}
	inputs := []stage.StreamElement{{Message: msg}}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- inputs[0]
	close(input)

	time.Sleep(100 * time.Millisecond)
	assert.False(t, synthesized, "should not have called synthesize for empty text")
}

func TestTTSStageWithInterruption_PassesThroughNonText(t *testing.T) {
	s := stage.NewTTSStageWithInterruption(&mockTTSService{}, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{
		makeAudioElement(make([]byte, 100), 16000),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	assert.NotNil(t, results[0].Audio, "Expected audio passthrough")
}

func TestTTSStageWithInterruption_EndOfStream(t *testing.T) {
	synthesizeCalled := false
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesizeCalled = true
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{makeEndOfStreamElement()}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	assert.True(t, results[0].EndOfStream, "EndOfStream should be forwarded")
	assert.False(t, synthesizeCalled, "Synthesize should not be called for EndOfStream")
}

func TestTTSStageWithInterruption_SkipsEmptyText(t *testing.T) {
	synthesizeCalled := false
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			synthesizeCalled = true
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	config := stage.DefaultTTSStageWithInterruptionConfig()
	config.SkipEmpty = true
	config.MinTextLength = 5

	s := stage.NewTTSStageWithInterruption(mock, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- makeTextElement("Hi") // shorter than MinTextLength=5
	close(input)

	time.Sleep(100 * time.Millisecond)
	assert.False(t, synthesizeCalled, "Synthesize should not be called for short text")
}

func TestTTSStageWithInterruption_MessageElement_NilText(t *testing.T) {
	s := stage.NewTTSStageWithInterruption(&mockTTSService{}, stage.DefaultTTSStageWithInterruptionConfig())

	// Element with nil Text and nil Message — should not panic
	elem := stage.StreamElement{}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- elem
	close(input)

	time.Sleep(100 * time.Millisecond)
	// No panic is the test passing
}

func TestTTSStageWithInterruption_SynthesisError(t *testing.T) {
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return nil, io.EOF
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{makeTextElement("Test synthesis error")}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	assert.NotNil(t, results[0].Error, "Expected error element for synthesis failure")
}

func TestTTSStageWithInterruption_PerformSynthesis_ReadError(t *testing.T) {
	synthesizeCalled := make(chan struct{}, 1)
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			select {
			case synthesizeCalled <- struct{}{}:
			default:
			}
			return io.NopCloser(&errorReader{err: io.ErrUnexpectedEOF}), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- stage.StreamElement{Text: func() *string { s := "Test text for read error"; return &s }()}
	close(input)

	select {
	case <-synthesizeCalled:
		// Good - synthesis was called
	case <-time.After(300 * time.Millisecond):
		t.Error("should have tried to synthesize")
	}
}

func TestTTSStageWithInterruption_ContextCancellation(t *testing.T) {
	mock := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			time.Sleep(500 * time.Millisecond)
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(mock, stage.DefaultTTSStageWithInterruptionConfig())

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Process(ctx, input, output)
	}()

	input <- makeTextElement("Test context cancellation")
	cancel()
	close(input)

	select {
	case <-errCh:
		// Expected - context cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("Process should have returned after context cancellation")
	}
}

func TestTTSStageWithInterruption_WithInterruption(t *testing.T) {
	mock := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("audio")), nil
		},
	}

	handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	handler.SetBotSpeaking(true)

	config := stage.DefaultTTSStageWithInterruptionConfig()
	config.InterruptionHandler = handler

	s := stage.NewTTSStageWithInterruption(mock, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Simulate interruption by processing a Speaking VAD state
	_, _ = handler.ProcessVADState(ctx, audio.VADStateSpeaking)

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- makeTextElement("This should be interrupted")
	close(input)

	// Wait for processing — audio should be skipped
	time.Sleep(200 * time.Millisecond)
}

func TestTTSStageWithInterruption_PostSynthesisInterruption(t *testing.T) {
	config := stage.DefaultTTSStageWithInterruptionConfig()
	handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	handler.SetBotSpeaking(true)
	config.InterruptionHandler = handler

	mock := &mockTTSService{
		synthesizeFunc: func(ctx context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			// Trigger interruption INSIDE synthesizeFunc
			_, _ = handler.ProcessVADState(ctx, audio.VADStateSpeaking)
			return io.NopCloser(strings.NewReader("audio data")), nil
		},
	}

	s := stage.NewTTSStageWithInterruption(mock, config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- makeTextElement("Test post-synthesis interruption")
	close(input)

	time.Sleep(200 * time.Millisecond)

	// Audio should be discarded; no audio elements expected
	select {
	case elem := <-output:
		if elem.Audio != nil {
			t.Errorf("Expected audio to be discarded after interruption, got audio element")
		}
	default:
		// Nothing in output is expected
	}
}
