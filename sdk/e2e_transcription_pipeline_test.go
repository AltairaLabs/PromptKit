//go:build e2e

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Transcription Pipeline E2E Tests
//
// These tests verify the audio transcription pipeline that:
// 1. Takes audio input
// 2. Transcribes it using STT (OpenAI Whisper)
// 3. Passes the transcription to an LLM for processing
// 4. Verifies pipeline stage events are emitted correctly
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_TranscriptionPipeline
// =============================================================================

// eventCollector collects events for verification
type eventCollector struct {
	mu     sync.Mutex
	events []*events.Event
}

func newEventCollector() *eventCollector {
	return &eventCollector{
		events: make([]*events.Event, 0),
	}
}

func (c *eventCollector) collect(e *events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *eventCollector) getEvents() []*events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*events.Event, len(c.events))
	copy(result, c.events)
	return result
}

func (c *eventCollector) getEventTypes() []events.EventType {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]events.EventType, len(c.events))
	for i, e := range c.events {
		result[i] = e.Type
	}
	return result
}

// TestE2E_TranscriptionPipeline_WithStageEvents tests the full transcription pipeline
// and verifies that stage events are emitted correctly.
func TestE2E_TranscriptionPipeline_WithStageEvents(t *testing.T) {
	// Skip if OpenAI API key is not set (needed for Whisper)
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		t.Skip("OPENAI_API_KEY not set - skipping transcription pipeline test")
	}

	// Skip if audio file doesn't exist
	audioPath := filepath.Join(os.Getenv("HOME"), "Downloads", "03-02-01-01-01-02-01.wav")
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", audioPath)
	}

	// Set up event collection
	eventBus := events.NewEventBus()
	collector := newEventCollector()

	// Subscribe to all stage events
	eventBus.Subscribe(events.EventPipelineStarted, collector.collect)
	eventBus.Subscribe(events.EventPipelineCompleted, collector.collect)
	eventBus.Subscribe(events.EventPipelineFailed, collector.collect)
	eventBus.Subscribe(events.EventStageStarted, collector.collect)
	eventBus.Subscribe(events.EventStageCompleted, collector.collect)
	eventBus.Subscribe(events.EventStageFailed, collector.collect)
	eventBus.Subscribe(events.EventProviderCallStarted, collector.collect)
	eventBus.Subscribe(events.EventProviderCallCompleted, collector.collect)

	// Create event emitter
	emitter := events.NewEmitter(eventBus, "test-run", "test-session", "test-conversation")

	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	require.NoError(t, err, "Should read audio file")

	// Create STT service (OpenAI Whisper)
	sttService := stt.NewOpenAI(openAIKey)

	// Create STT stage
	sttStage := stage.NewSTTStage(sttService, stage.DefaultSTTStageConfig())

	// Create a stage that converts transcribed text to a message for the LLM
	textToMessageStage := stage.NewMapStage("text_to_message", func(elem stage.StreamElement) (stage.StreamElement, error) {
		if elem.Text != nil && *elem.Text != "" {
			// Create a user message with the transcription and a prompt
			prompt := "The user said: \"" + *elem.Text + "\"\n\nPlease respond briefly to what was said."
			msg := &types.Message{
				Role:    "user",
				Content: prompt,
			}
			return stage.NewMessageElement(msg), nil
		}
		return elem, nil
	})

	// Get an available provider for the LLM call
	cfg := LoadE2EConfig()
	var provider providers.Provider
	var providerConfig ProviderConfig

	// Find first available text provider
	for _, p := range cfg.Providers {
		if p.HasCapability(CapText) && p.ID != "mock" {
			providerConfig = p
			break
		}
	}

	if providerConfig.ID == "" {
		t.Skip("No text-capable provider available")
	}

	// Create provider using the factory
	provider, err = createProviderForTest(t, providerConfig)
	require.NoError(t, err, "Should create provider")

	// Create provider stage with event emitter
	providerStage := stage.NewProviderStageWithEmitter(
		provider,
		nil, // no tools
		nil, // no tool policy
		&stage.ProviderConfig{
			MaxTokens:   256,
			Temperature: 0.7,
		},
		emitter,
	)

	// Build the pipeline: STT -> TextToMessage -> Provider
	pipeline, err := stage.NewPipelineBuilder().
		WithEventEmitter(emitter).
		Chain(sttStage, textToMessageStage, providerStage).
		Build()
	require.NoError(t, err, "Should build pipeline")

	// Create input with audio data
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
	close(inputChan)

	// Execute pipeline
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	output, err := pipeline.Execute(ctx, inputChan)
	require.NoError(t, err, "Should execute pipeline")

	// Collect output
	var outputElements []stage.StreamElement
	for elem := range output {
		outputElements = append(outputElements, elem)
		if elem.Error != nil {
			t.Logf("Pipeline element error: %v", elem.Error)
		}
	}

	// Wait a bit for all events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify output
	require.NotEmpty(t, outputElements, "Should have output from pipeline")

	// Check for text/message content in output
	var hasContent bool
	var responseText string
	for _, elem := range outputElements {
		if elem.Text != nil && *elem.Text != "" {
			hasContent = true
			responseText += *elem.Text
		}
		if elem.Message != nil && elem.Message.Content != "" {
			hasContent = true
			responseText = elem.Message.Content
		}
	}
	assert.True(t, hasContent, "Should have response content from LLM")
	t.Logf("LLM response: %s", truncate(responseText, 200))

	// Verify events
	eventTypes := collector.getEventTypes()
	t.Logf("Collected events: %v", eventTypes)

	// Should have pipeline started
	assert.Contains(t, eventTypes, events.EventPipelineStarted, "Should emit pipeline.started")

	// Should have stage events for each stage
	stageStartedCount := 0
	stageCompletedCount := 0
	for _, et := range eventTypes {
		if et == events.EventStageStarted {
			stageStartedCount++
		}
		if et == events.EventStageCompleted {
			stageCompletedCount++
		}
	}
	assert.GreaterOrEqual(t, stageStartedCount, 3, "Should have at least 3 stage.started events (stt, text_to_message, provider)")
	assert.GreaterOrEqual(t, stageCompletedCount, 3, "Should have at least 3 stage.completed events")

	// Should have provider call events
	assert.Contains(t, eventTypes, events.EventProviderCallStarted, "Should emit provider.call.started")
	assert.Contains(t, eventTypes, events.EventProviderCallCompleted, "Should emit provider.call.completed")

	// Should have pipeline completed (not failed)
	assert.Contains(t, eventTypes, events.EventPipelineCompleted, "Should emit pipeline.completed")
	assert.NotContains(t, eventTypes, events.EventPipelineFailed, "Should not emit pipeline.failed")

	// Verify stage event details
	collectedEvents := collector.getEvents()
	for _, e := range collectedEvents {
		switch data := e.Data.(type) {
		case events.StageStartedData:
			t.Logf("Stage started: %s (type: %s)", data.Name, data.StageType)
		case events.StageCompletedData:
			t.Logf("Stage completed: %s (duration: %v)", data.Name, data.Duration)
		case events.ProviderCallCompletedData:
			t.Logf("Provider call completed: %s/%s (tokens: %d in, %d out, cost: $%.6f)",
				data.Provider, data.Model, data.InputTokens, data.OutputTokens, data.Cost)
		}
	}
}

// TestE2E_TranscriptionPipeline_STTOnly tests just the STT portion of the pipeline
func TestE2E_TranscriptionPipeline_STTOnly(t *testing.T) {
	// Skip if OpenAI API key is not set
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		t.Skip("OPENAI_API_KEY not set - skipping STT test")
	}

	// Use the shorter audio file
	audioPath := filepath.Join(os.Getenv("HOME"), "Downloads", "03-02-01-01-01-02-01.wav")
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", audioPath)
	}

	// Set up event collection
	eventBus := events.NewEventBus()
	collector := newEventCollector()

	eventBus.Subscribe(events.EventStageStarted, collector.collect)
	eventBus.Subscribe(events.EventStageCompleted, collector.collect)

	emitter := events.NewEmitter(eventBus, "test-run", "test-session", "test-conversation")

	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	require.NoError(t, err)

	// Create STT service and stage
	sttService := stt.NewOpenAI(openAIKey)
	sttStage := stage.NewSTTStage(sttService, stage.DefaultSTTStageConfig())

	// Build simple pipeline with just STT
	pipeline, err := stage.NewPipelineBuilder().
		WithEventEmitter(emitter).
		Chain(sttStage).
		Build()
	require.NoError(t, err)

	// Create input
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
	close(inputChan)

	// Execute
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := pipeline.Execute(ctx, inputChan)
	require.NoError(t, err)

	// Collect output
	var transcription string
	for elem := range output {
		if elem.Text != nil {
			transcription = *elem.Text
		}
		if elem.Error != nil {
			t.Fatalf("STT error: %v", elem.Error)
		}
	}

	// Wait for events
	time.Sleep(50 * time.Millisecond)

	// Verify transcription
	assert.NotEmpty(t, transcription, "Should have transcription output")
	t.Logf("Transcription: %s", transcription)

	// The RAVDESS audio file contains emotional speech - verify we got something reasonable
	lowerText := strings.ToLower(transcription)
	assert.True(t,
		strings.Contains(lowerText, "kids") ||
			strings.Contains(lowerText, "talking") ||
			strings.Contains(lowerText, "door") ||
			len(transcription) > 5,
		"Transcription should contain recognizable words, got: %s", transcription)

	// Verify stage events
	eventTypes := collector.getEventTypes()
	assert.Contains(t, eventTypes, events.EventStageStarted, "Should emit stage.started for STT")
	assert.Contains(t, eventTypes, events.EventStageCompleted, "Should emit stage.completed for STT")
}

// TestE2E_TranscriptionPipeline_LongAudio tests transcription of longer audio
func TestE2E_TranscriptionPipeline_LongAudio(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long audio test in short mode")
	}

	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	// Use the longer Harvard sentences audio
	audioPath := filepath.Join(os.Getenv("HOME"), "Downloads", "harvard.wav")
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", audioPath)
	}

	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	require.NoError(t, err)

	// Create STT service
	sttService := stt.NewOpenAI(openAIKey)

	// Transcribe directly (not through pipeline) to test STT service
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	transcription, err := sttService.Transcribe(ctx, audioData, stt.TranscriptionConfig{
		Format:     stt.FormatWAV,
		SampleRate: 16000,
		Channels:   1,
		Language:   "en",
	})
	require.NoError(t, err)

	// Verify transcription
	assert.NotEmpty(t, transcription)
	t.Logf("Long audio transcription (%d chars): %s", len(transcription), truncate(transcription, 300))

	// Harvard sentences should contain certain phrases
	lowerText := strings.ToLower(transcription)
	assert.True(t,
		strings.Contains(lowerText, "beer") ||
			strings.Contains(lowerText, "heat") ||
			strings.Contains(lowerText, "cold") ||
			strings.Contains(lowerText, "smell") ||
			len(transcription) > 50,
		"Should transcribe Harvard sentences")
}

// createProviderForTest creates a provider instance for testing based on the config
func createProviderForTest(t *testing.T, cfg ProviderConfig) (providers.Provider, error) {
	t.Helper()

	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1024,
	}

	switch cfg.ID {
	case "openai":
		if os.Getenv("OPENAI_API_KEY") == "" {
			return nil, nil
		}
		return openai.NewProvider("openai", cfg.DefaultModel, "https://api.openai.com/v1", defaults, false), nil

	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return nil, nil
		}
		return claude.NewProvider("anthropic", cfg.DefaultModel, "https://api.anthropic.com", defaults, false), nil

	case "gemini":
		if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
			return nil, nil
		}
		return gemini.NewProvider("gemini", cfg.DefaultModel, "https://generativelanguage.googleapis.com", defaults, false), nil

	default:
		return nil, nil
	}
}
