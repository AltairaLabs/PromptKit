package providers

import "testing"

func TestResolveMaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		defaults  int
		want      int
	}{
		{"request wins over default", 123, 400, 123},
		{"default applies when request unset", 0, 400, 400},
		{"negative request counts as unset", -5, 400, 400},
		{"no request and no default means no limit", 0, 0, 0},
		{"unlimited default means no limit", 0, MaxTokensUnlimited, 0},
		{"request wins over unlimited default", 50, MaxTokensUnlimited, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveMaxTokens(tt.requested, ProviderDefaults{MaxTokens: tt.defaults})
			if got != tt.want {
				t.Errorf("ResolveMaxTokens(%d, %d) = %d, want %d", tt.requested, tt.defaults, got, tt.want)
			}
		})
	}
}
