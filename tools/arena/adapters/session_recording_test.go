package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}

func TestSessionRecordingAdapter_CanHandle(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	tests := []struct {
		name     string
		path     string
		typeHint string
		want     bool
	}{
		{
			name:     "handles .recording.json extension",
			path:     "session.recording.json",
			typeHint: "",
			want:     true,
		},
		{
			name:     "handles session type hint",
			path:     "anyfile.json",
			typeHint: "session",
			want:     true,
		},
		{
			name:     "handles recording type hint",
			path:     "anyfile.json",
			typeHint: "recording",
			want:     true,
		},
		{
			name:     "handles session_recording type hint",
			path:     "anyfile.json",
			typeHint: "session_recording",
			want:     true,
		},
		{
			name:     "does not handle other extensions",
			path:     "file.json",
			typeHint: "",
			want:     false,
		},
		{
			name:     "does not handle other type hints",
			path:     "file.json",
			typeHint: "arena",
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

func TestSessionRecordingAdapter_Load(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	// Create a test recording file
	recording := SessionRecordingFile{
		Metadata: RecordingMetadataFile{
			SessionID:  "test-session-123",
			ProviderID: "test-provider",
			Model:      "gpt-4",
			Tags:       []string{"test", "customer-support"},
		},
		Events: []RecordingEvent{
			{
				Type:      "message",
				Timestamp: time.Now(),
				Message: RecordedMsg{
					Role:    "user",
					Content: "Hello, I need help",
				},
			},
			{
				Type:      "message",
				Timestamp: time.Now().Add(time.Second),
				Message: RecordedMsg{
					Role:    "assistant",
					Content: "Hello! How can I assist you today?",
				},
			},
			{
				Type:      "message",
				Timestamp: time.Now().Add(2 * time.Second),
				Message: RecordedMsg{
					Role:    "user",
					Content: "I have a billing question",
					Parts: []RecordedContentPart{
						{
							Type: "text",
							Text: stringPtr("I have a billing question"),
						},
					},
				},
			},
		},
	}

	// Write to temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.recording.json")
	data, err := json.Marshal(recording)
	if err != nil {
		t.Fatalf("Failed to marshal recording: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Load the recording
	messages, metadata, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify messages
	if len(messages) != 3 {
		t.Errorf("Load() got %d messages, want 3", len(messages))
	}

	if messages[0].Role != "user" {
		t.Errorf("messages[0].Role = %s, want user", messages[0].Role)
	}
	if messages[0].Content != "Hello, I need help" {
		t.Errorf("messages[0].Content = %s, want 'Hello, I need help'", messages[0].Content)
	}

	if messages[1].Role != "assistant" {
		t.Errorf("messages[1].Role = %s, want assistant", messages[1].Role)
	}

	if messages[2].Role != "user" {
		t.Errorf("messages[2].Role = %s, want user", messages[2].Role)
	}
	if len(messages[2].Parts) != 1 {
		t.Errorf("messages[2].Parts length = %d, want 1", len(messages[2].Parts))
	}

	// Verify metadata
	if metadata.SessionID != "test-session-123" {
		t.Errorf("metadata.SessionID = %s, want test-session-123", metadata.SessionID)
	}

	if len(metadata.Tags) != 2 {
		t.Errorf("metadata.Tags length = %d, want 2", len(metadata.Tags))
	}

	if len(metadata.Timestamps) != 3 {
		t.Errorf("metadata.Timestamps length = %d, want 3", len(metadata.Timestamps))
	}

	if metadata.ProviderInfo["provider_id"] != "test-provider" {
		t.Errorf("metadata.ProviderInfo[provider_id] = %v, want test-provider", metadata.ProviderInfo["provider_id"])
	}

	if metadata.Duration == 0 {
		t.Error("metadata.Duration should be > 0")
	}
}

func TestSessionRecordingAdapter_Load_WithToolCalls(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	recording := SessionRecordingFile{
		Metadata: RecordingMetadataFile{
			SessionID: "test-tools",
		},
		Events: []RecordingEvent{
			{
				Type:      "message",
				Timestamp: time.Now(),
				Message: RecordedMsg{
					Role:    "assistant",
					Content: "",
					ToolCalls: []RecordedToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: RecordedToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"location":"SF"}`,
							},
						},
					},
				},
			},
			{
				Type:      "message",
				Timestamp: time.Now().Add(time.Second),
				Message: RecordedMsg{
					Role:       "tool",
					Content:    `{"temperature":72}`,
					ToolCallID: "call_123",
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.recording.json")
	data, err := json.Marshal(recording)
	if err != nil {
		t.Fatalf("Failed to marshal recording: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	messages, _, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("Load() got %d messages, want 2", len(messages))
	}

	// Check tool call
	if len(messages[0].ToolCalls) != 1 {
		t.Errorf("messages[0].ToolCalls length = %d, want 1", len(messages[0].ToolCalls))
	}
	if messages[0].ToolCalls[0].ID != "call_123" {
		t.Errorf("ToolCall ID = %s, want call_123", messages[0].ToolCalls[0].ID)
	}
	if messages[0].ToolCalls[0].Name != "get_weather" {
		t.Errorf("Function name = %s, want get_weather", messages[0].ToolCalls[0].Name)
	}

	// Check tool result
	if messages[1].Role != "tool" {
		t.Errorf("messages[1].Role = %s, want tool", messages[1].Role)
	}
	if messages[1].ToolResult == nil || messages[1].ToolResult.ID != "call_123" {
		t.Errorf("messages[1].ToolResult.ID = %v, want call_123", messages[1].ToolResult)
	}
}

func TestSessionRecordingAdapter_Load_WithMultimodal(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	recording := SessionRecordingFile{
		Metadata: RecordingMetadataFile{
			SessionID: "test-multimodal",
		},
		Events: []RecordingEvent{
			{
				Type:      "message",
				Timestamp: time.Now(),
				Message: RecordedMsg{
					Role:    "user",
					Content: "What's in this image?",
					Parts: []RecordedContentPart{
						{
							Type: "text",
							Text: stringPtr("What's in this image?"),
						},
						{
							Type: "image",
							Media: &RecordedMediaPart{
								MIMEType: "image/jpeg",
								Data:     "base64encodeddata",
								Size:     1024,
								Width:    800,
								Height:   600,
							},
						},
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.recording.json")
	data, err := json.Marshal(recording)
	if err != nil {
		t.Fatalf("Failed to marshal recording: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	messages, _, err := adapter.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Load() got %d messages, want 1", len(messages))
	}

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
	if messages[0].Parts[1].Media.MIMEType != "image/jpeg" {
		t.Errorf("Media MIMEType = %s, want image/jpeg", messages[0].Parts[1].Media.MIMEType)
	}
	if messages[0].Parts[1].Media.Width == nil || *messages[0].Parts[1].Media.Width != 800 {
		width := 0
		if messages[0].Parts[1].Media.Width != nil {
			width = *messages[0].Parts[1].Media.Width
		}
		t.Errorf("Media Width = %d, want 800", width)
	}
}

func TestSessionRecordingAdapter_Load_InvalidFile(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	_, _, err := adapter.Load("nonexistent.recording.json")
	if err == nil {
		t.Error("Load() should return error for nonexistent file")
	}
}

func TestSessionRecordingAdapter_Load_InvalidJSON(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.recording.json")
	if err := os.WriteFile(tmpFile, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	_, _, err := adapter.Load(tmpFile)
	if err == nil {
		t.Error("Load() should return error for invalid JSON")
	}
}

func TestConvertContentPart(t *testing.T) {
	adapter := NewSessionRecordingAdapter()

	tests := []struct {
		name  string
		part  RecordedContentPart
		check func(t *testing.T, cp types.ContentPart)
	}{
		{
			name: "text part",
			part: RecordedContentPart{
				Type: "text",
				Text: stringPtr("Hello"),
			},
			check: func(t *testing.T, cp types.ContentPart) {
				if cp.Type != "text" {
					t.Errorf("Type = %s, want text", cp.Type)
				}
				if cp.Text == nil || *cp.Text != "Hello" {
					text := ""
					if cp.Text != nil {
						text = *cp.Text
					}
					t.Errorf("Text = %s, want Hello", text)
				}
			},
		},
		{
			name: "image part with base64 data",
			part: RecordedContentPart{
				Type: "image",
				Media: &RecordedMediaPart{
					MIMEType: "image/png",
					Data:     "base64data",
					Size:     2048,
				},
			},
			check: func(t *testing.T, cp types.ContentPart) {
				if cp.Type != "image" {
					t.Errorf("Type = %s, want image", cp.Type)
				}
				if cp.Media == nil {
					t.Fatal("Media is nil")
				}
				if cp.Media.Data == nil || *cp.Media.Data != "base64data" {
					data := ""
					if cp.Media.Data != nil {
						data = *cp.Media.Data
					}
					t.Errorf("Data = %s, want base64data", data)
				}
			},
		},
		{
			name: "image part with URI",
			part: RecordedContentPart{
				Type: "image",
				Media: &RecordedMediaPart{
					MIMEType: "image/jpeg",
					URI:      "https://example.com/image.jpg",
				},
			},
			check: func(t *testing.T, cp types.ContentPart) {
				if cp.Media == nil {
					t.Fatal("Media is nil")
				}
				if cp.Media.URL == nil || *cp.Media.URL != "https://example.com/image.jpg" {
					url := ""
					if cp.Media.URL != nil {
						url = *cp.Media.URL
					}
					t.Errorf("URL = %s, want https://example.com/image.jpg", url)
				}
			},
		},
		{
			name: "image part with file path",
			part: RecordedContentPart{
				Type: "image",
				Media: &RecordedMediaPart{
					MIMEType: "image/png",
					Path:     "/path/to/image.png",
				},
			},
			check: func(t *testing.T, cp types.ContentPart) {
				if cp.Media == nil {
					t.Fatal("Media is nil")
				}
				if cp.Media.FilePath == nil || *cp.Media.FilePath != "/path/to/image.png" {
					path := ""
					if cp.Media.FilePath != nil {
						path = *cp.Media.FilePath
					}
					t.Errorf("FilePath = %s, want /path/to/image.png", path)
				}
			},
		},
		{
			name: "video part with duration",
			part: RecordedContentPart{
				Type: "video",
				Media: &RecordedMediaPart{
					MIMEType: "video/mp4",
					Duration: 5000, // 5 seconds in milliseconds
				},
			},
			check: func(t *testing.T, cp types.ContentPart) {
				if cp.Media == nil {
					t.Fatal("Media is nil")
				}
				expected := 5 // 5 seconds
				if cp.Media.Duration == nil || *cp.Media.Duration != expected {
					duration := 0
					if cp.Media.Duration != nil {
						duration = *cp.Media.Duration
					}
					t.Errorf("Duration = %v seconds, want %v seconds", duration, expected)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.convertContentPart(tt.part)
			tt.check(t, got)
		})
	}
}
