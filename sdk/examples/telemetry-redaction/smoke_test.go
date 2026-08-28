package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// TestScriptedModelProducesTheToolCall guards the thing that makes this example
// demonstrate anything.
//
// An earlier version used the file-backed mock repository, whose turns are keyed
// by a scenario id that arena supplies and the SDK does not. It fell through to
// the default response, produced no tool call, and printed "no content" on every
// row — which reads as three passes and proves nothing. A demo that measures
// nothing is worse than no demo, so the script is asserted rather than assumed.
func TestScriptedModelProducesTheToolCall(t *testing.T) {
	m := &scriptedModel{}

	first, err := m.GetTurn(t.Context(), mock.ResponseParams{})
	require.NoError(t, err)
	require.Len(t, first.ToolCalls, 1, "round 1 must call the tool, or nothing is traced")
	assert.Equal(t, accessToken, first.ToolCalls[0].Arguments["access_token"],
		"the credential must be in the arguments; it is the payload under test")
	assert.Equal(t, customerEmail, first.ToolCalls[0].Arguments["customer_email"])

	second, err := m.GetTurn(t.Context(), mock.ResponseParams{})
	require.NoError(t, err)
	assert.Empty(t, second.ToolCalls, "round 2 must end the loop")
	assert.NotEmpty(t, second.Content)
}

// TestRedactorPolicyScrubsArgsOnly pins the policy the example demonstrates:
// field-selective, not blanket.
func TestRedactorPolicyScrubsArgsOnly(t *testing.T) {
	policy := func(field, value string) string {
		if field == events.FieldToolCallArgs {
			return strings.ReplaceAll(value, accessToken, "[REDACTED]")
		}
		return value
	}

	assert.Equal(t, "[REDACTED]", policy(events.FieldToolCallArgs, accessToken))
	assert.Equal(t, accessToken, policy(events.FieldMessageContent, accessToken),
		"a policy scoped to arguments must leave message content alone")
}
