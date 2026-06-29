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

// elementWithSystemPrompt creates a placeholder stream element used to trigger
// the duplex provider stage's lazy session creation. The actual system prompt
// now lives on TurnState.SystemPrompt; tests that need a non-empty prompt
// must construct the stage via NewDuplexProviderStageWithTurnState.
func elementWithSystemPrompt(_ string) StreamElement {
	return StreamElement{}
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

		// Send test audio (16 bytes — must be PCM16-aligned to satisfy
		// MockStreamSession.SendChunk's alignment guard).
		audioData := []byte("test audio data!")
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

// TestDuplexProviderStage_MaterializesUserTurnAtAssistantResponse verifies the
// provider-agnostic fix: a buffered user transcript becomes a user Message when
// the assistant starts responding, even without a transcription_final marker
// (Gemini) and without a clean EndOfStream (barge-in). Drives handleResponseChunk
// directly with an input_transcription chunk followed by an assistant audio chunk.
func TestDuplexProviderStage_MaterializesUserTurnAtAssistantResponse(t *testing.T) {
	s := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	output := make(chan StreamElement, 8)
	ctx := context.Background()

	// User speech transcribed (no transcription_final → fast-path does NOT fire).
	require.NoError(t, s.handleResponseChunk(ctx, &providers.StreamChunk{
		Metadata: map[string]interface{}{"type": "input_transcription", "transcription": "tell me about bob dylan"},
	}, output))
	// No user Message yet — the input_transcription chunk forwards only an empty
	// passthrough element, not a user turn.
	for drained := false; !drained; {
		select {
		case e := <-output:
			require.Nil(t, e.Message, "user turn materialized before the assistant responded")
		default:
			drained = true
		}
	}

	// Assistant begins responding (audio) — user turn must materialize, ordered
	// before the audio element.
	require.NoError(t, s.handleResponseChunk(ctx, &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{Data: []byte{1, 2, 3, 4}, SampleRate: 24000, Channels: 1},
	}, output))

	var userMsg *types.Message
	var audioBeforeUser bool
	for done := false; !done; {
		select {
		case e := <-output:
			if e.Message != nil && e.Message.Role == "user" {
				userMsg = e.Message
			} else if e.Audio != nil && userMsg == nil {
				audioBeforeUser = true
			}
		default:
			done = true
		}
	}
	require.NotNil(t, userMsg, "user turn should materialize when the assistant responds")
	assert.Equal(t, "tell me about bob dylan", userMsg.Content)
	assert.False(t, audioBeforeUser, "user Message must be emitted before the assistant audio")
}

// TestDuplexProviderStage_UserTurnMaterializesOncePerUtterance verifies the
// buffer resets after materializing, so subsequent assistant content in the same
// response does not re-emit the user turn.
func TestDuplexProviderStage_UserTurnMaterializesOncePerUtterance(t *testing.T) {
	s := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	output := make(chan StreamElement, 8)
	ctx := context.Background()

	require.NoError(t, s.handleResponseChunk(ctx, &providers.StreamChunk{
		Metadata: map[string]interface{}{"type": "input_transcription", "transcription": "hi"},
	}, output))
	audio := &providers.StreamChunk{MediaData: &providers.StreamMediaData{Data: []byte{1, 2}, SampleRate: 24000, Channels: 1}}
	require.NoError(t, s.handleResponseChunk(ctx, audio, output))
	require.NoError(t, s.handleResponseChunk(ctx, audio, output))

	userMsgs := 0
	for {
		select {
		case e := <-output:
			if e.Message != nil && e.Message.Role == "user" {
				userMsgs++
			}
			continue
		default:
		}
		break
	}
	assert.Equal(t, 1, userMsgs, "user turn should materialize exactly once per utterance")
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

		// Send EndOfStream marker to allow bidirectional streaming to start
		// (the drain goroutine waits for EndOfStream before exiting)
		input <- StreamElement{
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
			if elem.Meta.InterruptedTurnComplete {
				hasInterruptedTurnComplete = true
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

		// Send EndOfStream marker to allow bidirectional streaming to start
		// (the drain goroutine waits for EndOfStream before exiting)
		input <- StreamElement{
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
			if elem.Meta.InterruptedTurnComplete {
				hasInterruptedTurnComplete = true
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

// chunkInjectingProvider wraps *mock.StreamingProvider and injects a fixed
// sequence of response chunks (including input_transcription metadata) into each
// session at CreateStreamSession time. This lets the streaming-materialization
// tests drive an exact StreamChunk sequence without modifying the mock package.
type chunkInjectingProvider struct {
	*providersmock.StreamingProvider
	chunks []providers.StreamChunk
}

func (p *chunkInjectingProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess, err := p.StreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if mockSess, ok := sess.(*providersmock.MockStreamSession); ok {
		mockSess.WithResponseChunks(p.chunks)
	}
	return sess, nil
}

// runStreamingTranscriptMaterialization drives DuplexProviderStage with a
// NO-turn_id input sequence (audio + EndOfStream) and a mock session that emits
// an input_transcription chunk followed by an assistant chunk carrying a
// FinishReason. It returns the ordered output elements so callers can assert
// that a user Message materialises from the transcript BEFORE the assistant
// message — proving the provider-agnostic streaming release path.
//
// transcript is the user-speech text the provider reports; assistantText is the
// model's reply. Both OpenAI Realtime and Gemini Live emit the identical
// normalized input_transcription chunk shape, so this single helper exercises
// the shared release logic for both providers.
func runStreamingTranscriptMaterialization(
	t *testing.T, transcript, assistantText string,
) []StreamElement {
	t.Helper()

	finishReason := "stop"
	chunks := []providers.StreamChunk{
		{
			Metadata: map[string]interface{}{
				"type":          "input_transcription",
				"transcription": transcript,
			},
		},
		{
			Content:      assistantText,
			Delta:        assistantText,
			FinishReason: &finishReason,
		},
	}

	inner := providersmock.NewStreamingProvider("test-stream", "mock-model", false)
	inner.WithAutoRespond("ok")
	inner.WithCloseAfterTurns(1)
	provider := &chunkInjectingProvider{StreamingProvider: inner, chunks: chunks}

	st := NewDuplexProviderStage(provider, baseConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan StreamElement, 4)
	output := make(chan StreamElement, 16)

	// NO pre-created user Message / turn_id — this is the continuous streaming
	// (interactive console / SDK realtime) path. Just audio then EndOfStream.
	input <- StreamElement{
		Audio: &AudioData{
			Samples:    []byte("audio data here!"),
			SampleRate: 16000,
			Format:     AudioFormatPCM16,
		},
	}
	input <- StreamElement{EndOfStream: true}
	close(input)

	done := make(chan error, 1)
	go func() { done <- st.Process(ctx, input, output) }()

	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}
	require.NoError(t, <-done)
	return elements
}

// assertUserTranscriptBeforeAssistant asserts that a user Message materialised
// from the input transcript appears in the output BEFORE the assistant message.
func assertUserTranscriptBeforeAssistant(
	t *testing.T, elements []StreamElement, transcript, assistantText string,
) {
	t.Helper()

	userIdx, assistantIdx := -1, -1
	for i := range elements {
		msg := elements[i].Message
		if msg == nil {
			continue
		}
		switch msg.Role {
		case roleUser:
			if msg.Content == transcript {
				userIdx = i
			}
		case roleAssistant:
			if msg.Content == assistantText {
				assistantIdx = i
			}
		}
	}

	require.GreaterOrEqual(t, userIdx, 0,
		"expected a user Message with the transcript content to be emitted")
	require.GreaterOrEqual(t, assistantIdx, 0,
		"expected the assistant Message to still flow to output")
	assert.Less(t, userIdx, assistantIdx,
		"user transcript message must be ordered BEFORE the assistant message")
}

// runStreamingTranscriptFastPath drives DuplexProviderStage with a NO-turn_id
// input sequence and a mock session that emits an input_transcription chunk
// carrying transcription_final=true (the full final user transcript) FOLLOWED by
// streaming assistant content and a much later assistant FinishReason. It returns
// the ordered output elements so callers can assert the user Message materialises
// from the FINAL transcript marker — BEFORE the assistant turn completes — rather
// than waiting for EndOfStream.
//
// The assistant FinishReason chunk arrives last; if the user turn only
// materialised at EndOfStream (the fallback path), the user Message would appear
// at/after the assistant element. The fast path must emit it immediately on the
// transcription_final chunk, ahead of the assistant content.
func runStreamingTranscriptFastPath(
	t *testing.T, transcript, assistantText string,
) []StreamElement {
	t.Helper()

	finishReason := "stop"
	chunks := []providers.StreamChunk{
		// FINAL input transcription marker — full user transcript, emitted the
		// moment the user stops speaking (well before the assistant responds).
		{
			Metadata: map[string]interface{}{
				"type":                "input_transcription",
				"transcription":       transcript,
				"transcription_final": true,
			},
		},
		// Assistant streaming content (no FinishReason yet).
		{
			Content: assistantText,
			Delta:   assistantText,
		},
		// Assistant turn completion arrives much later.
		{
			FinishReason: &finishReason,
		},
	}

	inner := providersmock.NewStreamingProvider("test-stream", "mock-model", false)
	inner.WithAutoRespond("ok")
	inner.WithCloseAfterTurns(1)
	provider := &chunkInjectingProvider{StreamingProvider: inner, chunks: chunks}

	st := NewDuplexProviderStage(provider, baseConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan StreamElement, 4)
	output := make(chan StreamElement, 16)

	input <- StreamElement{
		Audio: &AudioData{
			Samples:    []byte("audio data here!"),
			SampleRate: 16000,
			Format:     AudioFormatPCM16,
		},
	}
	input <- StreamElement{EndOfStream: true}
	close(input)

	done := make(chan error, 1)
	go func() { done <- st.Process(ctx, input, output) }()

	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}
	require.NoError(t, <-done)
	return elements
}

// countUserMessages returns the number of user Messages in the output whose
// content equals transcript. Used to assert the fast path and the EndOfStream
// fallback never both fire for the same transcript (no double-emit).
func countUserMessages(elements []StreamElement, transcript string) int {
	n := 0
	for i := range elements {
		msg := elements[i].Message
		if msg != nil && msg.Role == roleUser && msg.Content == transcript {
			n++
		}
	}
	return n
}

// TestDuplexProviderStage_StreamingFastPathMaterializesUserTurn verifies the
// fast path: when an input_transcription chunk carries transcription_final=true
// (no turn_id queued, non-empty transcript), the user Message is emitted
// IMMEDIATELY — before the assistant turn's FinishReason/EndOfStream element —
// and exactly once (no double-emit at EndOfStream).
func TestDuplexProviderStage_StreamingFastPathMaterializesUserTurn(t *testing.T) {
	const transcript = "hello from the user"
	const assistantText = "hi, how can I help?"
	elements := runStreamingTranscriptFastPath(t, transcript, assistantText)

	// User message ordered before the assistant message.
	assertUserTranscriptBeforeAssistant(t, elements, transcript, assistantText)

	// Fast path proof: the user turn must appear BEFORE the assistant's streaming
	// content element (the one carrying assistant Text). The fallback only emits
	// at EndOfStream — i.e. AFTER all assistant content has streamed through — so
	// this ordering is unique to the fast path firing on the transcription_final
	// chunk.
	userIdx, assistantContentIdx := -1, -1
	for i := range elements {
		if elements[i].Message != nil &&
			elements[i].Message.Role == roleUser &&
			elements[i].Message.Content == transcript {
			userIdx = i
		}
		if assistantContentIdx == -1 && elements[i].Text != nil && *elements[i].Text == assistantText {
			assistantContentIdx = i
		}
	}
	require.GreaterOrEqual(t, userIdx, 0, "expected a user Message from the final transcript")
	require.GreaterOrEqual(t, assistantContentIdx, 0,
		"expected the assistant streaming content element to flow to output")
	assert.Less(t, userIdx, assistantContentIdx,
		"fast path must emit the user turn BEFORE assistant content streams, not wait for EndOfStream")

	// No double-emit: the EndOfStream fallback must not re-emit the same turn.
	assert.Equal(t, 1, countUserMessages(elements, transcript),
		"user transcript must materialise exactly once (fast path, not also fallback)")
}

// TestDuplexProviderStage_StreamingMaterializesUserTurn verifies the
// continuous-streaming (no pre-created turn_id) FALLBACK case: when the provider
// reports an input_transcription WITHOUT the transcription_final marker and the
// turn completes, the stage materialises a user Message from the transcript at
// EndOfStream and orders it before the assistant message. This covers providers
// (e.g. Gemini Live) that never send the marker.
//
// This is PROVIDER-AGNOSTIC: both subtests use the identical normalized chunk
// shape that OpenAI Realtime and Gemini Live both emit, so a single release path
// covers both providers.
func TestDuplexProviderStage_StreamingMaterializesUserTurn(t *testing.T) {
	t.Run("OpenAI Realtime path", func(t *testing.T) {
		const transcript = "hello from the user"
		const assistantText = "hi, how can I help?"
		elements := runStreamingTranscriptMaterialization(t, transcript, assistantText)
		assertUserTranscriptBeforeAssistant(t, elements, transcript, assistantText)
	})

	// Gemini Live emits the SAME normalized input_transcription chunk
	// (Metadata{"type":"input_transcription","transcription":...}), so the shared
	// release path materialises the user turn identically — no per-provider code.
	t.Run("Gemini Live path", func(t *testing.T) {
		const transcript = "what is the weather today"
		const assistantText = "it is sunny"
		elements := runStreamingTranscriptMaterialization(t, transcript, assistantText)
		assertUserTranscriptBeforeAssistant(t, elements, transcript, assistantText)
	})
}
