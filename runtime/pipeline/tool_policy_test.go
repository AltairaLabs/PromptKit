package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

func TestMergeToolPolicy_NilPromptReturnsCallerUnchanged(t *testing.T) {
	caller := &ToolPolicy{MaxRounds: 7}
	assert.Same(t, caller, MergeToolPolicy(caller, nil))
	assert.Nil(t, MergeToolPolicy(nil, nil))
}

func TestMergeToolPolicy_PromptOnly(t *testing.T) {
	got := MergeToolPolicy(nil, &packspec.ToolPolicy{
		MaxRounds:           packspec.Ptr(200),
		MaxToolCallsPerTurn: packspec.Ptr(40),
		ToolChoice:          packspec.Ptr("required"),
		Blocklist:           []string{"delete_kit"},
	})
	require.NotNil(t, got)
	assert.Equal(t, 200, got.MaxRounds)
	assert.Equal(t, 40, got.MaxToolCallsPerTurn)
	assert.Equal(t, "required", got.ToolChoice)
	assert.Equal(t, []string{"delete_kit"}, got.Blocklist)
}

// Limits only narrow: whichever layer sets the lower value wins, in either
// direction, and an unset (0) layer never lowers the other to 0.
func TestMergeToolPolicy_LimitsTakeTheLowerSetValue(t *testing.T) {
	tests := []struct {
		name               string
		caller, prompt     int
		wantRoundsAndCalls int
	}{
		{"caller lower", 5, 200, 5},
		{"prompt lower", 30, 10, 10},
		{"caller unset", 0, 10, 10},
		{"prompt unset", 30, 0, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &ToolPolicy{MaxRounds: tt.caller, MaxToolCallsPerTurn: tt.caller}
			prompt := &packspec.ToolPolicy{}
			if tt.prompt > 0 {
				prompt.MaxRounds = packspec.Ptr(tt.prompt)
				prompt.MaxToolCallsPerTurn = packspec.Ptr(tt.prompt)
			}
			got := MergeToolPolicy(caller, prompt)
			assert.Equal(t, tt.wantRoundsAndCalls, got.MaxRounds)
			assert.Equal(t, tt.wantRoundsAndCalls, got.MaxToolCallsPerTurn)
		})
	}
}

func TestMergeToolPolicy_ToolChoiceAndBlocklist(t *testing.T) {
	caller := &ToolPolicy{ToolChoice: "none", Blocklist: []string{"a", "b"}, MaxCostUSD: 2, StopOnTool: "finish"}
	got := MergeToolPolicy(caller, &packspec.ToolPolicy{
		ToolChoice: packspec.Ptr("required"),
		Blocklist:  []string{"b", "c"},
	})
	assert.Equal(t, "none", got.ToolChoice, "caller's tool_choice wins when set")
	assert.Equal(t, []string{"a", "b", "c"}, got.Blocklist, "blocklists union")
	assert.Equal(t, 2.0, got.MaxCostUSD, "runtime-only fields come from the caller")
	assert.Equal(t, "finish", got.StopOnTool)
	assert.Equal(t, []string{"a", "b"}, caller.Blocklist, "caller is not mutated")
}
