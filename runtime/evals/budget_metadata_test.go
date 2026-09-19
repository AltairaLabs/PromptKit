package evals

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func costMessage(cost float64, in, out, cached int) types.Message {
	return types.Message{
		Role: "assistant",
		CostInfo: &types.CostInfo{
			TotalCost:    cost,
			InputTokens:  in,
			OutputTokens: out,
			CachedTokens: cached,
		},
	}
}

func TestSeedBudgetMetadata_DerivesFromCostInfo(t *testing.T) {
	latency := int64(250)
	messages := []types.Message{
		types.NewUserMessage("no cost on a user turn"),
		costMessage(1.5, 100, 50, 10),
		costMessage(2.5, 200, 80, 20),
	}

	got := SeedBudgetMetadata(nil, messages, &latency)

	assert.InDelta(t, 4.0, got["total_cost"], 1e-9, "spend accumulates across the transcript")
	assert.Equal(t, 300, got["input_tokens"])
	assert.Equal(t, 130, got["output_tokens"])
	assert.Equal(t, 30, got["cached_tokens"])
	assert.InDelta(t, 250.0, got["latency_ms"], 1e-9,
		"latency is seeded as a float because extractFloat64 is what reads it")
}

func TestSeedBudgetMetadata_NilLatencyOmitsTheKey(t *testing.T) {
	got := SeedBudgetMetadata(nil, []types.Message{costMessage(1, 1, 1, 0)}, nil)

	assert.NotContains(t, got, "latency_ms",
		"no completed call means no latency; seeding zero would report every "+
			"prospective request as instant")
	// The spend keys are still present — only latency is conditional.
	assert.Contains(t, got, "total_cost")
}

func TestSeedBudgetMetadata_CallerValuesWin(t *testing.T) {
	latency := int64(10)
	caller := map[string]any{
		"total_cost": 99.0,
		"unrelated":  "kept",
	}

	got := SeedBudgetMetadata(caller, []types.Message{costMessage(0.01, 5, 5, 0)}, &latency)

	assert.InDelta(t, 99.0, got["total_cost"], 1e-9,
		"a host tracking session-wide spend must not have it replaced by this "+
			"turn's derivation")
	assert.Equal(t, "kept", got["unrelated"], "unrelated caller keys survive")
	assert.Equal(t, 5, got["input_tokens"],
		"keys the caller did not set are still derived")
}

func TestSeedBudgetMetadata_DoesNotMutateCallerMap(t *testing.T) {
	// The adapter holds one request metadata map per turn and other hooks read
	// it too; writing derived values into it would leak this guardrail's view
	// into everything downstream.
	caller := map[string]any{"unrelated": "kept"}

	_ = SeedBudgetMetadata(caller, []types.Message{costMessage(1, 1, 1, 0)}, nil)

	assert.Len(t, caller, 1, "the caller's map must be left exactly as it was")
	assert.NotContains(t, caller, "total_cost")
}

func TestSeedBudgetMetadata_NoCostInfoYieldsZeros(t *testing.T) {
	got := SeedBudgetMetadata(nil, []types.Message{
		types.NewUserMessage("hi"),
		types.NewAssistantMessage("no CostInfo attached"),
	}, nil)

	// Zero rather than absent, which matches what cost_budget already does with
	// a missing key — so a transcript without cost data behaves identically
	// before and after seeding.
	assert.InDelta(t, 0.0, got["total_cost"], 1e-9)
	assert.Equal(t, 0, got["input_tokens"])
}
