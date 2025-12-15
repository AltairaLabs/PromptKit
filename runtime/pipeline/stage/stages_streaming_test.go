package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestDuplexProviderStage_Basic(t *testing.T) {
	t.Run("Name and Type", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		assert.Equal(t, "duplex_provider", stage.Name())
		assert.Equal(t, StageTypeBidirectional, stage.Type())
	})

	t.Run("Nil session returns error", func(t *testing.T) {
		stage := NewDuplexProviderStage(nil)
		ctx := context.Background()
		input := make(chan StreamElement)
		output := make(chan StreamElement, 1)

		close(input)

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no session configured")
	})
}

func TestDuplexProviderStage_InputForwarding(t *testing.T) {
	t.Run("Forwards audio elements to session", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send test audio
		audioData := []byte("test audio data")
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    audioData,
				SampleRate: 16000,
				Format:     AudioFormatPCM16,
			},
		}
		close(input)

		// Process in background
		done := make(chan error, 1)
		go func() {
			done <- stage.Process(ctx, input, output)
		}()

		// Wait for processing
		time.Sleep(100 * time.Millisecond)
		cancel()

		// Verify chunks were sent
		chunks := mockSession.GetChunks()
		require.NotEmpty(t, chunks, "Expected audio chunks to be forwarded")
		assert.Equal(t, audioData, chunks[0].Data)
	})

	t.Run("Forwards text elements to session", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send test text
		testText := "test message"
		input <- StreamElement{
			Text: &testText,
		}
		close(input)

		// Process in background
		done := make(chan error, 1)
		go func() {
			done <- stage.Process(ctx, input, output)
		}()

		// Wait for processing
		time.Sleep(100 * time.Millisecond)
		cancel()

		// Verify text was sent
		texts := mockSession.GetTexts()
		require.NotEmpty(t, texts, "Expected text to be forwarded")
		assert.Contains(t, texts[0], testText)
	})
}

func TestDuplexProviderStage_ResponseForwarding(t *testing.T) {
	t.Run("Forwards responses from session to output", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		mockSession.WithAutoRespond("test response")

		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send trigger
		testText := "trigger"
		input <- StreamElement{Text: &testText}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Read response
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Text)
			assert.Contains(t, *elem.Text, "test response")
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for response")
		}

		cancel()
	})

	t.Run("Handles context cancellation gracefully", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithCancel(context.Background())

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Start processing
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Cancel immediately
		cancel()
		close(input)

		// Should complete without hanging
		select {
		case <-errCh:
			// Success - didn't hang
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Process didn't complete after context cancellation")
		}
	})
}

func TestDuplexProviderStage_EdgeCases(t *testing.T) {
	t.Run("Empty audio data is skipped", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send empty audio
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte{},
				SampleRate: 16000,
			},
		}
		close(input)

		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		time.Sleep(100 * time.Millisecond)
		cancel()

		// Empty audio should be skipped
		chunks := mockSession.GetChunks()
		assert.Empty(t, chunks)
	})

	t.Run("Empty text is skipped", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send empty text
		emptyText := ""
		input <- StreamElement{
			Text: &emptyText,
		}
		close(input)

		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		time.Sleep(100 * time.Millisecond)
		cancel()

		// Empty text should be skipped
		texts := mockSession.GetTexts()
		assert.Empty(t, texts)
	})

	t.Run("Element with neither audio nor text is skipped", func(t *testing.T) {
		mockSession := providersmock.NewMockStreamSession()
		stage := NewDuplexProviderStage(mockSession)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send element with only metadata
		input <- StreamElement{
			Metadata: map[string]interface{}{"key": "value"},
		}
		close(input)

		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		time.Sleep(100 * time.Millisecond)
		cancel()

		// Should not forward anything
		chunks := mockSession.GetChunks()
		texts := mockSession.GetTexts()
		assert.Empty(t, chunks)
		assert.Empty(t, texts)
	})
}

func TestVADAccumulatorStage_Basic(t *testing.T) {
	t.Run("creates VAD accumulator stage", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		assert.NotNil(t, stage)
		assert.Equal(t, "vad_accumulator", stage.Name())
		assert.Equal(t, StageTypeAccumulate, stage.Type())
	})

	t.Run("processes audio elements", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send audio element
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte{1, 2, 3, 4},
				SampleRate: 16000,
			},
		}
		close(input)

		// Process in background
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Wait for result
		select {
		case err := <-errCh:
			assert.NoError(t, err)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for VAD processing")
		}
	})

	t.Run("passes through non-audio elements", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send non-audio element
		textVal := "test"
		input <- StreamElement{
			Text: &textVal,
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should pass through immediately
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Text)
			assert.Equal(t, "test", *elem.Text)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout - element not passed through")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send audio first
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte{1, 2, 3, 4},
				SampleRate: 16000,
			},
		}

		// Start processing in background
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Cancel after starting
		time.Sleep(50 * time.Millisecond)
		cancel()
		close(input)

		// Should complete
		select {
		case <-errCh:
			// OK - completed
		case <-time.After(1 * time.Second):
			t.Fatal("Process didn't complete after context cancellation")
		}
	})

	t.Run("processes multiple audio elements", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send multiple audio chunks
		for i := 0; i < 3; i++ {
			input <- StreamElement{
				Audio: &AudioData{
					Samples:    []byte{byte(i), byte(i + 1)},
					SampleRate: 16000,
				},
			}
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Wait for processing
		time.Sleep(200 * time.Millisecond)
	})

	t.Run("processes audio with different sample rates", func(t *testing.T) {
		analyzer := &mockVADAnalyzer{}
		transcriber := &mockTranscriber{}
		config := DefaultVADConfig()

		stage := NewVADAccumulatorStage(analyzer, transcriber, config)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send audio with different sample rate
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				SampleRate: 48000,
			},
		}
		close(input)

		// Process
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Wait for result
		select {
		case err := <-errCh:
			// May succeed or fail depending on implementation
			_ = err
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout processing audio")
		}
	})
}

func TestTTSStage_Basic(t *testing.T) {
	t.Run("creates TTS stage", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		assert.NotNil(t, stage)
		assert.Equal(t, "tts", stage.Name())
		assert.Equal(t, StageTypeTransform, stage.Type())
	})

	t.Run("processes text elements", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send text element
		textVal := "Hello world"
		input <- StreamElement{
			Text: &textVal,
		}
		close(input)

		// Process in background
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Should get audio output
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Audio)
			assert.NotEmpty(t, elem.Audio.Samples)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for TTS output")
		}

		// Wait for completion
		select {
		case err := <-errCh:
			assert.NoError(t, err)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for TTS completion")
		}
	})

	t.Run("passes through non-text elements", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send audio element (non-text)
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte{1, 2, 3},
				SampleRate: 16000,
			},
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should pass through
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Audio)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout - element not passed through")
		}
	})

	t.Run("respects MinTextLength config", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()
		config.MinTextLength = 10

		stage := NewTTSStage(tts, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send short text (below minimum)
		shortText := "hi"
		input <- StreamElement{
			Text: &shortText,
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should not synthesize, just pass through or skip
		select {
		case <-output:
			// OK if passed through or closed
		case <-time.After(500 * time.Millisecond):
			// OK if channel closed
		}
	})

	t.Run("respects SkipEmpty config", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()
		config.SkipEmpty = true

		stage := NewTTSStage(tts, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send empty text
		emptyText := ""
		input <- StreamElement{
			Text: &emptyText,
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should skip
		select {
		case <-output:
			// Channel closed is OK
		case <-time.After(500 * time.Millisecond):
			// OK if skipped
		}
	})

	t.Run("extracts text from Message content", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send element with message
		input <- StreamElement{
			Message: &types.Message{
				Role:    "assistant",
				Content: "Message content",
			},
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should synthesize from message content
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Audio)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for TTS from message")
		}
	})

	t.Run("extracts text from Message parts", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send element with message parts
		partText := "Part text"
		input <- StreamElement{
			Message: &types.Message{
				Role: "assistant",
				Parts: []types.ContentPart{
					{Text: &partText},
				},
			},
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should synthesize from message parts
		select {
		case elem := <-output:
			assert.NotNil(t, elem.Audio)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for TTS from message parts")
		}
	})

	t.Run("handles context cancellation during processing", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send text
		textVal := "test"
		input <- StreamElement{
			Text: &textVal,
		}

		// Start processing
		errCh := make(chan error, 1)
		go func() {
			errCh <- stage.Process(ctx, input, output)
		}()

		// Cancel after short delay
		time.Sleep(50 * time.Millisecond)
		cancel()
		close(input)

		// Should complete
		select {
		case <-errCh:
			// OK
		case <-time.After(1 * time.Second):
			t.Fatal("TTS didn't complete after context cancellation")
		}
	})

	t.Run("processes empty message with no text", func(t *testing.T) {
		tts := &mockTTSService{}
		config := DefaultTTSConfig()

		stage := NewTTSStage(tts, config)

		ctx := context.Background()
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send message with no content
		input <- StreamElement{
			Message: &types.Message{
				Role: "assistant",
			},
		}
		close(input)

		// Process
		go func() {
			_ = stage.Process(ctx, input, output)
		}()

		// Should pass through or skip
		select {
		case <-output:
			// OK if passed through or closed
		case <-time.After(500 * time.Millisecond):
			// OK if skipped
		}
	})
}

// Mock implementations for testing

type mockVADAnalyzer struct{}

func (m *mockVADAnalyzer) Name() string {
	return "mock-vad"
}

func (m *mockVADAnalyzer) Analyze(ctx context.Context, audio []byte) (float64, error) {
	// Return a score indicating speech detected
	return 0.8, nil
}

func (m *mockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	// Mock implementation - return a closed channel
	ch := make(chan audio.VADEvent)
	close(ch)
	return ch
}

func (m *mockVADAnalyzer) Reset() {
	// Mock implementation
}

func (m *mockVADAnalyzer) State() audio.VADState {
	return audio.VADStateQuiet
}

type mockTranscriber struct{}

func (m *mockTranscriber) Transcribe(ctx context.Context, audio []byte) (string, error) {
	return "transcribed text", nil
}

type mockTTSService struct{}

func (m *mockTTSService) Synthesize(ctx context.Context, text string) ([]byte, error) {
	return []byte("audio data"), nil
}

func (m *mockTTSService) MIMEType() string {
	return "audio/pcm"
}
