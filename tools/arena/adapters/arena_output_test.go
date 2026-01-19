package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestArenaOutputAdapter_CanHandle(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	tests := []struct {
		name     string
		path     string
		typeHint string
		want     bool
	}{
		{
			name: "handles .arena-output.json extension",
			path: "output.arena-output.json",
			want: true,
		},
		{
			name: "handles .arena.json extension",
			path: "scenario.arena.json",
			want: true,
		},
		{
			name:     "handles arena type hint",
			path:     "some-file.json",
			typeHint: "arena",
			want:     true,
		},
		{
			name:     "handles arena_output type hint",
			path:     "some-file.json",
			typeHint: "arena_output",
			want:     true,
		},
		{
			name:     "handles scenario_output type hint",
			path:     "some-file.json",
			typeHint: "scenario_output",
			want:     true,
		},
		{
			name: "does not handle other extensions",
			path: "file.txt",
			want: false,
		},
		{
			name:     "does not handle other type hints",
			path:     "file.json",
			typeHint: "other",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.CanHandle(tt.path, tt.typeHint)
			if got != tt.want {
				t.Errorf("CanHandle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArenaOutputAdapter_Load(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	arenaOutput := ArenaOutputFile{
		ScenarioID: "test-scenario",
		ProviderID: "provider-1",
		Metadata: ArenaOutputMetadata{
			Tags: []string{"test", "arena"},
		},
		Turns: []ArenaOutputTurn{
			{
				TurnIndex: 0,
				Timestamp: time.Now(),
				UserMessage: ArenaMessage{
					Content: "What is the weather?",
				},
				Response: ArenaResponse{
					Message: types.Message{
						Role:    "assistant",
						Content: "It's sunny today.",
					},
				},
			},
		},
		Summary: ArenaOutputSummary{
			TotalTurns:  1,
			PassedTurns: 1,
			TotalCost:   0.001,
		},
	}

	// Write to temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.arena-output.json")
	data, err := json.Marshal(arenaOutput)
	if err != nil {
		t.Fatalf("Failed to marshal arena output: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	messages, metadata, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("Load() got %d messages, want 2", len(messages))
	}

	// Check user message
	if messages[0].Role != "user" {
		t.Errorf("messages[0].Role = %s, want user", messages[0].Role)
	}
	if messages[0].Content != "What is the weather?" {
		t.Errorf("messages[0].Content = %s, want 'What is the weather?'", messages[0].Content)
	}

	// Check assistant message
	if messages[1].Role != "assistant" {
		t.Errorf("messages[1].Role = %s, want assistant", messages[1].Role)
	}
	if messages[1].Content != "It's sunny today." {
		t.Errorf("messages[1].Content = %s, want 'It's sunny today.'", messages[1].Content)
	}

	// Check metadata
	if metadata == nil {
		t.Fatal("metadata is nil")
	}
	if len(metadata.Tags) != 2 {
		t.Errorf("metadata.Tags length = %d, want 2", len(metadata.Tags))
	}
	if metadata.Extras["scenario_id"] != "test-scenario" {
		t.Errorf("metadata.Extras[scenario_id] = %v, want test-scenario", metadata.Extras["scenario_id"])
	}
}

func TestArenaOutputAdapter_Load_WithToolResults(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	arenaOutput := ArenaOutputFile{
		ScenarioID: "test-tools",
		Turns: []ArenaOutputTurn{
			{
				TurnIndex: 0,
				Timestamp: time.Now(),
				UserMessage: ArenaMessage{
					Content: "Get weather for NYC",
				},
				Response: ArenaResponse{
					Message: types.Message{
						Role: "assistant",
						ToolCalls: []types.MessageToolCall{
							{
								ID:   "call_123",
								Name: "get_weather",
								Args: json.RawMessage(`{"location":"NYC"}`),
							},
						},
					},
				},
				ToolResults: []ArenaToolResult{
					{
						ToolCallID: "call_123",
						Content:    "Sunny, 72°F",
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-tools.arena.json")
	data, err := json.Marshal(arenaOutput)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	messages, _, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should have: user message, assistant with tool call, tool result
	if len(messages) != 3 {
		t.Fatalf("Load() got %d messages, want 3", len(messages))
	}

	// Check tool call in assistant message
	if len(messages[1].ToolCalls) != 1 {
		t.Errorf("messages[1].ToolCalls length = %d, want 1", len(messages[1].ToolCalls))
	}

	// Check tool result message
	if messages[2].Role != "tool" {
		t.Errorf("messages[2].Role = %s, want tool", messages[2].Role)
	}
	if messages[2].ToolResult == nil || messages[2].ToolResult.ID != "call_123" {
		t.Errorf("messages[2].ToolResult.ID = %v, want call_123", messages[2].ToolResult)
	}
	if messages[2].ToolResult.Content != "Sunny, 72°F" {
		t.Errorf("messages[2].ToolResult.Content = %s, want 'Sunny, 72°F'", messages[2].ToolResult.Content)
	}
}

func TestArenaOutputAdapter_Load_InvalidFile(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	_, _, err := adapter.Load("/nonexistent/file.arena.json")
	if err == nil {
		t.Error("Load() should return error for nonexistent file")
	}
}

func TestArenaOutputAdapter_Load_InvalidJSON(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.arena.json")
	if err := os.WriteFile(tmpFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	_, _, err := adapter.Load(tmpFile)
	if err == nil {
		t.Error("Load() should return error for invalid JSON")
	}
}

func TestArenaOutputAdapter_Load_SimpleFormat(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	// Test the simple format (Messages array + RunID)
	simpleFormat := struct {
		Messages []types.Message `json:"Messages"`
		RunID    string          `json:"RunID"`
	}{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
		RunID: "test-run-123",
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "simple.arena.json")
	data, err := json.Marshal(simpleFormat)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	messages, metadata, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check messages
	if len(messages) != 3 {
		t.Fatalf("Load() got %d messages, want 3", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "Hello" {
		t.Errorf("messages[0] = %+v, want user: Hello", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Hi there!" {
		t.Errorf("messages[1] = %+v, want assistant: Hi there!", messages[1])
	}

	// Check metadata
	if metadata == nil {
		t.Fatal("metadata is nil")
	}
	if metadata.SessionID != "test-run-123" {
		t.Errorf("metadata.SessionID = %s, want test-run-123", metadata.SessionID)
	}
}

func TestConvertParts(t *testing.T) {
	adapter := NewArenaOutputAdapter()

	tests := []struct {
		name  string
		parts []ArenaContentPart
		want  int
	}{
		{
			name: "text part",
			parts: []ArenaContentPart{
				{
					Type: "text",
					Text: stringPtr("Hello"),
				},
			},
			want: 1,
		},
		{
			name: "mixed parts",
			parts: []ArenaContentPart{
				{
					Type: "text",
					Text: stringPtr("Check this image:"),
				},
				{
					Type: "image",
					Media: &types.MediaContent{
						MIMEType: "image/png",
					},
				},
			},
			want: 2,
		},
		{
			name:  "empty parts",
			parts: []ArenaContentPart{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.convertParts(tt.parts)
			if len(got) != tt.want {
				t.Errorf("convertParts() length = %d, want %d", len(got), tt.want)
			}
		})
	}
}
