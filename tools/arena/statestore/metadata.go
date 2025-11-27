package statestore

import (
	"context"
	"encoding/json"
	"fmt"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// SaveMetadata stores Arena-specific metadata for a run
// ConversationID should equal RunID for Arena executions
func (s *ArenaStateStore) SaveMetadata(ctx context.Context, conversationID string, metadata *RunMetadata) error {
	if metadata == nil {
		return fmt.Errorf("metadata cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get or create arena state
	arenaState, exists := s.conversations[conversationID]
	if !exists {
		// Create minimal state if it doesn't exist (for early failures)
		arenaState = &ArenaConversationState{
			ConversationState: runtimestore.ConversationState{
				ID:       conversationID,
				Metadata: make(map[string]interface{}),
			},
		}
		s.conversations[conversationID] = arenaState
	}

	// Save metadata
	arenaState.RunMetadata = metadata

	return nil
}

// GetResult reconstructs a complete RunResult from stored state
// This replaces the need to return RunResult from executeRun
func (s *ArenaStateStore) GetResult(ctx context.Context, runID string) (*RunResult, error) {
	arenaState, err := s.GetArenaState(ctx, runID)
	if err != nil {
		return nil, err
	}

	if arenaState.RunMetadata == nil {
		return nil, fmt.Errorf("run metadata not found for %s", runID)
	}

	// Compute derived values
	totalCost := s.computeTotalCost(arenaState)
	toolStats := s.computeToolStats(arenaState)
	violations := s.extractViolationsFlat(arenaState)

	// Collect media outputs from messages
	mediaOutputs := s.collectMediaOutputs(arenaState)

	// Build Params map from RunMetadata.Params and inject system_prompt from state.Metadata
	params := make(map[string]interface{})
	if arenaState.RunMetadata.Params != nil {
		for k, v := range arenaState.RunMetadata.Params {
			params[k] = v
		}
	}

	// Add system_prompt from conversation metadata if present
	if systemPrompt, ok := arenaState.Metadata["system_prompt"]; ok {
		params["system_prompt"] = systemPrompt
	}

	// Reconstruct RunResult
	result := &RunResult{
		RunID:      arenaState.RunMetadata.RunID,
		PromptPack: arenaState.RunMetadata.PromptPack,
		Region:     arenaState.RunMetadata.Region,
		ScenarioID: arenaState.RunMetadata.ScenarioID,
		ProviderID: arenaState.RunMetadata.ProviderID,
		Params:     params,
		Messages:   arenaState.Messages,
		Commit:     arenaState.RunMetadata.Commit,
		Cost:       totalCost,
		ToolStats:  toolStats,
		Violations: violations,
		StartTime:  arenaState.RunMetadata.StartTime,
		EndTime:    arenaState.RunMetadata.EndTime,
		Duration:   arenaState.RunMetadata.Duration,
		Error:      arenaState.RunMetadata.Error,
		SelfPlay:   arenaState.RunMetadata.SelfPlay,
		PersonaID:  arenaState.RunMetadata.PersonaID,

		UserFeedback:  arenaState.RunMetadata.UserFeedback,
		SessionTags:   arenaState.RunMetadata.SessionTags,
		AssistantRole: arenaState.RunMetadata.AssistantRole,
		UserRole:      arenaState.RunMetadata.UserRole,

		MediaOutputs: mediaOutputs,

		// Conversation-level assertions (summary)
		ConversationAssertions: buildConversationAssertionsSummary(arenaState.RunMetadata.ConversationAssertionResults),
	}

	return result, nil
}

// buildConversationAssertionsSummary converts raw conversation assertion results into summary format
func buildConversationAssertionsSummary(results []ConversationValidationResult) AssertionsSummary {
	total := len(results)
	failed := 0
	for i := range results {
		if !results[i].Passed {
			failed++
		}
	}
	passed := failed == 0 && total > 0
	return AssertionsSummary{
		Failed:  failed,
		Passed:  passed,
		Results: results,
		Total:   total,
	}
}

// DumpToJSON exports the arena state in the same format as Arena results
func (s *ArenaStateStore) DumpToJSON(ctx context.Context, conversationID string) ([]byte, error) {
	arenaState, err := s.GetArenaState(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	// Compute validations from messages (for backward compatibility)
	validations := s.extractValidations(arenaState)

	// Compute total cost from messages
	totalCost := s.computeTotalCost(arenaState)

	// Compute tool stats from messages
	toolStats := s.computeToolStats(arenaState)

	// Build result structure
	result := map[string]interface{}{
		"messages":   arenaState.Messages,
		"metadata":   arenaState.Metadata,
		"cost":       totalCost,
		"tool_stats": toolStats,
	}

	// Include run metadata if present
	if arenaState.RunMetadata != nil {
		result["RunID"] = arenaState.RunMetadata.RunID
		result["PromptPack"] = arenaState.RunMetadata.PromptPack
		result["Region"] = arenaState.RunMetadata.Region
		result["ScenarioID"] = arenaState.RunMetadata.ScenarioID
		result["ProviderID"] = arenaState.RunMetadata.ProviderID
		result["Params"] = arenaState.RunMetadata.Params
		result["Commit"] = arenaState.RunMetadata.Commit
		result["StartTime"] = arenaState.RunMetadata.StartTime
		result["EndTime"] = arenaState.RunMetadata.EndTime
		result["Duration"] = arenaState.RunMetadata.Duration
		result["Error"] = arenaState.RunMetadata.Error
		result["SelfPlay"] = arenaState.RunMetadata.SelfPlay
		result["PersonaID"] = arenaState.RunMetadata.PersonaID
		result["UserFeedback"] = arenaState.RunMetadata.UserFeedback
		result["SessionTags"] = arenaState.RunMetadata.SessionTags
		result["AssistantRole"] = arenaState.RunMetadata.AssistantRole
		result["UserRole"] = arenaState.RunMetadata.UserRole

		// Compute violations as flat list for RunResult compatibility
		result["Violations"] = s.extractViolationsFlat(arenaState)
		result["Messages"] = arenaState.Messages
		result["Cost"] = totalCost
		result["ToolStats"] = toolStats
	} else {
		// Legacy format without metadata
		result["conversation_id"] = conversationID
		result["validations"] = validations
	}

	return json.MarshalIndent(result, "", "  ")
}

// ListRunIDs returns all stored run IDs (for batch operations)
func (s *ArenaStateStore) ListRunIDs(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runIDs := make([]string, 0, len(s.conversations))
	for id, state := range s.conversations {
		// Only include conversations with run metadata
		if state.RunMetadata != nil {
			runIDs = append(runIDs, id)
		}
	}

	return runIDs, nil
}

// collectMediaOutputs extracts media outputs from arena state messages
func (s *ArenaStateStore) collectMediaOutputs(state *ArenaConversationState) []MediaOutput {
	var outputs []MediaOutput

	for msgIdx, msg := range state.Messages {
		// Only collect from assistant messages
		if msg.Role != "assistant" {
			continue
		}

		outputs = append(outputs, s.collectMediaFromMessage(&msg, msgIdx)...)
	}

	return outputs
}

// collectMediaFromMessage extracts media outputs from a single message
func (s *ArenaStateStore) collectMediaFromMessage(msg *types.Message, msgIdx int) []MediaOutput {
	var outputs []MediaOutput

	for partIdx, part := range msg.Parts {
		if part.Media == nil {
			continue
		}

		output := s.buildMediaOutput(&part, msgIdx, partIdx)
		outputs = append(outputs, output)
	}

	return outputs
}

// buildMediaOutput creates a MediaOutput from a content part
func (s *ArenaStateStore) buildMediaOutput(part *types.ContentPart, msgIdx, partIdx int) MediaOutput {
	output := MediaOutput{
		Type:       part.Type,
		MIMEType:   part.Media.MIMEType,
		MessageIdx: msgIdx,
		PartIdx:    partIdx,
	}

	// Copy metadata
	output.Duration = part.Media.Duration
	output.Width = part.Media.Width
	output.Height = part.Media.Height

	// Calculate size
	output.SizeBytes = s.calculateMediaSize(part.Media)

	// Store file path
	if part.Media.FilePath != nil {
		output.FilePath = *part.Media.FilePath
	}

	// Store thumbnail for images
	if part.Type == "image" && part.Media.Data != nil && len(*part.Media.Data) <= 50000 {
		output.Thumbnail = *part.Media.Data
	}

	return output
}

// calculateMediaSize determines the size of media content in bytes
func (s *ArenaStateStore) calculateMediaSize(media *types.MediaContent) int64 {
	if media.SizeKB != nil {
		return *media.SizeKB * 1024
	}
	if media.Data != nil {
		// Estimate from base64 data
		return int64(len(*media.Data) * 3 / 4)
	}
	return 0
}

// SaveRunMetadata is deprecated: use SaveMetadata instead
// Kept for backwards compatibility
func (s *ArenaStateStore) SaveRunMetadata(ctx context.Context, conversationID string, metadata *RunMetadata) error {
	return s.SaveMetadata(ctx, conversationID, metadata)
}

// GetRunResult is deprecated: use GetResult instead
// Kept for backwards compatibility
func (s *ArenaStateStore) GetRunResult(ctx context.Context, runID string) (*RunResult, error) {
	return s.GetResult(ctx, runID)
}
