package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These tests drive STATEFUL eval handlers through the REAL adapter, which is
// the junction #1704 broke. factory.go documents that any registered eval
// handler can be used as a guardrail, but the adapter built EvalContext inline
// with 3 of its 11 fields, so every handler reading Metadata, Extras,
// ToolCalls or PriorResults saw nothing.
//
// The two failure directions are opposite and both wrong, which is why one test
// is not enough:
//
//   - fail-OPEN: cost_budget with no Metadata computes zero spend, scores 1.0,
//     and never enforces. A budget guardrail that silently ignores the budget.
//   - fail-CLOSED: state_is with no Extras reports "no workflow state
//     available" and scores 0 — and score < 1.0 means enforce, so it blocks
//     every single turn regardless of the actual state.
//
// Every pre-existing adapter test uses either a stub that ignores EvalContext
// or a stateless content/length handler, so none of them could catch this.

// toolResultMessage builds the tool-role message that ExtractToolCalls pairs
// with an assistant tool call to produce a complete ToolCallRecord.
func toolResultMessage(id, name, text string) types.Message {
	result := types.NewTextToolResult(id, name, text)
	return types.Message{Role: "tool", Content: text, ToolResult: &result}
}

// TestGuardrailEnrichment_CostBudgetSeesMetadata pins the fail-open direction.
// The pipeline already puts per-conversation cost on ProviderRequest.Metadata;
// the adapter has to pass it through, or a cost_budget guardrail never fires.
func TestGuardrailEnrichment_CostBudgetSeesMetadata(t *testing.T) {
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_cost_usd": 0.01,
		"direction":    "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
		// Well over the $0.01 budget declared above.
		Metadata: map[string]any{"total_cost": 5.0},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "an expensive reply"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow,
		"cost_budget must fire when Metadata reports spend above the budget; "+
			"if the adapter drops Metadata it computes zero cost and allows forever")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// The under-budget half of that pair is deliberately absent, and this is the
// reason: cost_budget scores a *ratio* (1.0 - spend/budget), not a verdict, so
// any nonzero spend scores below 1.0 — and the adapter reads score < 1.0 as
// "enforce". Spending $0.02 of a $10 budget therefore blocks the turn. That
// is a pre-existing mismatch between continuous-score handlers and a binary
// gate, not something enrichment introduced; before enrichment it was merely
// unreachable, because a nil Metadata map always computed zero spend. It needs
// its own decision (EvalResult already has a MetricValue field for metrics,
// which is where a ratio belongs) and is tracked separately. The
// guardrail_triggered pair below supplies the over-fire protection that this
// missing half would otherwise have given.

// TestGuardrailEnrichment_GuardrailTriggeredSeesPriorResults pins PriorResults,
// which the adapter seeded as nil. guardrail_triggered exists to inspect
// earlier guardrail outcomes, so as a guardrail itself it was reading an empty
// slice and concluding nothing had run.
func TestGuardrailEnrichment_GuardrailTriggeredSeesPriorResults(t *testing.T) {
	hook, err := NewGuardrailHook("guardrail_triggered", map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
		"direction":      "output",
	})
	require.NoError(t, err)

	// A failed banned_words validation on the assistant turn is what
	// validationsToPriorResults converts into a PriorResult.
	blocked := types.Message{
		Role:    "assistant",
		Content: "blocked",
		Validations: []types.ValidationResult{
			{ValidatorType: "banned_words", Passed: false},
		},
	}
	req := &hooks.ProviderRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	resp := &hooks.ProviderResponse{Message: blocked}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"banned_words did trigger and should_trigger=true, so this is satisfied; "+
			"a nil PriorResults slice makes it conclude the validator never ran")
}

// TestGuardrailEnrichment_GuardrailTriggeredFiresWhenValidatorAbsent is the
// discriminating half: enrichment must not make guardrail_triggered pass
// unconditionally.
func TestGuardrailEnrichment_GuardrailTriggeredFiresWhenValidatorAbsent(t *testing.T) {
	hook, err := NewGuardrailHook("guardrail_triggered", map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
		"direction":      "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	resp := &hooks.ProviderResponse{
		// No Validations at all, so banned_words never ran.
		Message: types.Message{Role: "assistant", Content: "a clean reply"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow,
		"banned_words never ran, so should_trigger=true is unsatisfied")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestGuardrailEnrichment_StateIsSeesWorkflowExtras pins the fail-closed
// direction, which is the more damaging of the two: before the fix this
// guardrail blocked every turn, because a nil Extras map reads as "no workflow
// state available" and that scores 0.
func TestGuardrailEnrichment_StateIsSeesWorkflowExtras(t *testing.T) {
	hook, err := NewGuardrailHook("state_is", map[string]any{
		"state":     "checkout",
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "pay now"},
			{
				Role:    "assistant",
				Content: "taking you to checkout",
				Meta:    map[string]any{"_workflow_current_state": "checkout"},
			},
		},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "taking you to checkout"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"the workflow really is in state \"checkout\", so this guardrail must "+
			"allow; dropping Extras makes it block every turn instead")
}

// TestGuardrailEnrichment_StateIsFiresOnWrongState is the discriminating other
// half: enrichment must not turn state_is into an unconditional allow.
func TestGuardrailEnrichment_StateIsFiresOnWrongState(t *testing.T) {
	hook, err := NewGuardrailHook("state_is", map[string]any{
		"state":     "checkout",
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "still browsing",
				Meta:    map[string]any{"_workflow_current_state": "browsing"},
			},
		},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "still browsing"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow, "state is \"browsing\", not the required \"checkout\"")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestGuardrailEnrichment_ToolsCalledSeesToolCalls covers the widest-reaching
// field: ToolCalls is read by roughly thirty handlers, all of which were blind
// as guardrails. ToolCalls is derivable from the message history the adapter
// already holds, so there is no excuse for it being empty.
func TestGuardrailEnrichment_ToolsCalledSeesToolCalls(t *testing.T) {
	hook, err := NewGuardrailHook("tools_called", map[string]any{
		"tools":     []any{"lookup_order"},
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "where is my order"},
			{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "lookup_order", Args: []byte(`{"id":"A1"}`)},
				},
			},
			toolResultMessage("c1", "lookup_order", "shipped"),
		},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "it shipped"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"lookup_order was called in the transcript, so tools_called is satisfied; "+
			"an empty ToolCalls slice makes this guardrail block every turn")
}

// TestGuardrailEnrichment_ToolsCalledFiresWhenToolAbsent is the discriminating
// half of the ToolCalls pair: enrichment must not make tools_called pass for a
// transcript in which the tool was never called.
func TestGuardrailEnrichment_ToolsCalledFiresWhenToolAbsent(t *testing.T) {
	hook, err := NewGuardrailHook("tools_called", map[string]any{
		"tools":     []any{"lookup_order"},
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "where is my order"}},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "I cannot check that"},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow, "lookup_order was never called in this transcript")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestGuardrailEnrichment_PreservesCurrentMessageScope guards the fix against
// regressing #1679. Enrichment must not start inferring CurrentOutput from the
// last assistant turn the way BuildEvalContext does: an input guardrail judges
// the USER's message, which a transcript scan filtered to assistant role would
// never see. That exact inference was the #1679 bug.
func TestGuardrailEnrichment_PreservesCurrentMessageScope(t *testing.T) {
	hook, err := NewGuardrailHook("banned_words", map[string]any{
		"words":     []any{"wire transfer"},
		"direction": "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			// An earlier clean assistant turn. If enrichment re-derived
			// CurrentOutput from the last assistant message, or widened the
			// scope to the whole transcript, the user's banned text below
			// would stop being what gets judged.
			{Role: "assistant", Content: "how can I help?"},
			{Role: "user", Content: "please arrange a wire transfer"},
		},
		Metadata: map[string]any{"total_cost": 0.0},
	}

	d := hook.BeforeCall(context.Background(), req)

	require.False(t, d.Allow,
		"banned_words with direction=input must still judge the user's message "+
			"after enrichment (#1679 must not regress)")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}
