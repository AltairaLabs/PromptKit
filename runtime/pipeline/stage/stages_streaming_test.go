package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// baseConfig returns a standard config for testing
func baseConfig() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  4096,
			SampleRate: 16000,
			Encoding:   "pcm_linear16",
			Channels:   1,
			BitDepth:   16,
		},
		Metadata: make(map[string]interface{}),
	}
}

// elementWithSystemPrompt creates a stream element with system_prompt in metadata
func elementWithSystemPrompt(prompt string) StreamElement {
	return StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": prompt,
		},
	}
}

func TestDuplexProviderStage_Basic(t *testing.T) {
	t.Run("Name and Type", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false)
		stage := NewDuplexProviderStage(provider, baseConfig())

		assert.Equal(t, "duplex_provider", stage.Name())
		assert.Equal(t, StageTypeBidirectional, stage.Type())
	})

	t.Run("Nil provider returns error", func(t *testing.T) {
		stage := NewDuplexProviderStage(nil, baseConfig())
		ctx := context.Background()
		input := make(chan StreamElement)
		output := make(chan StreamElement, 1)

		close(input)

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no provider or session configured")
	})

	t.Run("Empty input channel returns error", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false)
		stage := NewDuplexProviderStage(provider, baseConfig())
		ctx := context.Background()
		input := make(chan StreamElement)
		output := make(chan StreamElement, 1)

		close(input)

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "input channel closed before receiving first element")
	})
}

func TestDuplexProviderStage_SessionCreation(t *testing.T) {
	t.Run("Creates session with system_prompt from metadata", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send element with system prompt
		input <- elementWithSystemPrompt("You are a helpful assistant")
		close(input)

		// Process
		done := make(chan error, 1)
		go func() {
			done <- stage.Process(ctx, input, output)
		}()

		// Wait for processing
		time.Sleep(200 * time.Millisecond)
		cancel()

		// Verify session was created
		session := provider.GetSession()
		require.NotNil(t, session, "Session should be created")
	})
}

func TestDuplexProviderStage_InputForwarding(t *testing.T) {
	t.Run("Forwards audio elements to session", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first (required for session creation)
		input <- elementWithSystemPrompt("Test system prompt")

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
		time.Sleep(200 * time.Millisecond)
		cancel()

		// Verify chunks were sent
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		require.NotEmpty(t, chunks, "Expected audio chunks to be forwarded")
		assert.Equal(t, audioData, chunks[0].Data)
	})

	t.Run("Forwards text elements to session", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first
		input <- elementWithSystemPrompt("Test system prompt")

		// Send text
		testText := "Hello, world!"
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
		time.Sleep(200 * time.Millisecond)
		cancel()

		// Verify text was sent
		session := provider.GetSession()
		require.NotNil(t, session)
		texts := session.GetTexts()
		require.NotEmpty(t, texts, "Expected text to be forwarded")
		assert.Equal(t, testText, texts[0])
	})
}

func TestDuplexProviderStage_ResponseForwarding(t *testing.T) {
	t.Run("Forwards response chunks to output", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response from LLM")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt and audio to trigger response
		input <- elementWithSystemPrompt("Test system prompt")
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte("audio"),
				SampleRate: 16000,
				Format:     AudioFormatPCM16,
			},
			EndOfStream: true,
		}
		close(input)

		// Process in background
		go func() {
			stage.Process(ctx, input, output)
		}()

		// Collect output
		var outputElements []StreamElement
		timeout := time.After(500 * time.Millisecond)
	collectLoop:
		for {
			select {
			case elem, ok := <-output:
				if !ok {
					break collectLoop
				}
				outputElements = append(outputElements, elem)
			case <-timeout:
				break collectLoop
			}
		}
		cancel()

		// Should have received response
		require.NotEmpty(t, outputElements, "Should receive output elements")
	})
}

func TestDuplexProviderStage_MessageForwarding(t *testing.T) {
	t.Run("Forwards user messages to output for state store", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt
		input <- elementWithSystemPrompt("Test")

		// Send user message
		input <- StreamElement{
			Message: &types.Message{
				Role:    "user",
				Content: "Hello",
			},
		}
		close(input)

		// Process in background
		go func() {
			stage.Process(ctx, input, output)
		}()

		// Collect output
		var outputElements []StreamElement
		timeout := time.After(500 * time.Millisecond)
	collectLoop:
		for {
			select {
			case elem, ok := <-output:
				if !ok {
					break collectLoop
				}
				outputElements = append(outputElements, elem)
			case <-timeout:
				break collectLoop
			}
		}
		cancel()

		// Should have user message in output
		var hasUserMessage bool
		for _, elem := range outputElements {
			if elem.Message != nil && elem.Message.Role == "user" {
				hasUserMessage = true
				break
			}
		}
		assert.True(t, hasUserMessage, "User message should be forwarded to output")
	})
}

func TestDuplexProviderStage_ContextCancellation(t *testing.T) {
	t.Run("Respects context cancellation", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt
		input <- elementWithSystemPrompt("Test")

		// Process in background
		done := make(chan error, 1)
		go func() {
			done <- stage.Process(ctx, input, output)
		}()

		// Wait for session to be created
		time.Sleep(100 * time.Millisecond)

		// Cancel context
		cancel()

		// Process should complete
		select {
		case <-done:
			// Expected
		case <-time.After(time.Second):
			t.Fatal("Process should have completed after context cancellation")
		}
	})
}

