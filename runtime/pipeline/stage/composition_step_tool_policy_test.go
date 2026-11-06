package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
)

// A step's tool policy starts from the host's and only narrows it.
func TestStepToolPolicy_StartsFromTheHostPolicy(t *testing.T) {
	base := &pipeline.ToolPolicy{MaxRounds: 5, MaxCallsPerMinute: 3, StopOnTool: "done", Blocklist: []string{"rm"}}
	agent := func(term *composition.Termination) *composition.Step {
		return &composition.Step{Kind: composition.KindAgent, Termination: term}
	}

	got := stepToolPolicy(agent(nil), base)
	assert.Equal(t, 5, got.MaxRounds)
	assert.Equal(t, 3, got.MaxCallsPerMinute, "runtime-only fields come from the host")
	assert.Equal(t, "done", got.StopOnTool)
	got.Blocklist[0] = "changed"
	assert.Equal(t, "rm", base.Blocklist[0], "the host's policy is not shared")

	got = stepToolPolicy(agent(&composition.Termination{MaxSteps: packspec.Ptr(2), ToolCalled: "finish"}), base)
	assert.Equal(t, 2, got.MaxRounds, "a lower max_steps narrows")
	assert.Equal(t, "finish", got.StopOnTool, "the step's tool_called replaces the host's")

	got = stepToolPolicy(agent(&composition.Termination{MaxSteps: packspec.Ptr(9)}), base)
	assert.Equal(t, 5, got.MaxRounds, "a higher max_steps cannot widen the host's limit")

	got = stepToolPolicy(&composition.Step{Kind: composition.KindPrompt}, base)
	assert.Equal(t, 1, got.MaxRounds, "a prompt step is single-round")

	got = stepToolPolicy(agent(&composition.Termination{MaxSteps: packspec.Ptr(4)}), nil)
	assert.Equal(t, 4, got.MaxRounds, "without a host policy the step's bounds alone")
}
