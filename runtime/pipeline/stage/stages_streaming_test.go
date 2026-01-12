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

func TestDuplexProviderStage_InterruptionHandling(t *testing.T) {
	t.Run("Captures interrupted responses with finish_reason metadata", func(t *testing.T) {
		// Configure provider to emit an interrupted chunk followed by a final chunk
		interruptedContent := "Sure, I'd be ha-"
		finalContent := "Absolutely! We have a free demo available."
		finishReasonComplete := "complete"

		responseChunks := []providers.StreamChunk{
			// First, some content arrives
			{
				Content: interruptedContent,
				Delta:   interruptedContent,
			},
			// Then interruption occurs
			{
				Interrupted: true,
			},
			// Then the final response
			{
				Content:      finalContent,
				Delta:        finalContent,
				FinishReason: &finishReasonComplete,
			},
		}

		provider := providersmock.NewStreamingProvider("test", "test-model", false)
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 20)

		// Send system prompt (don't close input yet - that would close the session)
		input <- elementWithSystemPrompt("Test system prompt")

		// Process in background
		go func() {
			stage.Process(ctx, input, output)
		}()

		// Wait for session to be created
		time.Sleep(50 * time.Millisecond)

		// Emit response chunks directly to the session
		session := provider.GetSession()
		require.NotNil(t, session, "Session should be created")
		for _, chunk := range responseChunks {
			session.EmitChunk(&chunk)
		}

		// Give time for chunks to be processed
		time.Sleep(50 * time.Millisecond)

		// Now close input to end the session
		close(input)

		// Collect output elements
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

		// Find the interrupted message and final message
		var interruptedMsg, finalMsg *types.Message
		for _, elem := range outputElements {
			if elem.Message != nil && elem.Message.Role == "assistant" {
				// Check for finish_reason in message metadata
				if elem.Message.Meta != nil {
					if reason, ok := elem.Message.Meta["finish_reason"].(string); ok {
						if reason == "interrupted" {
							interruptedMsg = elem.Message
						} else if reason == "complete" {
							finalMsg = elem.Message
						}
					}
				}
			}
		}

		// Verify interrupted message was captured
		assert.NotNil(t, interruptedMsg, "Should capture interrupted response as a message")
		if interruptedMsg != nil {
			assert.Equal(t, interruptedContent, interruptedMsg.Content, "Interrupted message should have partial content")
			assert.Equal(t, "interrupted", interruptedMsg.Meta["finish_reason"], "Interrupted message should have finish_reason: interrupted")
			assert.True(t, interruptedMsg.Meta["is_partial"].(bool), "Interrupted message should be marked as partial")
		}

		// Verify final message was captured
		assert.NotNil(t, finalMsg, "Should capture final response as a message")
		if finalMsg != nil {
			assert.Equal(t, finalContent, finalMsg.Content, "Final message should have complete content")
			assert.Equal(t, "complete", finalMsg.Meta["finish_reason"], "Final message should have finish_reason: complete")
		}
	})

	// This test simulates the REAL Gemini behavior we're seeing:
	// 1. Content chunks arrive (Gemini starts responding)
	// 2. interrupted: true arrives (user started speaking)
	// 3. turnComplete with NO content arrives (closing interrupted turn)
	// 4. More content arrives (Gemini's new response)
	// 5. turnComplete with content arrives (final response)
	//
	// The key issue: step 3's turnComplete should NOT emit EndOfStream
	// because it's just closing the interrupted turn, not the final response.
	t.Run("Empty turnComplete after interruption does not emit EndOfStream", func(t *testing.T) {
		finishReasonComplete := "complete"

		provider := providersmock.NewStreamingProvider("test", "test-model", false)
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 20)

		// Send system prompt with EndOfStream to allow bidirectional streaming to start
		// (the drain goroutine waits for EndOfStream before exiting)
		input <- StreamElement{
			Metadata:    map[string]interface{}{"system_prompt": "Test system prompt"},
			EndOfStream: true,
		}

		go func() {
			stage.Process(ctx, input, output)
		}()

		time.Sleep(50 * time.Millisecond)
		session := provider.GetSession()
		require.NotNil(t, session)

		// Simulate Gemini's interruption flow:
		// 1. Some content arrives
		session.EmitChunk(&providers.StreamChunk{
			Content: "Sure, I'd be ha-",
			Delta:   "Sure, I'd be ha-",
		})

		// 2. Interruption signal
		session.EmitChunk(&providers.StreamChunk{
			Interrupted: true,
		})

		// 3. Empty turnComplete (this should NOT cause EndOfStream)
		session.EmitChunk(&providers.StreamChunk{
			FinishReason: &finishReasonComplete,
			// No content - this is just closing the interrupted turn
		})

		time.Sleep(50 * time.Millisecond)

		// Collect elements so far - should NOT have EndOfStream yet
		var elementsBeforeFinal []StreamElement
		collectTimeout := time.After(100 * time.Millisecond)
	collectBeforeFinal:
		for {
			select {
			case elem := <-output:
				elementsBeforeFinal = append(elementsBeforeFinal, elem)
			case <-collectTimeout:
				break collectBeforeFinal
			}
		}

		// Verify no EndOfStream was emitted for the empty turnComplete
		hasEndOfStreamBeforeFinal := false
		hasInterruptedTurnComplete := false
		for _, elem := range elementsBeforeFinal {
			if elem.EndOfStream {
				hasEndOfStreamBeforeFinal = true
			}
			if elem.Metadata != nil {
				if itc, ok := elem.Metadata["interrupted_turn_complete"].(bool); ok && itc {
					hasInterruptedTurnComplete = true
				}
			}
		}

		assert.False(t, hasEndOfStreamBeforeFinal,
			"Empty turnComplete after interruption should NOT emit EndOfStream")
		assert.True(t, hasInterruptedTurnComplete,
			"Should emit interrupted_turn_complete metadata")

		// 4. Now the real response arrives
		session.EmitChunk(&providers.StreamChunk{
			Content: "Absolutely! We have a demo.",
			Delta:   "Absolutely! We have a demo.",
		})

		// 5. Final turnComplete with content
		session.EmitChunk(&providers.StreamChunk{
			Content:      "",
			FinishReason: &finishReasonComplete,
		})

		time.Sleep(50 * time.Millisecond)
		close(input)

		// Collect remaining elements
		var finalElements []StreamElement
		finalTimeout := time.After(500 * time.Millisecond)
	collectFinal:
		for {
			select {
			case elem, ok := <-output:
				if !ok {
					break collectFinal
				}
				finalElements = append(finalElements, elem)
			case <-finalTimeout:
				break collectFinal
			}
		}
		cancel()

		// Now we should have EndOfStream with content
		hasEndOfStreamFinal := false
		var finalMessage *types.Message
		for _, elem := range finalElements {
			if elem.EndOfStream {
				hasEndOfStreamFinal = true
				finalMessage = elem.Message
			}
		}

		assert.True(t, hasEndOfStreamFinal, "Final turnComplete should emit EndOfStream")
		assert.NotNil(t, finalMessage, "Final response should have a message")
		if finalMessage != nil {
			assert.Equal(t, "Absolutely! We have a demo.", finalMessage.Content)
		}
	})

	// Test the scenario where interruption happens with NO accumulated content
	// (Gemini was interrupted before it could respond)
	t.Run("Interruption with no content followed by empty turnComplete", func(t *testing.T) {
		finishReasonComplete := "complete"

		provider := providersmock.NewStreamingProvider("test", "test-model", false)
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 20)

		// Send system prompt with EndOfStream to allow bidirectional streaming to start
		// (the drain goroutine waits for EndOfStream before exiting)
		input <- StreamElement{
			Metadata:    map[string]interface{}{"system_prompt": "Test system prompt"},
			EndOfStream: true,
		}

		go func() {
			stage.Process(ctx, input, output)
		}()

		time.Sleep(50 * time.Millisecond)
		session := provider.GetSession()
		require.NotNil(t, session)

		// Interruption with NO prior content (Gemini hadn't started responding)
		session.EmitChunk(&providers.StreamChunk{
			Interrupted: true,
		})

		// Empty turnComplete
		session.EmitChunk(&providers.StreamChunk{
			FinishReason: &finishReasonComplete,
		})

		time.Sleep(50 * time.Millisecond)

		// Collect elements - should have interrupted_turn_complete, NOT EndOfStream
		var elements []StreamElement
		collectTimeout := time.After(100 * time.Millisecond)
	collect:
		for {
			select {
			case elem := <-output:
				elements = append(elements, elem)
			case <-collectTimeout:
				break collect
			}
		}

		hasEndOfStream := false
		hasInterruptedTurnComplete := false
		for _, elem := range elements {
			if elem.EndOfStream {
				hasEndOfStream = true
			}
			if elem.Metadata != nil {
				if itc, ok := elem.Metadata["interrupted_turn_complete"].(bool); ok && itc {
					hasInterruptedTurnComplete = true
				}
			}
		}

		assert.False(t, hasEndOfStream,
			"Empty turnComplete after interruption (no content) should NOT emit EndOfStream")
		assert.True(t, hasInterruptedTurnComplete,
			"Should emit interrupted_turn_complete metadata")

		close(input)
		cancel()
	})
}

func TestDuplexProviderStage_VideoImageForwarding(t *testing.T) {
	t.Run("Forwards video elements to session", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first (required for session creation)
		input <- elementWithSystemPrompt("Test system prompt")

		// Send test video data
		videoData := []byte{0x00, 0x00, 0x00, 0x01, 0x67} // H.264 NAL unit start
		input <- StreamElement{
			Video: &VideoData{
				Data:       videoData,
				MIMEType:   "video/h264",
				Width:      1920,
				Height:     1080,
				IsKeyFrame: true,
				Timestamp:  time.Now(),
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

		// Verify video was forwarded
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		// Note: video is forwarded through SendChunk which populates GetChunks()
		// The actual verification depends on mock implementation
		require.NotEmpty(t, chunks, "Expected video chunks to be forwarded")
	})

	t.Run("Forwards image elements to session", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first
		input <- elementWithSystemPrompt("Test system prompt")

		// Send test image data (JPEG magic bytes)
		imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
		input <- StreamElement{
			Image: &ImageData{
				Data:      imageData,
				MIMEType:  "image/jpeg",
				Width:     640,
				Height:    480,
				Timestamp: time.Now(),
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

		// Verify image was forwarded
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		require.NotEmpty(t, chunks, "Expected image chunks to be forwarded")
	})

	t.Run("Handles mixed audio video and image elements", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first
		input <- elementWithSystemPrompt("Test system prompt")

		// Send mixed media types
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte("audio data"),
				SampleRate: 16000,
				Format:     AudioFormatPCM16,
			},
		}
		input <- StreamElement{
			Image: &ImageData{
				Data:     []byte{0xFF, 0xD8},
				MIMEType: "image/jpeg",
			},
		}
		input <- StreamElement{
			Video: &VideoData{
				Data:     []byte{0x00, 0x00, 0x00, 0x01},
				MIMEType: "video/h264",
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

		// Verify all media types were forwarded
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		require.GreaterOrEqual(t, len(chunks), 3, "Expected all media chunks to be forwarded")
	})

	t.Run("Skips video elements with empty data", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first
		input <- elementWithSystemPrompt("Test system prompt")

		// Send video element with empty data - should be skipped
		input <- StreamElement{
			Video: &VideoData{
				Data:     []byte{}, // Empty data
				MIMEType: "video/h264",
			},
		}

		// Send valid audio element
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte("audio data"),
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

		// Verify only non-empty elements were forwarded
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		// Should have audio chunk but not empty video chunk
		require.Len(t, chunks, 1, "Expected only audio chunk (video with empty data should be skipped)")
	})

	t.Run("Skips image elements with empty data", func(t *testing.T) {
		provider := providersmock.NewStreamingProvider("test", "test-model", false).
			WithAutoRespond("Test response")
		stage := NewDuplexProviderStage(provider, baseConfig())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		input := make(chan StreamElement, 10)
		output := make(chan StreamElement, 10)

		// Send system prompt first
		input <- elementWithSystemPrompt("Test system prompt")

		// Send image element with empty data - should be skipped
		input <- StreamElement{
			Image: &ImageData{
				Data:     []byte{}, // Empty data
				MIMEType: "image/jpeg",
			},
		}

		// Send valid audio element
		input <- StreamElement{
			Audio: &AudioData{
				Samples:    []byte("audio data"),
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

		// Verify only non-empty elements were forwarded
		session := provider.GetSession()
		require.NotNil(t, session)
		chunks := session.GetChunks()
		require.Len(t, chunks, 1, "Expected only audio chunk (image with empty data should be skipped)")
	})
}

