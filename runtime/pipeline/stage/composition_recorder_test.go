package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestCompositionRecorder exercises NewCompositionStageWithRecorder end-to-end:
// branch selection, parallel execution, and leaf steps are all captured by
// CompositionMetadata, and Reset() clears the state.
func TestCompositionRecorder(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo") // defined in composition_executor_test.go

	// Composition layout:
	//   br      — if input.flag == "a" → take "leaf_a", else → take "leaf_b"
	//   leaf_a  — echo tool (taken when flag=="a")
	//   leaf_b  — echo tool (skipped when flag=="a", depends on leaf_a so it is
	//             unreachable when leaf_a is skipped, not the case here)
	//   par     — parallel of two echo branches, barrier-reduced into "m"
	//   fin     — echo tool that consumes par output
	comp := &composition.Composition{
		Version: 1, Output: "fin",
		Steps: []*composition.Step{
			{
				ID:   "br",
				Kind: composition.KindBranch,
				Predicate: &composition.Predicate{
					Path:  "${input.flag}",
					Op:    "equals",
					Value: "a",
				},
				Then: "leaf_a",
				Else: "leaf_b",
			},
			{ID: "leaf_a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "A"}},
			{ID: "leaf_b", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "B"}, DependsOn: []string{"leaf_a"}},
			{
				ID:   "par",
				Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "p1", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"n": "1"}},
					{ID: "p2", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"n": "2"}},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceBarrier, Into: "m"},
			},
			{ID: "fin", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"out": "${par.output.m}"}},
		},
	}

	rec := NewCompositionRecorder()
	cs := NewCompositionStageWithRecorder("rec-comp", comp, CompositionExecutorDeps{ToolRegistry: reg}, rec)

	pipe, err := NewPipelineBuilder().Chain(cs).Build()
	if err != nil {
		t.Fatal(err)
	}

	msg := types.Message{Role: "user", Content: `{"flag":"a"}`}
	res, err := pipe.ExecuteSync(context.Background(), NewMessageElement(&msg))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Response == nil {
		t.Fatal("expected a response")
	}

	meta := rec.CompositionMetadata()

	// --- composition_step_outputs: must contain every completed leaf step ---
	stepOutputsRaw, ok := meta["composition_step_outputs"]
	if !ok {
		t.Fatal("CompositionMetadata missing composition_step_outputs")
	}
	stepOutputs, ok := stepOutputsRaw.(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("composition_step_outputs has unexpected type %T", stepOutputsRaw)
	}
	for _, wantID := range []string{"leaf_a", "fin"} {
		if _, found := stepOutputs[wantID]; !found {
			t.Errorf("composition_step_outputs missing step %q; got keys: %v", wantID, mapKeys(stepOutputs))
		}
	}

	// --- composition_branch_taken: branch "br" must record the taken target ---
	branchRaw, ok := meta["composition_branch_taken"]
	if !ok {
		t.Fatal("CompositionMetadata missing composition_branch_taken")
	}
	branches, ok := branchRaw.(map[string]string)
	if !ok {
		t.Fatalf("composition_branch_taken has unexpected type %T", branchRaw)
	}
	if taken, found := branches["br"]; !found {
		t.Error("composition_branch_taken missing entry for step 'br'")
	} else if taken != "leaf_a" {
		t.Errorf("composition_branch_taken['br'] = %q, want 'leaf_a'", taken)
	}

	// --- composition_parallel_status: parallel step "par" must be "complete" ---
	parallelRaw, ok := meta["composition_parallel_status"]
	if !ok {
		t.Fatal("CompositionMetadata missing composition_parallel_status")
	}
	parallel, ok := parallelRaw.(map[string]string)
	if !ok {
		t.Fatalf("composition_parallel_status has unexpected type %T", parallelRaw)
	}
	if status, found := parallel["par"]; !found {
		t.Error("composition_parallel_status missing entry for step 'par'")
	} else if status != "complete" {
		t.Errorf("composition_parallel_status['par'] = %q, want 'complete'", status)
	}

	// --- Reset() clears all data ---
	rec.Reset()
	afterReset := rec.CompositionMetadata()
	if steps, _ := afterReset["composition_step_outputs"].(map[string]json.RawMessage); len(steps) != 0 {
		t.Errorf("after Reset, composition_step_outputs not empty: %v", steps)
	}
	if br, _ := afterReset["composition_branch_taken"].(map[string]string); len(br) != 0 {
		t.Errorf("after Reset, composition_branch_taken not empty: %v", br)
	}
	if par, _ := afterReset["composition_parallel_status"].(map[string]string); len(par) != 0 {
		t.Errorf("after Reset, composition_parallel_status not empty: %v", par)
	}
}

// mapKeys returns the keys of a map[string]json.RawMessage for error messages.
func mapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
