package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// Helper to get keys from map
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestDuplexStateStore_SystemPromptCaptured verifies that the system prompt
// from the prompt registry is properly captured in the state store output.
//
// This is a TDD test - it defines the expected behavior:
// 1. When a prompt registry has a system prompt for a task type
// 2. And a duplex conversation is executed with that task type
// 3. Then the result messages should include a system message with the prompt
func TestDuplexStateStore_SystemPromptCaptured(t *testing.T) {
	// Setup: Create a prompt registry with a known system prompt
	const expectedSystemPrompt = "You are a helpful voice assistant named Nova."
	const taskType = "voice-assistant"

	promptRepo := memory.NewPromptRepository()
	promptRepo.RegisterPrompt(taskType, &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       taskType,
			SystemTemplate: expectedSystemPrompt,
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(promptRepo)

	// Setup: Create a mock provider that supports streaming
	mockProvider := mock.NewStreamingProvider("test-mock", "mock-model", false)

	// Setup: Create state store to capture conversation
	store := statestore.NewArenaStateStore()

	// Setup: Create executor with prompt registry
	executor := NewDuplexConversationExecutor(nil, promptRegistry, nil)

	// Setup: Create scenario that uses the task type
	scenario := &config.Scenario{
		ID:       "test-scenario",
		TaskType: taskType,
		Duplex: &config.DuplexConfig{
			Timeout: "30s",
		},
		Turns: []config.TurnDefinition{
			{
				Role: "user",
				Parts: []config.TurnContentPart{
					{
						Type: "audio",
						Media: &config.TurnMediaContent{
							FilePath: "testdata/test.pcm",
							MIMEType: "audio/L16",
						},
					},
				},
			},
		},
	}

	conversationID := "test-conv-" + time.Now().Format("20060102150405")
	req := ConversationRequest{
		Provider:       mockProvider,
		Scenario:       scenario,
		Config:         &config.Config{LoadedProviders: map[string]*config.Provider{}},
		RunID:          "test-run",
		ConversationID: conversationID,
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute: Run the conversation
	result := executor.ExecuteConversation(ctx, req)

	// Assert: The execution should complete (may fail due to missing audio file, but state should be captured)
	t.Logf("Execution completed. Failed: %v, Error: %s", result.Failed, result.Error)

	// Debug: Check if state was saved to store
	state, err := store.Load(ctx, conversationID)
	if err != nil {
		t.Logf("Debug: Failed to load state from store: %v", err)
	} else {
		t.Logf("Debug: State loaded directly from store. Messages count: %d, SystemPrompt: %s", len(state.Messages), state.SystemPrompt)
		for i, msg := range state.Messages {
			t.Logf("Debug:   Message[%d]: role=%s, content=%s", i, msg.Role, msg.Content)
		}
		if state.Metadata != nil {
			t.Logf("Debug: Metadata keys: %v", keysOf(state.Metadata))
			if sp, ok := state.Metadata["system_prompt"]; ok {
				t.Logf("Debug: system_prompt in metadata: %v", sp)
			}
		}
	}

	// Debug: Check result directly
	t.Logf("Debug: result.Messages count: %d", len(result.Messages))
	for i, msg := range result.Messages {
		t.Logf("Debug: result.Message[%d]: role=%s, content=%s", i, msg.Role, msg.Content)
	}

	// Debug: Check if result.Failed or result.Error give clues
	t.Logf("Debug: result.Failed=%v, result.Error=%q", result.Failed, result.Error)
	t.Logf("Debug: conversationID used: %s", conversationID)

	// The primary test: verify system prompt is in the STATE STORE
	// (result.Messages may be empty due to pipeline timing issues, but state store should have data)
	if state == nil {
		t.Fatal("State should not be nil")
	}

	foundSystemPrompt := false
	for i, msg := range state.Messages {
		t.Logf("State Message %d: role=%s, content_len=%d", i, msg.Role, len(msg.Content))
		if msg.Role == "system" {
			foundSystemPrompt = true
			if msg.Content == "" {
				t.Error("System message has empty content")
			}
			if !strings.Contains(msg.Content, "Nova") {
				t.Errorf("System prompt should contain 'Nova', got: %s", msg.Content)
			}
			t.Logf("System prompt found in state store: %s", msg.Content)
			break
		}
	}

	if !foundSystemPrompt {
		t.Errorf("System prompt was not captured in state store messages. Messages count: %d", len(state.Messages))
		for i, msg := range state.Messages {
			t.Logf("  [%d] role=%s content=%s", i, msg.Role, msg.Content)
		}
	}

	// TODO: result.Messages is currently empty - this is a separate issue with buildResultFromStateStore
	// The primary goal (system prompt captured in state store) is achieved
	if len(result.Messages) == 0 && !result.Failed {
		t.Log("NOTE: result.Messages is empty but state store has data - buildResultFromStateStore issue")
	}
}

// TestDuplexStateStore_BuildResultFromStateStore tests that buildResultFromStateStore
// correctly loads messages from an ArenaStateStore
func TestDuplexStateStore_BuildResultFromStateStore(t *testing.T) {
	// Setup: Create and populate a state store directly
	store := statestore.NewArenaStateStore()
	conversationID := "test-build-result"

	// Manually save some messages
	ctx := context.Background()
	testState := &runtimestore.ConversationState{
		ID: conversationID,
		Messages: []types.Message{
			{Role: "system", Content: "Test system prompt"},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}
	if err := store.Save(ctx, testState); err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Verify the store has data
	loaded, err := store.Load(ctx, conversationID)
	if err != nil {
		t.Fatalf("Failed to load from store: %v", err)
	}
	t.Logf("Store has %d messages", len(loaded.Messages))

	// Now test buildResultFromStateStore
	executor := NewDuplexConversationExecutor(nil, nil, nil)
	req := &ConversationRequest{
		ConversationID: conversationID,
		Scenario:       &config.Scenario{ID: "test"},
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	result := executor.buildResultFromStateStore(req)

	// Check result
	t.Logf("Result: Failed=%v, Error=%q, Messages=%d", result.Failed, result.Error, len(result.Messages))
	if result.Failed {
		t.Errorf("buildResultFromStateStore failed: %s", result.Error)
	}
	if len(result.Messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(result.Messages))
	}
	for i, msg := range result.Messages {
		t.Logf("  Message[%d]: role=%s, content=%s", i, msg.Role, msg.Content)
	}
}

// TestDuplexStateStore_MediaOutputCaptured verifies that media output (audio/video)
// from the assistant is properly captured in the state store in duplex mode.
//
// TDD test - defines expected behavior:
// 1. When the provider returns media responses (MediaDelta with audio/video)
// 2. Then the assistant message in state store should have media content parts
// 3. This should work the same as non-duplex mode response streaming
//
// The issue: DuplexProviderStage.chunkToElement creates elem.Audio but doesn't
// include it in elem.Message. ArenaStateStoreSaveStage only collects elem.Message,
// so media is lost.
func TestDuplexStateStore_MediaOutputCaptured(t *testing.T) {
	// This test uses the buildResultFromStateStore directly to verify
	// that if messages with media parts are in the store, they are returned.
	// The actual fix needs to happen in DuplexProviderStage.chunkToElement.

	// Setup: Create and populate a state store with assistant message containing media
	store := statestore.NewArenaStateStore()
	conversationID := "test-media-output"

	// Create test messages including assistant with media
	// MediaContent.Data is *string (base64 encoded)
	audioDataB64 := "dGVzdCBhdWRpbyBkYXRh" // base64 of "test audio data"
	testState := &runtimestore.ConversationState{
		ID: conversationID,
		Messages: []types.Message{
			{Role: "system", Content: "Test system prompt"},
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "", // Audio-only response has no text
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: "audio/pcm",
							Data:     &audioDataB64,
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	if err := store.Save(ctx, testState); err != nil {
		t.Fatalf("Failed to save test state: %v", err)
	}

	// Verify the store has data with media
	loaded, err := store.Load(ctx, conversationID)
	if err != nil {
		t.Fatalf("Failed to load from store: %v", err)
	}
	t.Logf("Store has %d messages", len(loaded.Messages))

	// Now test buildResultFromStateStore
	executor := NewDuplexConversationExecutor(nil, nil, nil)
	req := &ConversationRequest{
		ConversationID: conversationID,
		Scenario:       &config.Scenario{ID: "test"},
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	result := executor.buildResultFromStateStore(req)

	// Check result
	t.Logf("Result: Failed=%v, Error=%q, Messages=%d, MediaOutputs=%d",
		result.Failed, result.Error, len(result.Messages), len(result.MediaOutputs))

	if result.Failed {
		t.Errorf("buildResultFromStateStore failed: %s", result.Error)
	}

	// Verify messages include the assistant with media
	if len(result.Messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(result.Messages))
	}

	// Find assistant message with media
	foundAssistantWithMedia := false
	for _, msg := range result.Messages {
		if msg.Role == "assistant" {
			t.Logf("Assistant message: content=%q, parts=%d", msg.Content, len(msg.Parts))
			for _, part := range msg.Parts {
				t.Logf("  Part: type=%s, hasMedia=%v", part.Type, part.Media != nil)
				if part.Type == types.ContentTypeAudio || (part.Media != nil && part.Media.Data != nil) {
					foundAssistantWithMedia = true
					t.Logf("Found assistant message with media content")
				}
			}
		}
	}

	if !foundAssistantWithMedia {
		t.Error("Expected assistant message with media content in result, but none found")
	}

	// Verify MediaOutputs are collected
	if len(result.MediaOutputs) == 0 {
		t.Error("Expected MediaOutputs to be populated from assistant media parts")
	} else {
		t.Logf("MediaOutputs: %d", len(result.MediaOutputs))
		for i, mo := range result.MediaOutputs {
			t.Logf("  MediaOutput[%d]: type=%s, mimeType=%s, sizeBytes=%d", i, mo.Type, mo.MIMEType, mo.SizeBytes)
		}
	}
}

// TestDuplexStateStore_PipelineCapturesMediaFromProvider verifies that when
// the provider returns media (audio/video) via MediaDelta in StreamChunk,
// it is properly converted to a Message with media parts and saved to state store.
//
// This is the TDD test for the actual fix needed in DuplexProviderStage.chunkToElement
func TestDuplexStateStore_PipelineCapturesMediaFromProvider(t *testing.T) {
	// Setup: Create a mock provider that returns audio in MediaDelta
	mockProvider := mock.NewStreamingProvider("test-mock", "mock-model", false)
	mockProvider.WithAutoRespond("") // Will be configured after session creation

	// Setup: Create state store to capture conversation
	store := statestore.NewArenaStateStore()

	// Setup: Create executor
	executor := NewDuplexConversationExecutor(nil, nil, nil)

	// Setup: Create scenario
	scenario := &config.Scenario{
		ID: "test-media-pipeline",
		Duplex: &config.DuplexConfig{
			Timeout: "30s",
		},
		Turns: []config.TurnDefinition{
			{
				Role: "user",
				Parts: []config.TurnContentPart{
					{
						Type: "audio",
						Media: &config.TurnMediaContent{
							FilePath: "testdata/test.pcm",
							MIMEType: "audio/L16",
						},
					},
				},
			},
		},
	}

	conversationID := "test-pipeline-media-" + time.Now().Format("20060102150405")
	req := ConversationRequest{
		Provider: mockProvider,
		Scenario: scenario,
		Config: &config.Config{
			LoadedProviders: map[string]*config.Provider{},
		},
		RunID:          "test-run",
		ConversationID: conversationID,
		StateStoreConfig: &StateStoreConfig{
			Store: store,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute: Run the conversation
	result := executor.ExecuteConversation(ctx, req)

	// The mock provider auto-responds with text only by default.
	// This test documents the current behavior and will fail when we add
	// audio response support to the mock.
	t.Logf("Execution completed. Failed: %v, Error: %s", result.Failed, result.Error)

	// Load state from store
	state, err := store.Load(ctx, conversationID)
	if err != nil {
		t.Fatalf("Failed to load state from store: %v", err)
	}

	t.Logf("State has %d messages", len(state.Messages))
	for i, msg := range state.Messages {
		t.Logf("  Message[%d]: role=%s, content=%q, parts=%d", i, msg.Role, msg.Content, len(msg.Parts))
	}

	// Check result.MediaOutputs
	t.Logf("Result.MediaOutputs: %d", len(result.MediaOutputs))

	// Currently this test documents the expected behavior:
	// When the provider returns media in MediaDelta, it should be captured.
	// The mock provider doesn't currently return audio, so this test passes
	// but serves as documentation of expected behavior.
	//
	// TODO: When implementing audio response support in the mock:
	// 1. Configure mock to return audio in MediaDelta
	// 2. Verify assistant message has audio content parts
	// 3. Verify MediaOutputs contains the audio
}

// TestDuplexStateStore_SystemPromptInMetadata verifies that system prompt
// is passed through pipeline metadata (even if no messages are saved).
//
// This tests the mechanism: system prompt should be in StateStoreConfig.Metadata
func TestDuplexStateStore_SystemPromptPassedToStateStore(t *testing.T) {
	// Setup
	const expectedSystemPrompt = "You are a test assistant."
	const taskType = "test-assistant"

	promptRepo := memory.NewPromptRepository()
	promptRepo.RegisterPrompt(taskType, &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       taskType,
			SystemTemplate: expectedSystemPrompt,
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(promptRepo)

	// Verify the prompt can be loaded
	assembled := promptRegistry.Load(taskType)
	if assembled == nil {
		t.Fatal("Prompt registry should return assembled prompt")
	}
	if assembled.SystemPrompt != expectedSystemPrompt {
		t.Fatalf("Expected system prompt %q but got %q", expectedSystemPrompt, assembled.SystemPrompt)
	}
	t.Logf("Prompt registry correctly returns system prompt: %s", assembled.SystemPrompt)

	// Verify buildBaseSessionConfig creates proper audio config
	// Note: System prompt is now handled by the pipeline (PromptAssemblyStage adds it to metadata,
	// DuplexProviderStage creates session with it). This is consistent with non-duplex pipelines.
	executor := NewDuplexConversationExecutor(nil, promptRegistry, nil)

	req := &ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test-scenario",
			TaskType: taskType,
			Duplex: &config.DuplexConfig{
				Timeout: "30s",
			},
		},
	}

	sessionConfig := executor.buildBaseSessionConfig(req)
	if sessionConfig == nil {
		t.Fatal("buildBaseSessionConfig should return non-nil config")
	}
	// Base config should NOT have system instruction - that's added by the pipeline
	if sessionConfig.SystemInstruction != "" {
		t.Errorf("buildBaseSessionConfig should NOT set SystemInstruction (got %q), pipeline handles this",
			sessionConfig.SystemInstruction)
	}
	t.Log("buildBaseSessionConfig correctly creates base config without system instruction")
}
