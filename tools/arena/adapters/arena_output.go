package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ArenaOutputAdapter loads Arena output JSON files (from completed scenario runs).
type ArenaOutputAdapter struct{}

// NewArenaOutputAdapter creates a new arena output adapter.
func NewArenaOutputAdapter() *ArenaOutputAdapter {
	return &ArenaOutputAdapter{}
}

// CanHandle returns true for *.arena-output.json files or "arena_output" type hint.
func (a *ArenaOutputAdapter) CanHandle(path string, typeHint string) bool {
	if matchesTypeHint(typeHint, "arena", "arena_output", "scenario_output") {
		return true
	}
	return hasExtension(path, ".arena-output.json", ".arena.json")
}

// Load reads an arena output file and converts it to Arena messages.
func (a *ArenaOutputAdapter) Load(path string) ([]types.Message, *RecordingMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read arena output: %w", err)
	}

	var output ArenaOutputFile
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, nil, fmt.Errorf("failed to parse arena output JSON: %w", err)
	}

	messages, metadata := a.convertToMessages(&output)
	return messages, metadata, nil
}

// convertToMessages transforms arena output to Arena messages and metadata.
func (a *ArenaOutputAdapter) convertToMessages(output *ArenaOutputFile) ([]types.Message, *RecordingMetadata) {
	messages := make([]types.Message, 0)
	timestamps := make([]time.Time, 0)

	// Extract messages from turn results
	for _, turn := range output.Turns {
		// Add user message
		if turn.UserMessage.Content != "" || len(turn.UserMessage.Parts) > 0 {
			msg := types.Message{
				Role:    "user",
				Content: turn.UserMessage.Content,
			}
			if len(turn.UserMessage.Parts) > 0 {
				msg.Parts = a.convertParts(turn.UserMessage.Parts)
			}
			messages = append(messages, msg)
			timestamps = append(timestamps, turn.Timestamp)
		}

		// Add assistant message
		if turn.Response.Message.Content != "" || len(turn.Response.Message.Parts) > 0 || len(turn.Response.Message.ToolCalls) > 0 {
			msg := turn.Response.Message // Use message directly
			messages = append(messages, msg)
			timestamps = append(timestamps, turn.Timestamp)
		}

		// Add tool result messages
		for _, toolResult := range turn.ToolResults {
			msg := types.Message{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:      toolResult.ToolCallID,
					Content: toolResult.Content,
				},
			}
			messages = append(messages, msg)
			timestamps = append(timestamps, turn.Timestamp)
		}
	}

	metadata := &RecordingMetadata{
		Timestamps: timestamps,
		Tags:       output.Metadata.Tags,
		Extras:     make(map[string]interface{}),
	}

	// Extract provider info
	if output.ScenarioID != "" {
		metadata.Extras["scenario_id"] = output.ScenarioID
	}
	if output.ProviderID != "" {
		metadata.ProviderInfo = map[string]interface{}{
			"provider_id": output.ProviderID,
		}
	}

	// Calculate duration
	if len(timestamps) > 1 {
		metadata.Duration = timestamps[len(timestamps)-1].Sub(timestamps[0])
	}

	return messages, metadata
}

// convertParts converts arena output parts to types.ContentPart.
func (a *ArenaOutputAdapter) convertParts(parts []ArenaContentPart) []types.ContentPart {
	result := make([]types.ContentPart, len(parts))
	for i, part := range parts {
		result[i] = types.ContentPart{
			Type: part.Type,
		}
		if part.Text != nil && *part.Text != "" {
			result[i].Text = part.Text
		}
		if part.Media != nil {
			result[i].Media = part.Media
		}
	}
	return result
}

// ArenaOutputFile represents the structure of an arena output JSON file.
type ArenaOutputFile struct {
	ScenarioID string              `json:"scenario_id"`
	ProviderID string              `json:"provider_id"`
	Metadata   ArenaOutputMetadata `json:"metadata"`
	Turns      []ArenaOutputTurn   `json:"turns"`
	Summary    ArenaOutputSummary  `json:"summary"`
}

// ArenaOutputMetadata contains metadata about the scenario run.
type ArenaOutputMetadata struct {
	Tags   []string               `json:"tags,omitempty"`
	Extras map[string]interface{} `json:"extras,omitempty"`
}

// ArenaOutputTurn represents a single turn in the arena output.
type ArenaOutputTurn struct {
	TurnIndex   int                    `json:"turn_index"`
	Timestamp   time.Time              `json:"timestamp"`
	UserMessage ArenaMessage           `json:"user_message"`
	Response    ArenaResponse          `json:"response"`
	ToolResults []ArenaToolResult      `json:"tool_results,omitempty"`
	Assertions  []ArenaAssertionResult `json:"assertions,omitempty"`
}

// ArenaMessage represents a message in the arena output.
type ArenaMessage struct {
	Content string             `json:"content"`
	Parts   []ArenaContentPart `json:"parts,omitempty"`
}

// ArenaContentPart represents a content part in arena output.
type ArenaContentPart struct {
	Type  string              `json:"type"`
	Text  *string             `json:"text,omitempty"`
	Media *types.MediaContent `json:"media,omitempty"`
}

// ArenaResponse represents a provider response in arena output.
type ArenaResponse struct {
	Message types.Message `json:"message"`
	Cost    *ArenaCost    `json:"cost,omitempty"`
}

// ArenaCost represents cost information.
type ArenaCost struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// ArenaToolResult represents a tool execution result.
type ArenaToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// ArenaAssertionResult represents an assertion result.
type ArenaAssertionResult struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// ArenaOutputSummary contains summary statistics.
type ArenaOutputSummary struct {
	TotalTurns  int     `json:"total_turns"`
	PassedTurns int     `json:"passed_turns"`
	FailedTurns int     `json:"failed_turns"`
	TotalCost   float64 `json:"total_cost"`
}
