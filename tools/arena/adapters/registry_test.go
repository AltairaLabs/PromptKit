package adapters

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockAdapter is a test adapter for testing the registry.
type mockAdapter struct {
	canHandleFunc func(path string, typeHint string) bool
	loadFunc      func(path string) ([]types.Message, *RecordingMetadata, error)
}

func (m *mockAdapter) CanHandle(path string, typeHint string) bool {
	if m.canHandleFunc != nil {
		return m.canHandleFunc(path, typeHint)
	}
	return false
}

func (m *mockAdapter) Load(path string) ([]types.Message, *RecordingMetadata, error) {
	if m.loadFunc != nil {
		return m.loadFunc(path)
	}
	return nil, nil, nil
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}

	// Should have built-in adapters registered
	if len(registry.adapters) == 0 {
		t.Error("NewRegistry() should register built-in adapters")
	}
}

func TestRegistry_Register(t *testing.T) {
	registry := &Registry{
		adapters: make([]RecordingAdapter, 0),
	}

	adapter := &mockAdapter{}
	registry.Register(adapter)

	if len(registry.adapters) != 1 {
		t.Errorf("Register() did not add adapter, got %d adapters", len(registry.adapters))
	}
}

func TestRegistry_FindAdapter(t *testing.T) {
	registry := &Registry{
		adapters: make([]RecordingAdapter, 0),
	}

	// Register a mock adapter that handles .test files
	mockAdptr := &mockAdapter{
		canHandleFunc: func(path string, typeHint string) bool {
			return hasExtension(path, ".test")
		},
	}
	registry.Register(mockAdptr)

	tests := []struct {
		name     string
		path     string
		typeHint string
		want     bool
	}{
		{
			name:     "finds adapter for .test extension",
			path:     "file.test",
			typeHint: "",
			want:     true,
		},
		{
			name:     "does not find adapter for .other extension",
			path:     "file.other",
			typeHint: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := registry.FindAdapter(tt.path, tt.typeHint)
			got := adapter != nil
			if got != tt.want {
				t.Errorf("FindAdapter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistry_Load(t *testing.T) {
	registry := &Registry{
		adapters: make([]RecordingAdapter, 0),
	}

	// Register a mock adapter
	expectedMessages := []types.Message{
		{Role: "user", Content: "test"},
	}
	expectedMetadata := &RecordingMetadata{
		SessionID: "test-session",
	}

	mockAdptr := &mockAdapter{
		canHandleFunc: func(path string, typeHint string) bool {
			return hasExtension(path, ".test")
		},
		loadFunc: func(path string) ([]types.Message, *RecordingMetadata, error) {
			return expectedMessages, expectedMetadata, nil
		},
	}
	registry.Register(mockAdptr)

	messages, metadata, err := registry.Load("file.test", "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(messages) != len(expectedMessages) {
		t.Errorf("Load() got %d messages, want %d", len(messages), len(expectedMessages))
	}

	if metadata.SessionID != expectedMetadata.SessionID {
		t.Errorf("Load() got session ID %s, want %s", metadata.SessionID, expectedMetadata.SessionID)
	}
}

func TestRegistry_Load_NoAdapter(t *testing.T) {
	registry := &Registry{
		adapters: make([]RecordingAdapter, 0),
	}

	_, _, err := registry.Load("file.unknown", "")
	if err == nil {
		t.Error("Load() should return error when no adapter found")
	}
}

func TestHasExtension(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		extensions []string
		want       bool
	}{
		{
			name:       "matches single extension",
			path:       "file.json",
			extensions: []string{".json"},
			want:       true,
		},
		{
			name:       "matches one of multiple extensions",
			path:       "file.yaml",
			extensions: []string{".json", ".yaml", ".yml"},
			want:       true,
		},
		{
			name:       "case insensitive match",
			path:       "file.JSON",
			extensions: []string{".json"},
			want:       true,
		},
		{
			name:       "does not match different extension",
			path:       "file.txt",
			extensions: []string{".json"},
			want:       false,
		},
		{
			name:       "matches .recording.json compound extension",
			path:       "file.recording.json",
			extensions: []string{".recording.json"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExtension(tt.path, tt.extensions...)
			if got != tt.want {
				t.Errorf("hasExtension() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesTypeHint(t *testing.T) {
	tests := []struct {
		name     string
		typeHint string
		types    []string
		want     bool
	}{
		{
			name:     "matches exact type",
			typeHint: "session",
			types:    []string{"session"},
			want:     true,
		},
		{
			name:     "matches one of multiple types",
			typeHint: "transcript",
			types:    []string{"session", "transcript", "arena"},
			want:     true,
		},
		{
			name:     "case insensitive match",
			typeHint: "SESSION",
			types:    []string{"session"},
			want:     true,
		},
		{
			name:     "trims whitespace",
			typeHint: " session ",
			types:    []string{"session"},
			want:     true,
		},
		{
			name:     "does not match different type",
			typeHint: "other",
			types:    []string{"session"},
			want:     false,
		},
		{
			name:     "empty type hint returns false",
			typeHint: "",
			types:    []string{"session"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesTypeHint(tt.typeHint, tt.types...)
			if got != tt.want {
				t.Errorf("matchesTypeHint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordingMetadata(t *testing.T) {
	// Test RecordingMetadata structure
	metadata := &RecordingMetadata{
		JudgeTargets: map[string]ProviderSpec{
			"default": {
				Type:  "openai",
				Model: "gpt-4",
				ID:    "gpt-4-judge",
			},
		},
		ProviderInfo: map[string]interface{}{
			"provider_id": "test-provider",
			"model":       "test-model",
		},
		Tags:       []string{"tag1", "tag2"},
		Timestamps: []time.Time{time.Now(), time.Now().Add(time.Minute)},
		SessionID:  "test-session",
		Duration:   time.Minute,
		Extras: map[string]interface{}{
			"custom_field": "value",
		},
	}

	if metadata.SessionID != "test-session" {
		t.Errorf("SessionID = %s, want test-session", metadata.SessionID)
	}

	if len(metadata.JudgeTargets) != 1 {
		t.Errorf("JudgeTargets length = %d, want 1", len(metadata.JudgeTargets))
	}

	if metadata.JudgeTargets["default"].Type != "openai" {
		t.Errorf("JudgeTargets[default].Type = %s, want openai", metadata.JudgeTargets["default"].Type)
	}

	if len(metadata.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(metadata.Tags))
	}

	if metadata.Duration != time.Minute {
		t.Errorf("Duration = %v, want 1m", metadata.Duration)
	}
}

func TestProviderSpec(t *testing.T) {
	spec := ProviderSpec{
		Type:  "openai",
		Model: "gpt-4",
		ID:    "gpt-4-judge",
	}

	if spec.Type != "openai" {
		t.Errorf("Type = %s, want openai", spec.Type)
	}
	if spec.Model != "gpt-4" {
		t.Errorf("Model = %s, want gpt-4", spec.Model)
	}
	if spec.ID != "gpt-4-judge" {
		t.Errorf("ID = %s, want gpt-4-judge", spec.ID)
	}
}
