package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestPipeline_AttachesClassifyRegistry(t *testing.T) {
	reg := classify.NewRegistry()

	var seen *classify.Registry
	probe := NewStageFunc("probe", StageTypeTransform, func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		seen = classify.FromContext(ctx)
		for e := range in {
			out <- e
		}
		return nil
	})

	cfg := DefaultPipelineConfig()
	cfg.ClassifyRegistry = reg
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(probe).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	in := make(chan StreamElement)
	out, err := p.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	close(in)
	for range out { //nolint:revive // drain
	}

	if seen != reg {
		t.Fatalf("stage did not observe the configured classify registry (got %v)", seen)
	}
}

// TestPipelineRollup_DoesNotDropBreakdown is a drop-guard: accumulateResult's
// per-message cost roll-up must preserve Quantities/Breakdown carried on each
// assistant message's CostInfo, not just sum the flat headline fields.
func TestPipelineRollup_DoesNotDropBreakdown(t *testing.T) {
	m1 := &types.CostInfo{
		Quantities: map[string]float64{base.UnitInputToken: 100},
		Breakdown: []types.CostLineItem{
			{Provider: "x", Capability: "inference", Unit: base.UnitInputToken, Quantity: 100, USD: 0.1},
		},
		InputCostUSD: 0.1,
		TotalCost:    0.1,
		InputTokens:  100,
	}
	m2 := &types.CostInfo{
		Quantities: map[string]float64{base.UnitCacheWriteToken: 40},
		Breakdown: []types.CostLineItem{
			{Provider: "x", Capability: "inference", Unit: base.UnitCacheWriteToken, Quantity: 40, USD: 0.05},
		},
	}

	out := make(chan StreamElement, 2)
	out <- StreamElement{Message: &types.Message{Role: roleAssistant, CostInfo: m1}}
	out <- StreamElement{Message: &types.Message{Role: roleAssistant, CostInfo: m2}}
	close(out)

	p := &StreamPipeline{}
	result, err := p.accumulateResult(out)
	if err != nil {
		t.Fatalf("accumulateResult: %v", err)
	}

	if got := result.CostInfo.Quantities[base.UnitCacheWriteToken]; got != 40 {
		t.Fatalf("cache-write must survive roll-up, got %v", got)
	}
	if diff := result.CostInfo.TotalCost - 0.15; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected TotalCost ~0.15, got %v", result.CostInfo.TotalCost)
	}
}
