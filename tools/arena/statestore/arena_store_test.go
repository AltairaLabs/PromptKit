package statestore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArenaStateStore_SaveAndLoad(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &statestore.ConversationState{
		ID:           "conv-123",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
		},
		TokenCount: 1,
		Metadata:   map[string]interface{}{"test": "value"},
	}

	// Save
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	assert.Equal(t, "conv-123", loaded.ID)
	assert.Equal(t, "user-alice", loaded.UserID)
	assert.Equal(t, "You are a helpful assistant", loaded.SystemPrompt)
	assert.Len(t, loaded.Messages, 1)
	assert.Equal(t, "Hello", loaded.Messages[0].Content)
	assert.Equal(t, "value", loaded.Metadata["test"])
}

func TestArenaStateStore_DeepCloneMessageMeta(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create state with message containing Meta field (simulating assertions)
	assertionResults := map[string]interface{}{
		"content_includes": map[string]interface{}{
			"ok":      true,
			"details": map[string]interface{}{"matched": true},
		},
		"content_matches": map[string]interface{}{
			"ok":      true,
			"details": map[string]interface{}{"pattern": ".*hello.*"},
		},
	}

	state := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
			{
				Role:      "assistant",
				Content:   "Hello! How can I help you?",
				Timestamp: time.Now(),
				Meta: map[string]interface{}{
					"assertions": assertionResults,
					"other_data": "some value",
				},
			},
		},
	}

	// Save the state
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Modify the original message Meta after saving
	state.Messages[1].Meta["assertions"] = map[string]interface{}{
		"modified": true,
	}
	state.Messages[1].Meta["new_key"] = "new value"

	// Load the state
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Verify the loaded state has the original Meta, not the modified version
	require.Len(t, loaded.Messages, 2)
	assistantMsg := loaded.Messages[1]

	// Check that assertions are preserved
	require.NotNil(t, assistantMsg.Meta)
	assertions, ok := assistantMsg.Meta["assertions"]
	require.True(t, ok, "assertions key should exist in Meta")

	assertionsMap, ok := assertions.(map[string]interface{})
	require.True(t, ok, "assertions should be a map")

	// Verify content_includes assertion
	contentIncludes, ok := assertionsMap["content_includes"]
	require.True(t, ok, "content_includes should exist")
	contentIncludesMap := contentIncludes.(map[string]interface{})
	assert.Equal(t, true, contentIncludesMap["ok"])

	// Verify content_matches assertion
	contentMatches, ok := assertionsMap["content_matches"]
	require.True(t, ok, "content_matches should exist")
	contentMatchesMap := contentMatches.(map[string]interface{})
	assert.Equal(t, true, contentMatchesMap["ok"])

	// Verify other_data is preserved
	assert.Equal(t, "some value", assistantMsg.Meta["other_data"])

	// Verify modified keys are NOT present
	_, hasModified := assertionsMap["modified"]
	assert.False(t, hasModified, "modified key should not exist")
	_, hasNewKey := assistantMsg.Meta["new_key"]
	assert.False(t, hasNewKey, "new_key should not exist")
}

func TestArenaStateStore_DeepCloneNestedStructures(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create complex nested structure in Meta
	toolCallArgs, _ := json.Marshal(map[string]interface{}{
		"query": "test search",
		"limit": 10,
	})

	state := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:      "assistant",
				Content:   "I'll search for that.",
				Timestamp: time.Now(),
				ToolCalls: []types.MessageToolCall{
					{
						ID:   "call-1",
						Name: "search",
						Args: toolCallArgs,
					},
				},
				CostInfo: &types.CostInfo{
					InputTokens:   10,
					OutputTokens:  5,
					InputCostUSD:  0.0001,
					OutputCostUSD: 0.0002,
					TotalCost:     0.0003,
				},
				Validations: []types.ValidationResult{
					{
						ValidatorType: "banned_words",
						Passed:        true,
						Details: map[string]interface{}{
							"checked_words": []string{"word1", "word2"},
						},
						Timestamp: time.Now(),
					},
				},
				Meta: map[string]interface{}{
					"nested": map[string]interface{}{
						"level2": map[string]interface{}{
							"level3": "deep value",
						},
					},
					"array": []interface{}{"item1", "item2"},
				},
			},
		},
	}

	// Save
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Modify original after save
	state.Messages[0].ToolCalls[0].Name = "modified"
	state.Messages[0].CostInfo.InputTokens = 999
	state.Messages[0].Validations[0].Passed = false
	state.Messages[0].Meta["nested"].(map[string]interface{})["level2"].(map[string]interface{})["level3"] = "modified"
	state.Messages[0].Meta["array"].([]interface{})[0] = "modified"

	// Load and verify original values are preserved
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	msg := loaded.Messages[0]

	// Verify ToolCalls preserved
	assert.Equal(t, "search", msg.ToolCalls[0].Name)

	// Verify CostInfo preserved
	assert.Equal(t, 10, msg.CostInfo.InputTokens)
	assert.Equal(t, 0.0003, msg.CostInfo.TotalCost)

	// Verify Validations preserved
	assert.True(t, msg.Validations[0].Passed)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)

	// Verify nested Meta preserved
	nested := msg.Meta["nested"].(map[string]interface{})
	level2 := nested["level2"].(map[string]interface{})
	assert.Equal(t, "deep value", level2["level3"])

	// Verify array preserved
	arr := msg.Meta["array"].([]interface{})
	assert.Equal(t, "item1", arr[0])
}

func TestArenaStateStore_DeepCloneMultipleSaves(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Initial state
	state := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "First message",
			},
		},
	}

	// Save initial state
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Add message with assertions
	state.Messages = append(state.Messages, types.Message{
		Role:    "assistant",
		Content: "First response",
		Meta: map[string]interface{}{
			"assertions": map[string]interface{}{
				"turn": 1,
			},
		},
	})

	// Save updated state
	err = store.Save(ctx, state)
	require.NoError(t, err)

	// Add another turn
	state.Messages = append(state.Messages, types.Message{
		Role:    "user",
		Content: "Second message",
	})
	state.Messages = append(state.Messages, types.Message{
		Role:    "assistant",
		Content: "Second response",
		Meta: map[string]interface{}{
			"assertions": map[string]interface{}{
				"turn": 2,
			},
		},
	})

	// Save final state
	err = store.Save(ctx, state)
	require.NoError(t, err)

	// Load and verify all messages preserved
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	require.Len(t, loaded.Messages, 4)

	// Check first assistant message
	assert.Equal(t, "assistant", loaded.Messages[1].Role)
	assert.NotNil(t, loaded.Messages[1].Meta)
	assertions1 := loaded.Messages[1].Meta["assertions"].(map[string]interface{})
	// Our custom deep clone preserves types, so it's int not float64
	assert.Equal(t, 1, assertions1["turn"])

	// Check second assistant message
	assert.Equal(t, "assistant", loaded.Messages[3].Role)
	assert.NotNil(t, loaded.Messages[3].Meta)
	assertions2 := loaded.Messages[3].Meta["assertions"].(map[string]interface{})
	assert.Equal(t, 2, assertions2["turn"])
}

func TestArenaStateStore_GetArenaState(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Get arena state
	arenaState, err := store.GetArenaState(ctx, "conv-123")
	require.NoError(t, err)
	assert.Equal(t, "conv-123", arenaState.ID)
	assert.Len(t, arenaState.Messages, 1)
}

func TestArenaStateStore_DumpToJSON(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "Response",
				Meta: map[string]interface{}{
					"assertions": map[string]interface{}{
						"passed": true,
					},
				},
			},
		},
		Metadata: map[string]interface{}{
			"test": "value",
		},
	}

	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Dump to JSON
	data, err := store.DumpToJSON(ctx, "conv-123")
	require.NoError(t, err)

	// Verify JSON structure
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "conv-123", result["conversation_id"])
	messages := result["messages"].([]interface{})
	assert.Len(t, messages, 1)

	// Verify Meta field is in JSON output
	msg := messages[0].(map[string]interface{})
	meta := msg["meta"].(map[string]interface{})
	assertions := meta["assertions"].(map[string]interface{})
	assert.Equal(t, true, assertions["passed"])
}

// TestArenaStateStore_SaveRunMetadata tests saving run metadata
func TestArenaStateStore_SaveRunMetadata(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// First save conversation state
	state := &statestore.ConversationState{
		ID:     "run-123",
		UserID: "test-user",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Save run metadata
	startTime := time.Now().Add(-5 * time.Second)
	endTime := time.Now()
	metadata := &RunMetadata{
		RunID:      "run-123",
		Region:     "us-west",
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Params: map[string]interface{}{
			"temperature": 0.7,
		},
		StartTime: startTime,
		EndTime:   endTime,
		Duration:  endTime.Sub(startTime),
		SelfPlay:  true,
		PersonaID: "customer",
	}

	err = store.SaveRunMetadata(ctx, "run-123", metadata)
	require.NoError(t, err)

	// Retrieve and verify
	arenaState, err := store.GetArenaState(ctx, "run-123")
	require.NoError(t, err)
	require.NotNil(t, arenaState.RunMetadata)

	assert.Equal(t, "run-123", arenaState.RunMetadata.RunID)
	assert.Equal(t, "us-west", arenaState.RunMetadata.Region)
	assert.Equal(t, "test-scenario", arenaState.RunMetadata.ScenarioID)
	assert.Equal(t, "test-provider", arenaState.RunMetadata.ProviderID)
	assert.Equal(t, 0.7, arenaState.RunMetadata.Params["temperature"])
	assert.True(t, arenaState.RunMetadata.SelfPlay)
	assert.Equal(t, "customer", arenaState.RunMetadata.PersonaID)
}

// TestArenaStateStore_SaveRunMetadata_WithError tests saving metadata with error
func TestArenaStateStore_SaveRunMetadata_WithError(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	metadata := &RunMetadata{
		RunID:      "run-error",
		Region:     "us-east",
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Duration:   time.Second,
		Error:      "provider not found: test-provider",
	}

	// Should save even without conversation state (for early failures)
	err := store.SaveRunMetadata(ctx, "run-error", metadata)
	require.NoError(t, err)

	// Retrieve and verify error is stored
	arenaState, err := store.GetArenaState(ctx, "run-error")
	require.NoError(t, err)
	require.NotNil(t, arenaState.RunMetadata)
	assert.Equal(t, "provider not found: test-provider", arenaState.RunMetadata.Error)
}

// TestArenaStateStore_GetRunResult tests reconstructing RunResult
func TestArenaStateStore_GetRunResult(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Save conversation state with messages and cost info
	state := &statestore.ConversationState{
		ID:     "run-123",
		UserID: "test-user",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
			{
				Role:    "assistant",
				Content: "Hi there",
				CostInfo: &types.CostInfo{
					InputTokens:  10,
					OutputTokens: 20,
					TotalCost:    0.001,
				},
				ToolCalls: []types.MessageToolCall{
					{ID: "call-1", Name: "search"},
				},
				Validations: []types.ValidationResult{
					{
						ValidatorType: "length",
						Passed:        false,
						Details:       map[string]interface{}{"max": 100, "actual": 150},
					},
				},
			},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Save run metadata
	startTime := time.Now().Add(-5 * time.Second)
	endTime := time.Now()
	metadata := &RunMetadata{
		RunID:      "run-123",
		Region:     "us-west",
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		StartTime:  startTime,
		EndTime:    endTime,
		Duration:   endTime.Sub(startTime),
	}
	err = store.SaveRunMetadata(ctx, "run-123", metadata)
	require.NoError(t, err)

	// Get reconstructed RunResult
	result, err := store.GetRunResult(ctx, "run-123")
	require.NoError(t, err)

	// Verify metadata fields
	assert.Equal(t, "run-123", result.RunID)
	assert.Equal(t, "us-west", result.Region)
	assert.Equal(t, "test-scenario", result.ScenarioID)
	assert.Equal(t, "test-provider", result.ProviderID)
	assert.Equal(t, startTime, result.StartTime)
	assert.Equal(t, endTime, result.EndTime)

	// Verify messages
	assert.Len(t, result.Messages, 2)
	assert.Equal(t, "Hello", result.Messages[0].Content)
	assert.Equal(t, "Hi there", result.Messages[1].Content)

	// Verify computed cost
	assert.Equal(t, 10, result.Cost.InputTokens)
	assert.Equal(t, 20, result.Cost.OutputTokens)
	assert.Equal(t, 0.001, result.Cost.TotalCost)

	// Verify computed tool stats
	require.NotNil(t, result.ToolStats)
	assert.Equal(t, 1, result.ToolStats.TotalCalls)
	assert.Equal(t, 1, result.ToolStats.ByTool["search"])

	// Verify violations
	assert.Len(t, result.Violations, 1)
	assert.Equal(t, "length", result.Violations[0].Type)
}

// TestArenaStateStore_GetRunResult_NoMetadata tests error when metadata missing
func TestArenaStateStore_GetRunResult_NoMetadata(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Save only conversation state, no metadata
	state := &statestore.ConversationState{
		ID:       "run-no-meta",
		UserID:   "test-user",
		Messages: []types.Message{{Role: "user", Content: "Hello"}},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Should fail to get RunResult without metadata
	_, err = store.GetRunResult(ctx, "run-no-meta")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run metadata not found")
}

// TestArenaStateStore_ListRunIDs tests listing all run IDs
func TestArenaStateStore_ListRunIDs(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Save multiple runs
	runs := []string{"run-1", "run-2", "run-3"}
	for _, runID := range runs {
		state := &statestore.ConversationState{
			ID:       runID,
			Messages: []types.Message{{Role: "user", Content: "test"}},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		metadata := &RunMetadata{
			RunID:      runID,
			ScenarioID: "test",
			ProviderID: "test",
		}
		err = store.SaveRunMetadata(ctx, runID, metadata)
		require.NoError(t, err)
	}

	// List all run IDs
	runIDs, err := store.ListRunIDs(ctx)
	require.NoError(t, err)
	assert.Len(t, runIDs, 3)

	// Verify all IDs present (order may vary)
	idMap := make(map[string]bool)
	for _, id := range runIDs {
		idMap[id] = true
	}
	for _, expectedID := range runs {
		assert.True(t, idMap[expectedID], "Expected %s to be in list", expectedID)
	}
}

// TestArenaStateStore_DumpToJSON_WithMetadata tests JSON export with metadata
func TestArenaStateStore_DumpToJSON_WithMetadata(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Save conversation state
	state := &statestore.ConversationState{
		ID:     "run-123",
		UserID: "test-user",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "Hi",
				CostInfo: &types.CostInfo{
					InputTokens:  10,
					OutputTokens: 5,
					TotalCost:    0.0015,
				},
			},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Save run metadata
	metadata := &RunMetadata{
		RunID:      "run-123",
		Region:     "us-west",
		ScenarioID: "test-scenario",
		ProviderID: "gpt-4",
		StartTime:  time.Now().Add(-5 * time.Second),
		EndTime:    time.Now(),
		Duration:   5 * time.Second,
	}
	err = store.SaveRunMetadata(ctx, "run-123", metadata)
	require.NoError(t, err)

	// Dump to JSON
	jsonData, err := store.DumpToJSON(ctx, "run-123")
	require.NoError(t, err)

	// Debug: print JSON
	t.Logf("JSON output: %s", string(jsonData))

	// Parse and verify
	var result map[string]interface{}
	err = json.Unmarshal(jsonData, &result)
	require.NoError(t, err)

	// Verify RunResult-compatible structure
	assert.Equal(t, "run-123", result["RunID"])
	assert.Equal(t, "us-west", result["Region"])
	assert.Equal(t, "test-scenario", result["ScenarioID"])
	assert.Equal(t, "gpt-4", result["ProviderID"])

	// Verify messages
	messages := result["Messages"].([]interface{})
	assert.Len(t, messages, 2)

	// Verify computed cost (uses snake_case from types.CostInfo JSON tags)
	cost := result["Cost"].(map[string]interface{})
	assert.Equal(t, float64(10), cost["input_tokens"])
	assert.Equal(t, float64(5), cost["output_tokens"])
	assert.Equal(t, 0.0015, cost["total_cost_usd"])
}

func TestArenaStateStore_MultimodalContentPreservation(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create a message with multimodal Parts
	textPtr := "What's in this image?"
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	imageURL := "data:image/png;base64," + imageData
	detailAuto := "auto"

	state := &statestore.ConversationState{
		ID:     "conv-multimodal",
		UserID: "user-test",
		Messages: []types.Message{
			{
				Role:      "user",
				Timestamp: time.Now(),
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: &textPtr,
					},
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      &imageURL,
							MIMEType: types.MIMETypeImagePNG,
							Detail:   &detailAuto,
						},
					},
				},
			},
			{
				Role:      "assistant",
				Content:   "I see a small test image.",
				Timestamp: time.Now(),
			},
		},
	}

	// Save the state
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load and verify Parts are preserved
	loaded, err := store.Load(ctx, "conv-multimodal")
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 2)

	// Check first message has Parts
	userMsg := loaded.Messages[0]
	assert.Equal(t, "user", userMsg.Role)
	require.Len(t, userMsg.Parts, 2, "Parts should be preserved after save/load")

	// Verify text part
	assert.Equal(t, types.ContentTypeText, userMsg.Parts[0].Type)
	require.NotNil(t, userMsg.Parts[0].Text)
	assert.Equal(t, "What's in this image?", *userMsg.Parts[0].Text)

	// Verify image part
	assert.Equal(t, types.ContentTypeImage, userMsg.Parts[1].Type)
	require.NotNil(t, userMsg.Parts[1].Media)
	require.NotNil(t, userMsg.Parts[1].Media.URL)
	assert.Contains(t, *userMsg.Parts[1].Media.URL, imageData)
	assert.Equal(t, types.MIMETypeImagePNG, userMsg.Parts[1].Media.MIMEType)
	require.NotNil(t, userMsg.Parts[1].Media.Detail)
	assert.Equal(t, "auto", *userMsg.Parts[1].Media.Detail)

	// Verify assistant message
	assistantMsg := loaded.Messages[1]
	assert.Equal(t, "assistant", assistantMsg.Role)
	assert.Equal(t, "I see a small test image.", assistantMsg.Content)
	assert.Len(t, assistantMsg.Parts, 0, "Assistant message should have no Parts")
}

func TestArenaStateStore_MultimodalContentCloning(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create initial state with one multimodal message
	textPtr := "First message"
	state := &statestore.ConversationState{
		ID:     "conv-clone-test",
		UserID: "user-test",
		Messages: []types.Message{
			{
				Role:      "user",
				Timestamp: time.Now(),
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: &textPtr,
					},
				},
			},
		},
	}

	// Save first turn
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load and add another message
	loaded, err := store.Load(ctx, "conv-clone-test")
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 1)
	require.Len(t, loaded.Messages[0].Parts, 1, "First message should have Parts")

	// Add a second multimodal message
	secondTextPtr := "Second message"
	loaded.Messages = append(loaded.Messages, types.Message{
		Role:      "assistant",
		Content:   "Response to first",
		Timestamp: time.Now(),
	})
	loaded.Messages = append(loaded.Messages, types.Message{
		Role:      "user",
		Timestamp: time.Now(),
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &secondTextPtr,
			},
		},
	})

	// Save second turn
	err = store.Save(ctx, loaded)
	require.NoError(t, err)

	// Load again and verify all Parts are preserved
	final, err := store.Load(ctx, "conv-clone-test")
	require.NoError(t, err)
	require.Len(t, final.Messages, 3)

	// Verify first user message still has Parts
	assert.Len(t, final.Messages[0].Parts, 1, "First message Parts should be preserved across multiple saves")
	assert.Equal(t, "First message", *final.Messages[0].Parts[0].Text)

	// Verify assistant message has no Parts
	assert.Len(t, final.Messages[1].Parts, 0)
	assert.Equal(t, "Response to first", final.Messages[1].Content)

	// Verify second user message has Parts
	assert.Len(t, final.Messages[2].Parts, 1, "Second message Parts should be preserved")
	assert.Equal(t, "Second message", *final.Messages[2].Parts[0].Text)
}

func TestArenaStateStore_AudioAndVideoContentPreservation(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	audioData := "//uQxAAAAAAAAAAAAAAAAAAAAAAASW5mbwAAAA8AAAACAAADhABVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVV"
	audioURL := "data:audio/mp3;base64," + audioData
	videoURL := "https://example.com/video.mp4"

	state := &statestore.ConversationState{
		ID:     "conv-av-test",
		UserID: "user-test",
		Messages: []types.Message{
			{
				Role:      "user",
				Timestamp: time.Now(),
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							URL:      &audioURL,
							MIMEType: types.MIMETypeAudioMP3,
						},
					},
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							URL:      &videoURL,
							MIMEType: types.MIMETypeVideoMP4,
						},
					},
				},
			},
		},
	}

	// Save and load
	err := store.Save(ctx, state)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "conv-av-test")
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 1)

	msg := loaded.Messages[0]
	require.Len(t, msg.Parts, 2, "Audio and video Parts should be preserved")

	// Verify audio part
	assert.Equal(t, types.ContentTypeAudio, msg.Parts[0].Type)
	require.NotNil(t, msg.Parts[0].Media)
	require.NotNil(t, msg.Parts[0].Media.URL)
	assert.Contains(t, *msg.Parts[0].Media.URL, audioData)
	assert.Equal(t, types.MIMETypeAudioMP3, msg.Parts[0].Media.MIMEType)

	// Verify video part
	assert.Equal(t, types.ContentTypeVideo, msg.Parts[1].Type)
	require.NotNil(t, msg.Parts[1].Media)
	require.NotNil(t, msg.Parts[1].Media.URL)
	assert.Equal(t, videoURL, *msg.Parts[1].Media.URL)
	assert.Equal(t, types.MIMETypeVideoMP4, msg.Parts[1].Media.MIMEType)
}
