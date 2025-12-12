package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/session"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// MockVADAnalyzer is a simple VAD implementation for demonstration
type MockVADAnalyzer struct {
	state        audio.VADState
	frameCount   int
	stateChannel chan audio.VADEvent
}

func NewMockVADAnalyzer() *MockVADAnalyzer {
	return &MockVADAnalyzer{
		state:        audio.VADStateQuiet,
		stateChannel: make(chan audio.VADEvent, 10),
	}
}

func (v *MockVADAnalyzer) Name() string {
	return "mock-vad"
}

func (v *MockVADAnalyzer) Analyze(_ context.Context, audioData []byte) (float64, error) {
	v.frameCount++
	oldState := v.state

	// Simulate speech detection: first 3 frames = speech, then silence
	if v.frameCount <= 3 {
		v.state = audio.VADStateSpeaking
		prob := 0.8

		// Emit state change event if transitioning to speaking
		if oldState != audio.VADStateSpeaking {
			v.stateChannel <- audio.VADEvent{
				State:      audio.VADStateSpeaking,
				PrevState:  oldState,
				Confidence: prob,
				Timestamp:  time.Now(),
			}
		}

		return prob, nil
	}

	v.state = audio.VADStateQuiet
	prob := 0.1

	// Emit state change event if transitioning to quiet
	if oldState != audio.VADStateQuiet {
		v.stateChannel <- audio.VADEvent{
			State:      audio.VADStateQuiet,
			PrevState:  oldState,
			Confidence: prob,
			Timestamp:  time.Now(),
		}
	}

	return prob, nil
}

func (v *MockVADAnalyzer) State() audio.VADState {
	return v.state
}

func (v *MockVADAnalyzer) OnStateChange() <-chan audio.VADEvent {
	return v.stateChannel
}

func (v *MockVADAnalyzer) Reset() {
	v.frameCount = 0
	v.state = audio.VADStateQuiet
}

// MockTranscriptionService transcribes audio for demonstration
type MockTranscriptionService struct{}

func (m *MockTranscriptionService) Transcribe(_ context.Context, audioData []byte) (string, error) {
	return fmt.Sprintf("User said: What's the weather?"), nil
}

// MockTTSService synthesizes audio for demonstration
type MockTTSService struct{}

func (m *MockTTSService) Synthesize(_ context.Context, text string) ([]byte, error) {
	audioData := []byte(fmt.Sprintf("[Audio: %s]", text))
	return audioData, nil
}

func (m *MockTTSService) MIMEType() string {
	return "audio/mpeg"
}

func main() {
	// Create context with reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use mock provider for demonstration (no API keys needed)
	provider := mock.NewProviderWithRepository("mock", "mock-model", false,
		mock.NewInMemoryMockRepository("This is a streaming response from the provider"))

	// Skip VAD for simplicity - just demonstrate Pipeline streaming with state and TTS
	stateStore := statestore.NewMemoryStore()
	stateStoreConfig := &pipeline.StateStoreConfig{
		Store:          stateStore,
		ConversationID: "demo-conversation",
	}
	stateLoadMiddleware := middleware.StateStoreLoadMiddleware(stateStoreConfig)
	stateSaveMiddleware := middleware.StateStoreSaveMiddleware(stateStoreConfig)

	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   1000,
		Temperature: 0.7,
	}
	providerMiddleware := middleware.ProviderMiddleware(provider, tools.NewRegistry(), nil, providerConfig)

	ttsService := &MockTTSService{}
	ttsConfig := middleware.TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 10,
	}
	ttsMiddleware := middleware.NewTTSMiddleware(ttsService, ttsConfig)

	// Build pipeline: Load state → Provider → Save state → TTS
	p := pipeline.NewPipeline(
		stateLoadMiddleware,
		providerMiddleware,
		stateSaveMiddleware,
		ttsMiddleware,
	)

	sess, err := session.NewBidirectionalSession(&session.BidirectionalConfig{
		ConversationID: "demo-conversation",
		Pipeline:       p,
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	defer sess.Close()

	fmt.Println("=== Pipeline Streaming Audio Demo ===")
	fmt.Println("Demonstrating: Provider → TTS → Audio Output")
	fmt.Println()

	// Send initial message to start the pipeline
	if err := sess.SendText(ctx, "Hello, tell me about streaming!"); err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// Read response chunks
	responseChan := sess.Response()
	chunkCount := 0
	textChunks := 0
	audioChunks := 0

	fmt.Println("Receiving response chunks:")
	for chunk := range responseChan {
		chunkCount++

		if chunk.Error != nil {
			log.Printf("✗ Error in chunk %d: %v", chunkCount, chunk.Error)
			break
		}

		if chunk.Delta != "" {
			textChunks++
			fmt.Printf("← Text chunk: \"%s\"\n", chunk.Delta)
		}

		if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil && len(*chunk.MediaDelta.Data) > 0 {
			audioChunks++
			fmt.Printf("← Audio chunk: %d bytes (%s)\n", len(*chunk.MediaDelta.Data), chunk.MediaDelta.MIMEType)
		}

		if chunk.FinishReason != nil {
			fmt.Printf("\n✓ Stream finished: %s\n", *chunk.FinishReason)
			break
		}
	}

	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Total chunks: %d\n", chunkCount)
	fmt.Printf("  Text chunks: %d\n", textChunks)
	fmt.Printf("  Audio chunks: %d\n", audioChunks)
	fmt.Println("\n✓ Pipeline streaming demonstration complete!")
}
