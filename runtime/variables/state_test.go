package variables

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func TestStateProvider_Name(t *testing.T) {
	tests := []struct {
		name     string
		provider *StateProvider
		want     string
	}{
		{
			name:     "default name",
			provider: NewStateProvider(),
			want:     "state",
		},
		{
			name:     "with prefix",
			provider: NewStatePrefixProvider("user_", false),
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
		name     string
		provider *StateProvider
		state    *statestore.ConversationState
		want     map[string]string
		wantErr  bool
	}{
		{
			name:     "nil state returns nil",
			provider: NewStateProvider(),
			state:    nil,
			want:     nil,
		},
		{
			name:     "nil metadata returns nil",
			provider: NewStateProvider(),
			state:    &statestore.ConversationState{},
			want:     nil,
		},
		{
			name:     "extracts all metadata",
			provider: NewStateProvider(),
			state: &statestore.ConversationState{
				Metadata: map[string]interface{}{
					"user_name": "Alice",
					"user_id":   123,
					"active":    true,
				},
			},
			want: map[string]string{
				"user_name": "Alice",
				"user_id":   "123",
				"active":    "true",
			},
		},
		{
			name:     "filters by prefix",
			provider: NewStatePrefixProvider("user_", false),
			state: &statestore.ConversationState{
				Metadata: map[string]interface{}{
					"user_name":    "Alice",
					"user_id":      123,
					"session_type": "chat",
				},
			},
			want: map[string]string{
				"user_name": "Alice",
				"user_id":   "123",
			},
		},
		{
			name:     "strips prefix when configured",
			provider: NewStatePrefixProvider("user_", true),
			state: &statestore.ConversationState{
				Metadata: map[string]interface{}{
					"user_name":    "Alice",
					"user_id":      123,
					"session_type": "chat",
				},
			},
			want: map[string]string{
				"name": "Alice",
				"id":   "123",
			},
		},
		{
			name:     "empty prefix includes all",
			provider: NewStatePrefixProvider("", true),
			state: &statestore.ConversationState{
				Metadata: map[string]interface{}{
					"key1": "value1",
					"key2": "value2",
				},
			},
			want: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.provider.Provide(context.Background(), tt.state)
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
