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
