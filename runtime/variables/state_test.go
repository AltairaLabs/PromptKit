package variables

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// mockStateStore is a test helper that implements statestore.Store
type mockStateStore struct {
	state *statestore.ConversationState
	err   error
}

func (m *mockStateStore) Save(_ context.Context, _ *statestore.ConversationState) error {
	return nil
}

func (m *mockStateStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return m.state, m.err
}

func (m *mockStateStore) Fork(_ context.Context, _, _ string) error {
	return nil
}

// mockMetadataStore implements both statestore.Store and statestore.MetadataAccessor.
// It tracks whether LoadMetadata was called to verify the fast path is used.
type mockMetadataStore struct {
	mockStateStore
	metadata         map[string]interface{}
	metadataErr      error
	loadMetaCalled   bool
	loadStateCalled  bool
}

func (m *mockMetadataStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	m.loadStateCalled = true
	return m.mockStateStore.Load(nil, "")
}

func (m *mockMetadataStore) LoadMetadata(_ context.Context, _ string) (map[string]interface{}, error) {
	m.loadMetaCalled = true
	return m.metadata, m.metadataErr
}

func TestStateProvider_Name(t *testing.T) {
	tests := []struct {
		name     string
		provider *StateProvider
		want     string
	}{
		{
			name:     "default name",
			provider: NewStateProvider(nil, "test"),
			want:     "state",
		},
		{
			name:     "with prefix",
			provider: NewStatePrefixProvider(nil, "test", "user_", false),
			want:     "state[user_]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.provider.Name(); got != tt.want {
				t.Errorf("StateProvider.Name() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStateProvider_Provide(t *testing.T) {
	tests := []struct {
		name    string
		store   *mockStateStore
		prefix  string
		strip   bool
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "nil store returns nil",
			store: nil,
			want:  nil,
		},
		{
			name:  "nil state returns nil",
			store: &mockStateStore{state: nil},
			want:  nil,
		},
		{
			name:  "nil metadata returns nil",
			store: &mockStateStore{state: &statestore.ConversationState{}},
			want:  nil,
		},
		{
			name: "extracts all metadata",
			store: &mockStateStore{
				state: &statestore.ConversationState{
					Metadata: map[string]interface{}{
						"user_name": "Alice",
						"user_id":   123,
						"active":    true,
					},
				},
			},
			want: map[string]string{
				"user_name": "Alice",
				"user_id":   "123",
				"active":    "true",
			},
		},
		{
			name: "filters by prefix",
			store: &mockStateStore{
				state: &statestore.ConversationState{
					Metadata: map[string]interface{}{
						"user_name":    "Alice",
						"user_id":      123,
						"session_type": "chat",
					},
				},
			},
			prefix: "user_",
			want: map[string]string{
				"user_name": "Alice",
				"user_id":   "123",
			},
		},
		{
			name: "strips prefix when configured",
			store: &mockStateStore{
				state: &statestore.ConversationState{
					Metadata: map[string]interface{}{
						"user_name":    "Alice",
						"user_id":      123,
						"session_type": "chat",
					},
				},
			},
			prefix: "user_",
			strip:  true,
			want: map[string]string{
				"name": "Alice",
				"id":   "123",
			},
		},
		{
			name: "empty prefix includes all",
			store: &mockStateStore{
				state: &statestore.ConversationState{
					Metadata: map[string]interface{}{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			prefix: "",
			strip:  true,
			want: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var provider *StateProvider
			if tt.prefix != "" || tt.strip {
				provider = NewStatePrefixProvider(tt.store, "test-conv", tt.prefix, tt.strip)
			} else {
				provider = NewStateProvider(tt.store, "test-conv")
			}

			got, err := provider.Provide(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("StateProvider.Provide() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("StateProvider.Provide() got %d vars, want %d", len(got), len(tt.want))
				return
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("StateProvider.Provide()[%s] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestStateProvider_UsesMetadataAccessor(t *testing.T) {
	// Verify that when the store implements MetadataAccessor,
	// the provider uses LoadMetadata instead of Load (avoids deep-copying messages).
	store := &mockMetadataStore{
		metadata: map[string]interface{}{
			"user_name": "Alice",
			"count":     42,
		},
	}

	provider := NewStateProvider(store, "test-conv")
	got, err := provider.Provide(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have used LoadMetadata, not Load
	if !store.loadMetaCalled {
		t.Error("expected LoadMetadata to be called")
	}
	if store.loadStateCalled {
		t.Error("Load should not be called when MetadataAccessor is available")
	}

	// Verify the values
	if got["user_name"] != "Alice" {
		t.Errorf("user_name = %v, want Alice", got["user_name"])
	}
	if got["count"] != "42" {
		t.Errorf("count = %v, want 42", got["count"])
	}
}

func TestStateProvider_FallsBackToLoad(t *testing.T) {
	// Verify that when MetadataAccessor is NOT implemented,
	// the provider falls back to Load.
	store := &mockStateStore{
		state: &statestore.ConversationState{
			Metadata: map[string]interface{}{
				"key": "value",
			},
		},
	}

	provider := NewStateProvider(store, "test-conv")
	got, err := provider.Provide(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["key"] != "value" {
		t.Errorf("key = %v, want value", got["key"])
	}
}

func TestStateProvider_MetadataAccessorNotFound(t *testing.T) {
	// Verify that ErrNotFound from MetadataAccessor returns nil (not error).
	store := &mockMetadataStore{
		metadataErr: statestore.ErrNotFound,
	}

	provider := NewStateProvider(store, "test-conv")
	got, err := provider.Provide(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
