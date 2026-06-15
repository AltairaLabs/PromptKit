package engine

// TestExecuteRun_CompositionRecorder is an end-to-end Arena integration test
// (RFC 0010 Task 7): verifies that per-run composition recorder is wired
// through the full execution path so composition_* assertions can read
// per-step observability data.
//
// The composition has:
//   - a branch step ("route") that chooses "leaf_a" when the input flag == "a"
//   - two leaf tool steps ("leaf_a", "leaf_b") with echo executors
//   - a parallel step ("par") with two branches ("p1", "p2")
//   - a final step ("fin") that echoes the parallel output
//
// The scenario declares ConversationAssertions using composition_step_output,
// composition_branch_taken, composition_parallel_complete, and composition_output.
// All four must pass, which proves:
//  1. The recorder is reset per turn (no stale data from a hypothetical prior turn).
//  2. The same recorder is both wired into the CompositionStage (recording)
//     and set as the EvalOrchestrator's CompositionMetadataProvider (reading).
//  3. The EvalOrchestrator clone used for this run has the recorder set (not nil).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register composition_* and assertion handlers
)

// echoRecorderExecutor echoes its JSON args back unchanged (same as echoE2EExecutor
// in the Task 4 test but with a distinct Name so both tests can coexist).
type echoRecorderExecutor struct{ name string }

func (e *echoRecorderExecutor) Name() string { return e.name }
func (e *echoRecorderExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	return args, nil
}

// TestExecuteRun_CompositionRecorder runs a full Arena executeRun with a
// workflow entry state that has orchestration: composition. The composition
// contains a branch, two parallel branches, and leaf tool steps. After the run,
// the ConversationAssertionResults on the stored run metadata are inspected to
// confirm all composition_* assertions passed.
func TestExecuteRun_CompositionRecorder(t *testing.T) {
	ctx := context.Background()

	// --- 1. Build the composition:
	//
	//   route   — branch: if input.flag == "a" → "leaf_a", else → "leaf_b"
	//   leaf_a  — echo tool (taken path)
	//   leaf_b  — echo tool (depends on leaf_a, so only runs when leaf_a ran)
	//   par     — parallel of two echo branches (p1, p2), reduced into "m"
	//   fin     — echo tool consuming par.output.m (composition output)
	comp := &composition.Composition{
		Version: 1,
		Output:  "fin",
		Steps: []*composition.Step{
			{
				ID:   "route",
				Kind: composition.KindBranch,
				Predicate: &composition.Predicate{
					Path:  "${input.flag}",
					Op:    "equals",
					Value: "a",
				},
				Then: "leaf_a",
				Else: "leaf_b",
			},
			{
				ID:   "leaf_a",
				Kind: composition.KindTool,
				Tool: "echo-rec",
				Args: map[string]any{"step": "leaf_a", "value": "from-branch"},
			},
			{
				// leaf_b depends on leaf_a so it only runs when leaf_a is skipped
				// (i.e. when the branch takes "leaf_b"). For flag=="a" this is skipped.
				ID:        "leaf_b",
				Kind:      composition.KindTool,
				Tool:      "echo-rec",
				Args:      map[string]any{"step": "leaf_b", "value": "not-taken"},
				DependsOn: []string{"leaf_a"},
			},
			{
				ID:   "par",
				Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{
						ID:   "p1",
						Kind: composition.KindTool,
						Tool: "echo-rec",
						Args: map[string]any{"branch": "p1"},
					},
					{
						ID:   "p2",
						Kind: composition.KindTool,
						Tool: "echo-rec",
						Args: map[string]any{"branch": "p2"},
					},
				},
				Reduce: &composition.Reducer{
					Strategy: composition.ReduceBarrier,
					Into:     "m",
				},
			},
			{
				ID:   "fin",
				Kind: composition.KindTool,
				Tool: "echo-rec",
				Args: map[string]any{"fin": "done", "parallel": "${par.output.m}"},
			},
		},
	}

	// --- 2. Build the workflow spec.
	wfSpec := &workflow.Spec{
		Version: 1,
		Entry:   "compose",
		States: map[string]*workflow.State{
			"compose": {
				PromptTask:    "compose-rec-task",
				Orchestration: workflow.OrchestrationComposition,
				Composition:   "rec-comp",
			},
		},
	}

	// --- 3. Tool registry with the echo executor.
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name:        "echo-rec",
		Description: "echoes args",
		Mode:        "echo-rec",
	}))
	reg.RegisterExecutor(&echoRecorderExecutor{name: "echo-rec"})

	// --- 4. Pack carrying the composition.
	pack := &prompt.Pack{
		ID:       "recorder-test-pack",
		Workflow: wfSpec,
		Compositions: map[string]*composition.Composition{
			"rec-comp": comp,
		},
	}

	// --- 5. EvalOrchestrator with the full default registry (includes
	//        composition_* handlers, assertion wrapper, etc.).
	evalReg := evals.NewEvalTypeRegistry()
	evalOrch := NewEvalOrchestrator(evalReg, nil, false, nil, "")

	// --- 6. Scenario with ConversationAssertions that exercise all four
	//        composition_* handlers. These run after the conversation completes
	//        via evaluateConversationAssertions → resolveEvalOrchestrator.
	scenario := &config.Scenario{
		ID:       "recorder-scenario",
		TaskType: "compose-rec-task",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: `{"flag":"a"}`},
		},
		ConversationAssertions: []assertions.AssertionConfig{
			// branch "route" must have taken "leaf_a" (flag == "a")
			{
				Type:    "composition_branch_taken",
				Message: "branch route took leaf_a",
				Params: map[string]any{
					"branch":   "route",
					"expected": "leaf_a",
				},
			},
			// parallel "par" must be complete
			{
				Type:    "composition_parallel_complete",
				Message: "parallel par is complete",
				Params: map[string]any{
					"parallel": "par",
				},
			},
			// leaf_a step must have recorded output containing "leaf_a"
			{
				Type:    "composition_step_output",
				Message: "leaf_a step output recorded",
				Params: map[string]any{
					"step":     "leaf_a",
					"contains": "leaf_a",
				},
			},
			// fin step (composition output) must contain "done"
			{
				Type:    "composition_output",
				Message: "composition output contains done",
				Params: map[string]any{
					"contains": "done",
				},
			},
		},
	}

	// --- 7. Wire the Arena engine.
	mockProv := mock.NewProvider("mock-rec", "test-model", false)
	provReg := providers.NewRegistry()
	provReg.Register(mockProv)

	arenaStore := statestore.NewArenaStateStore()

	pipelineExec := turnexecutors.NewPipelineExecutor(reg, nil)
	scriptedExec := turnexecutors.NewScriptedExecutor(pipelineExec)
	convExec := NewDefaultConversationExecutor(scriptedExec, nil, nil, nil, evalOrch)

	transExec := newWorkflowTransitionExecutor(wfSpec, reg)

	e := &Engine{
		config: &config.Config{
			LoadedPack: pack,
		},
		scenarios: map[string]*config.Scenario{
			"recorder-scenario": scenario,
		},
		evals:                make(map[string]*config.Eval),
		providers:            make(map[string]*config.Provider),
		providerRegistry:     provReg,
		stateStore:           arenaStore,
		conversationExecutor: convExec,
		toolRegistry:         reg,
		workflowSpec:         wfSpec,
		workflowTransExec:    transExec,
		evalOrchestrator:     evalOrch,
	}

	// --- 8. Execute.
	combo := RunCombination{
		ScenarioID: "recorder-scenario",
		ProviderID: "mock-rec",
		Region:     "default",
	}
	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err)
	require.NotEmpty(t, runID)

	// --- 9. Load the stored run result.
	result, err := arenaStore.GetResult(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, result, "run result must be stored")

	// The run must not have failed.
	assert.Empty(t, result.Error, "composition run must not error")

	// --- 10. Assert all four composition_* conversation assertions passed.
	//
	//   ConversationAssertions.Results is populated by saveRunMetadata, which
	//   reads from ConversationResult.ConversationAssertionResults.
	summary := result.ConversationAssertions
	require.NotEmpty(t, summary.Results,
		"expected ConversationAssertions.Results to be populated (got %d total, %d failed)",
		summary.Total, summary.Failed)

	for _, res := range summary.Results {
		assert.True(t, res.Passed,
			"assertion %q must pass; message: %s; details: %v",
			res.Type, res.Message, res.Details)
	}
}
