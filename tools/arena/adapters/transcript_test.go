package adapters

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"gopkg.in/yaml.v3"
)

func TestTranscriptAdapter_CanHandle(t *testing.T) {
	adapter := NewTranscriptAdapter()

	tests := []struct {
		name     string
		path     string
		typeHint string
		want     bool
	}{
		{
			name: "handles .transcript.yaml extension",
			path: "conversation.transcript.yaml",
			want: true,
		},
		{
			name: "handles .transcript.yml extension",
			path: "chat.transcript.yml",
			want: true,
		},
		{
			name:     "handles transcript type hint",
			path:     "file.txt",
			typeHint: "transcript",
			want:     true,
		},
		{
			name:     "handles yaml type hint",
			path:     "file.txt",
			typeHint: "yaml",
			want:     true,
		},
		{
			name: "does not handle other extensions",
			path: "file.json",
			want: false,
		},
		{
			name:     "does not handle other type hints",
			path:     "file.yaml",
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

func TestTranscriptAdapter_Load(t *testing.T) {
	adapter := NewTranscriptAdapter()

	transcript := TranscriptFile{
		Metadata: TranscriptMetadata{
			SessionID: "test-session",
			Provider:  "openai",
			Model:     "gpt-4",
			Tags:      []string{"test", "transcript"},
			JudgeTargets: map[string]TranscriptProviderSpec{
				"judge1": {
					Type:  "openai",
					Model: "gpt-4",
					ID:    "provider-1",
				},
			},
		},
		Messages: []TranscriptMessage{
			{
				Role:      "user",
				Content:   "Hello!",
				Timestamp: time.Now().Format(time.RFC3339),
			},
			{
				Role:      "assistant",
				Content:   "Hi there!",
				Timestamp: time.Now().Add(1 * time.Second).Format(time.RFC3339),
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.transcript.yaml")
	data, err := yaml.Marshal(transcript)
	if err != nil {
		t.Fatalf("Failed to marshal transcript: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	messages, metadata, err := adapter.Load(RecordingReference{ID: tmpFile, Source: tmpFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("Load() got %d messages, want 2", len(messages))
	}

	// Check messages
	if messages[0].Role != "user" {
		t.Errorf("messages[0].Role = %s, want user", messages[0].Role)
	}
	if messages[0].Content != "Hello!" {
		t.Errorf("messages[0].Content = %s, want 'Hello!'", messages[0].Content)
	}

	if messages[1].Role != "assistant" {
		t.Errorf("messages[1].Role = %s, want assistant", messages[1].Role)
	}

	// Check metadata
	if metadata == nil {
		t.Fatal("metadata is nil")
	}
	if metadata.SessionID != "test-session" {
		t.Errorf("metadata.SessionID = %s, want test-session", metadata.SessionID)
	}
	if len(metadata.Tags) != 2 {
		t.Errorf("metadata.Tags length = %d, want 2", len(metadata.Tags))
	}
	if len(metadata.JudgeTargets) != 1 {
		t.Errorf("metadata.JudgeTargets length = %d, want 1", len(metadata.JudgeTargets))
	}
}

func TestTranscriptAdapter_Load_WithToolCalls(t *testing.T) {
	adapter := NewTranscriptAdapter()

	transcript := TranscriptFile{
		Metadata: TranscriptMetadata{
			SessionID: "test-tools",
		},
		Messages: []TranscriptMessage{
			{
				Role:    "user",
				Content: "What's the weather?",
			},
			{
				Role: "assistant",
				ToolCalls: []TranscriptToolCall{
					{
						ID:   "call_456",
						Type: "function",
						Function: TranscriptToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"location":"NYC"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "Sunny, 75°F",
				ToolCallID: "call_456",
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-tools.transcript.yaml")
	data, err := yaml.Marshal(transcript)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	messages, _, err := adapter.Load(RecordingReference{ID: tmpFile, Source: tmpFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("Load() got %d messages, want 3", len(messages))
	}

	// Check tool call
	if len(messages[1].ToolCalls) != 1 {
		t.Errorf("messages[1].ToolCalls length = %d, want 1", len(messages[1].ToolCalls))
	}
	if messages[1].ToolCalls[0].ID != "call_456" {
		t.Errorf("ToolCall ID = %s, want call_456", messages[1].ToolCalls[0].ID)
	}
	if messages[1].ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCall Name = %s, want get_weather", messages[1].ToolCalls[0].Name)
	}

	// Check tool result
	if messages[2].Role != "tool" {
		t.Errorf("messages[2].Role = %s, want tool", messages[2].Role)
	}
	if messages[2].ToolResult == nil || messages[2].ToolResult.ID != "call_456" {
		t.Errorf("messages[2].ToolResult.ID = %v, want call_456", messages[2].ToolResult)
	}
}

func TestTranscriptAdapter_Load_WithMultimodal(t *testing.T) {
	adapter := NewTranscriptAdapter()

	transcript := TranscriptFile{
		Metadata: TranscriptMetadata{
			SessionID: "test-multimodal",
		},
		Messages: []TranscriptMessage{
			{
				Role:    "user",
				Content: "Analyze this image",
				Parts: []TranscriptContentPart{
					{
						Type: "text",
						Text: testutil.Ptr("Analyze this image"),
					},
					{
						Type: "image",
						Media: &TranscriptMediaPart{
							MIMEType: "image/jpeg",
							URI:      "https://example.com/photo.jpg",
							Width:    1024,
							Height:   768,
						},
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-multimodal.transcript.yaml")
	data, err := yaml.Marshal(transcript)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	messages, _, err := adapter.Load(RecordingReference{ID: tmpFile, Source: tmpFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Load() got %d messages, want 1", len(messages))
	}

	// Check multimodal parts
	if len(messages[0].Parts) != 2 {
		t.Fatalf("messages[0].Parts length = %d, want 2", len(messages[0].Parts))
	}

	// Check text part
	if messages[0].Parts[0].Type != "text" {
		t.Errorf("Part 0 type = %s, want text", messages[0].Parts[0].Type)
	}

	// Check image part
	if messages[0].Parts[1].Type != "image" {
		t.Errorf("Part 1 type = %s, want image", messages[0].Parts[1].Type)
	}
	if messages[0].Parts[1].Media == nil {
		t.Fatal("Part 1 media is nil")
	}
	if messages[0].Parts[1].Media.URL == nil || *messages[0].Parts[1].Media.URL != "https://example.com/photo.jpg" {
		url := ""
		if messages[0].Parts[1].Media.URL != nil {
			url = *messages[0].Parts[1].Media.URL
		}
		t.Errorf("Media URL = %s, want https://example.com/photo.jpg", url)
	}
}

func TestTranscriptAdapter_Load_InvalidFile(t *testing.T) {
	adapter := NewTranscriptAdapter()

	_, _, err := adapter.Load(RecordingReference{ID: "/nonexistent/file.transcript.yaml", Source: "/nonexistent/file.transcript.yaml"})
	if err == nil {
		t.Error("Load() should return error for nonexistent file")
	}
}

func TestTranscriptAdapter_Load_InvalidYAML(t *testing.T) {
	adapter := NewTranscriptAdapter()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.transcript.yaml")
	if err := os.WriteFile(tmpFile, []byte("invalid: [unclosed"), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	_, _, err := adapter.Load(RecordingReference{ID: tmpFile, Source: tmpFile})
	if err == nil {
		t.Error("Load() should return error for invalid YAML")
	}
}

func TestTranscriptConvertContentPart(t *testing.T) {
	adapter := NewTranscriptAdapter()

	tests := []struct {
		name string
		part TranscriptContentPart
		want string // expected type
	}{
		{
			name: "text part",
			part: TranscriptContentPart{
				Type: "text",
				Text: testutil.Ptr("Hello world"),
			},
			want: "text",
		},
		{
			name: "image part with data",
			part: TranscriptContentPart{
				Type: "image",
				Media: &TranscriptMediaPart{
					MIMEType: "image/png",
					Data:     "base64data",
				},
			},
			want: "image",
		},
		{
			name: "audio part with path",
			part: TranscriptContentPart{
				Type: "audio",
				Media: &TranscriptMediaPart{
					MIMEType: "audio/mp3",
					Path:     "/audio/file.mp3",
					Duration: 30000, // 30 seconds in milliseconds
				},
			},
			want: "audio",
		},
		{
			name: "video part with URI",
			part: TranscriptContentPart{
				Type: "video",
				Media: &TranscriptMediaPart{
					MIMEType: "video/mp4",
					URI:      "https://example.com/video.mp4",
					Width:    1920,
					Height:   1080,
				},
			},
			want: "video",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.convertContentPart(tt.part)
			if got.Type != tt.want {
				t.Errorf("convertContentPart().Type = %s, want %s", got.Type, tt.want)
			}
		})
	}
}
