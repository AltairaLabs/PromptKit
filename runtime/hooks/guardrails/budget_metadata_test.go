package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// The budget handlers read spend and latency off EvalContext.Metadata, which
// nothing populated on the hook path. cost_budget therefore computed zero spend
// and never fired, and latency_budget treated its missing key as a hard fail —
// scoring 0, which the adapter reads as enforce — so it blocked every turn
// regardless of actual latency (#1707).
//
// Both values are already available to the adapter: latency arrives on
// ProviderResponse.LatencyMs, and per-message CostInfo carries tokens and spend.
// Nothing needs threading through the pipeline.

func costMsg(role, content string, cost float64, in, out int) types.Message {
	return types.Message{
		Role:    role,
		Content: content,
		CostInfo: &types.CostInfo{
			TotalCost:    cost,
			InputTokens:  in,
			OutputTokens: out,
		},
	}
}

// TestBudgetMetadata_LatencyBudgetAllowsFastCall is the discriminating case for
// latency. Before the fix this enforced — not because the call was slow, but
// because the key was absent — so a passing test had to be one that only a real
// implementation satisfies.
func TestBudgetMetadata_LatencyBudgetAllowsFastCall(t *testing.T) {
	hook, err := NewGuardrailHook("latency_budget", map[string]any{
		"max_ms":    1000.0,
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	resp := &hooks.ProviderResponse{
		Message:   types.Message{Role: "assistant", Content: "quick reply"},
		LatencyMs: 50,
	}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"50ms is far inside a 1000ms budget; a missing latency_ms key made this "+
			"guardrail block every turn instead")
}

// TestBudgetMetadata_LatencyBudgetEnforcesSlowCall is the other half: seeding
// latency must not turn the guardrail into an unconditional allow.
func TestBudgetMetadata_LatencyBudgetEnforcesSlowCall(t *testing.T) {
	hook, err := NewGuardrailHook("latency_budget", map[string]any{
		"max_ms":    100.0,
		"direction": "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	resp := &hooks.ProviderResponse{
		Message:   types.Message{Role: "assistant", Content: "slow reply"},
		LatencyMs: 5000,
	}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow, "5000ms blows a 100ms budget")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestBudgetMetadata_CostBudgetEnforcesOverspend pins the fail-open half:
// without derived spend, cost_budget saw $0 and allowed forever.
func TestBudgetMetadata_CostBudgetEnforcesOverspend(t *testing.T) {
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_cost_usd": 0.01,
		"direction":    "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "hi"},
			costMsg("assistant", "expensive", 3.0, 1000, 500),
			{Role: "user", Content: "again"},
			costMsg("assistant", "also expensive", 2.0, 900, 400),
		},
	}
	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "reply"}}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow,
		"$5 of accumulated CostInfo blows a $0.01 budget; without derived spend "+
			"this guardrail computed $0 and never fired")
	assert.True(t, d.Enforced)
}

// TestBudgetMetadata_CostBudgetAllowsUnderBudgetWithThreshold is the half that
// was impossible before #1711 and is only reachable now that both fixes are in.
// cost_budget scores a ratio, so being inside budget still scores below 1.0 —
// it needs min_score to express "within budget is acceptable".
func TestBudgetMetadata_CostBudgetAllowsUnderBudgetWithThreshold(t *testing.T) {
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_cost_usd": 10.0,
		"min_score":    0.9, // tolerate spending up to 10% of the budget
		"direction":    "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "hi"},
			costMsg("assistant", "cheap", 0.02, 100, 50),
		},
	}
	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "reply"}}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"$0.02 of a $10 budget scores 0.998, which clears min_score 0.9")
}

// TestBudgetMetadata_CostBudgetSeesTokenTotals covers the token thresholds,
// which are separate params reading separate derived keys.
func TestBudgetMetadata_CostBudgetSeesTokenTotals(t *testing.T) {
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_total_tokens": 500.0,
		"direction":        "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			costMsg("assistant", "one", 0, 400, 300),
		},
	}
	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "reply"}}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow, "700 total tokens exceeds a 500 cap")
	assert.True(t, d.Enforced)
}

// TestBudgetMetadata_CallerMetadataWins keeps the escape hatch: a caller that
// puts its own session-wide totals on ProviderRequestMetadata must not have them
// silently replaced by per-request derivation. Arena threading cross-turn spend
// is the motivating case.
func TestBudgetMetadata_CallerMetadataWins(t *testing.T) {
	// min_score is load-bearing for this test, not decoration: cost_budget
	// scores a ratio, so at the default 1.0 threshold *any* nonzero spend
	// enforces and the assertion below would hold even if the caller's value
	// were discarded. With a threshold, "under budget" genuinely allows, so the
	// test can only pass when the caller's $99 is what gets read.
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_cost_usd": 1.0,
		"min_score":    0.5,
		"direction":    "output",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			// Derived spend from CostInfo is trivial and would be under budget.
			costMsg("assistant", "cheap", 0.01, 10, 10),
		},
		// The caller knows the run has already spent far more than this turn.
		Metadata: map[string]any{"total_cost": 99.0},
	}
	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "reply"}}

	d := hook.AfterCall(context.Background(), req, resp)

	require.False(t, d.Allow,
		"the caller's $99 must win over $0.01 derived from this turn's messages")
	assert.True(t, d.Enforced)
}

// TestBudgetMetadata_InputDirectionSeesSpend covers the BeforeCall path, which
// needs its own seeding call. A spend guardrail on the input side is the useful
// case — refusing to send another expensive request is cheaper than judging the
// reply after paying for it.
func TestBudgetMetadata_InputDirectionSeesSpend(t *testing.T) {
	hook, err := NewGuardrailHook("cost_budget", map[string]any{
		"max_cost_usd": 0.01,
		"direction":    "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			costMsg("assistant", "expensive earlier turn", 5.0, 1000, 500),
			// BeforeCall only evaluates when the last message is the user's.
			{Role: "user", Content: "and another thing"},
		},
	}

	d := hook.BeforeCall(context.Background(), req)

	require.False(t, d.Allow,
		"$5 already spent blows a $0.01 budget before this call is even sent; "+
			"BeforeCall needs its own seeding, not just AfterCall")
	assert.True(t, d.Enforced)
}

// TestBudgetMetadata_InputDirectionHasNoLatency documents the asymmetry: there is
// no latency before the call, so an input-direction latency guardrail cannot be
// satisfied and must not be silently treated as passing.
func TestBudgetMetadata_InputDirectionHasNoLatency(t *testing.T) {
	hook, err := NewGuardrailHook("latency_budget", map[string]any{
		"max_ms":    1000.0,
		"direction": "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

	d := hook.BeforeCall(context.Background(), req)

	require.False(t, d.Allow,
		"no call has happened yet, so there is no latency to judge; failing "+
			"closed is correct and latency_budget is an output-only guardrail")
}
