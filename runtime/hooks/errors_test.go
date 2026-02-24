package hooks

import "testing"

func TestHookDeniedError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  HookDeniedError
		want string
	}{
		{
			name: "provider before",
			err: HookDeniedError{
				HookName: "cost-budget",
				HookType: "provider_before",
				Reason:   "budget exceeded",
			},
			want: `hook "cost-budget" (provider_before) denied: budget exceeded`,
		},
		{
			name: "tool before",
			err: HookDeniedError{
				HookName: "tool-policy",
				HookType: "tool_before",
				Reason:   "forbidden tool",
			},
			want: `hook "tool-policy" (tool_before) denied: forbidden tool`,
		},
		{
			name: "chunk",
			err: HookDeniedError{
				HookName: "banned-words",
				HookType: "chunk",
				Reason:   "contains banned word",
				Metadata: map[string]any{"validator_type": "banned_words"},
			},
			want: `hook "banned-words" (chunk) denied: contains banned word`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
