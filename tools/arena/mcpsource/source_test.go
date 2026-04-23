package mcpsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScope_Valid(t *testing.T) {
	for _, s := range []Scope{ScopeRun, ScopeScenario, ScopeSession} {
		assert.True(t, s.Valid(), "scope %q should be valid", s)
	}
}

func TestScope_InvalidValues(t *testing.T) {
	for _, s := range []Scope{"", "repetition", "turn"} {
		assert.False(t, Scope(s).Valid(), "scope %q should be invalid", s)
	}
}

func TestParseScope(t *testing.T) {
	tests := []struct {
		in      string
		want    Scope
		wantErr bool
	}{
		{"run", ScopeRun, false},
		{"scenario", ScopeScenario, false},
		{"session", ScopeSession, false},
		{"", "", true},
		{"bogus", "", true},
	}
	for _, tc := range tests {
		got, err := ParseScope(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q", tc.in)
			continue
		}
		assert.NoError(t, err, "input %q", tc.in)
		assert.Equal(t, tc.want, got)
	}
}
