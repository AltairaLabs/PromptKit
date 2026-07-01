package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// minimalEngineForWorkflow builds an Engine directly (bypassing NewEngine's
// side-effecting buildMediaStorage/buildStateStore) with just the fields that
// initWorkflow reads. Used by inline-composition tests only.
func minimalEngineForWorkflow(cfg *arenaconfig.Config) *Engine {
	return &Engine{
		config:       cfg,
		toolRegistry: tools.NewRegistry(),
		// promptRegistry is not used by initWorkflow; nil is safe here.
		promptRegistry: (*prompt.Registry)(nil),
	}
}

// TestInitWorkflow_InlineCompositionsLoaded verifies that when config.Compositions
// is set (the inline Arena path), initWorkflow parses them and populates
// config.LoadedPack.Compositions so that buildCompositionResolver can find them
// at turn time. This covers the fix in execution_workflow_integration.go.
func TestInitWorkflow_InlineCompositionsLoaded(t *testing.T) {
	// Minimal workflow spec — needed because initWorkflow returns early if
	// config.Workflow is nil.
	workflowRaw := map[string]interface{}{
		"version": 1,
		"entry":   "start",
		"states": map[string]interface{}{
			"start": map[string]interface{}{
				"prompt_task":   "start-task",
				"orchestration": "composition",
				"composition":   "flow",
			},
		},
	}

	// Inline compositions: one named "flow" with a single tool step.
	compositionsRaw := map[string]interface{}{
		"flow": map[string]interface{}{
			"version": 1,
			"steps": []interface{}{
				map[string]interface{}{
					"id":   "step1",
					"kind": "tool",
					"tool": "echo",
				},
			},
		},
	}

	cfg := &arenaconfig.Config{
		Workflow:     workflowRaw,
		Compositions: compositionsRaw,
	}

	eng := minimalEngineForWorkflow(cfg)

	if err := eng.initWorkflow(); err != nil {
		t.Fatalf("initWorkflow() returned unexpected error: %v", err)
	}

	// LoadedPack must have been created and populated.
	if cfg.LoadedPack == nil {
		t.Fatal("initWorkflow() did not create config.LoadedPack when Compositions was set")
	}
	if cfg.LoadedPack.Compositions == nil {
		t.Fatal("initWorkflow() did not populate config.LoadedPack.Compositions")
	}
	comp, ok := cfg.LoadedPack.Compositions["flow"]
	if !ok || comp == nil {
		t.Fatalf("initWorkflow() did not add 'flow' to LoadedPack.Compositions; got: %v",
			cfg.LoadedPack.Compositions)
	}
	if len(comp.Steps) != 1 || comp.Steps[0].ID != "step1" {
		t.Errorf("composition 'flow' has unexpected steps: %+v", comp.Steps)
	}
}

// TestInitWorkflow_NoCompositions verifies that when config.Compositions is nil,
// initWorkflow does not create a LoadedPack (no-op path).
func TestInitWorkflow_NoCompositions(t *testing.T) {
	workflowRaw := map[string]interface{}{
		"version": 1,
		"entry":   "start",
		"states": map[string]interface{}{
			"start": map[string]interface{}{
				"prompt_task": "start-task",
			},
		},
	}

	cfg := &arenaconfig.Config{
		Workflow:     workflowRaw,
		Compositions: nil,
	}

	eng := minimalEngineForWorkflow(cfg)

	if err := eng.initWorkflow(); err != nil {
		t.Fatalf("initWorkflow() returned unexpected error: %v", err)
	}

	// LoadedPack should not have been created when Compositions is nil.
	if cfg.LoadedPack != nil {
		t.Errorf("initWorkflow() unexpectedly created LoadedPack when Compositions was nil: %+v", cfg.LoadedPack)
	}
}

// TestInitWorkflow_MergesIntoExistingLoadedPack verifies that when config.LoadedPack
// is already populated (e.g. from a compiled pack file), initWorkflow merges inline
// compositions into the existing map without replacing it.
func TestInitWorkflow_MergesIntoExistingLoadedPack(t *testing.T) {
	workflowRaw := map[string]interface{}{
		"version": 1,
		"entry":   "start",
		"states": map[string]interface{}{
			"start": map[string]interface{}{
				"prompt_task": "start-task",
			},
		},
	}

	compositionsRaw := map[string]interface{}{
		"inline-flow": map[string]interface{}{
			"version": 1,
			"steps": []interface{}{
				map[string]interface{}{"id": "s1", "kind": "tool", "tool": "echo"},
			},
		},
	}

	existingPack := &prompt.Pack{
		ID: "pre-existing",
	}

	cfg := &arenaconfig.Config{
		Workflow:     workflowRaw,
		Compositions: compositionsRaw,
		LoadedPack:   existingPack,
	}

	eng := minimalEngineForWorkflow(cfg)

	if err := eng.initWorkflow(); err != nil {
		t.Fatalf("initWorkflow() returned unexpected error: %v", err)
	}

	// The existing pack should still be the same pointer.
	if cfg.LoadedPack != existingPack {
		t.Error("initWorkflow() replaced config.LoadedPack instead of merging into it")
	}
	if cfg.LoadedPack.Compositions["inline-flow"] == nil {
		t.Error("initWorkflow() did not merge 'inline-flow' into existing LoadedPack.Compositions")
	}
}
