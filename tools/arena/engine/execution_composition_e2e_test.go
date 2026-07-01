package engine

// TestExecuteRun_CompositionState is an end-to-end Arena integration test
// (RFC 0010 Task 4): verifies that when the workflow entry state has
// orchestration: composition, Arena resolves the composition from LoadedPack,
// stamps ActiveComposition on the TurnRequest, and the turn executes the
// composition rather than an LLM provider call.
//
// The composition is a single tool step ("echo-e2e") that echoes its args back
// as JSON. This avoids mock-response step-keying entirely — no LLM is involved.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// echoE2EExecutor is a tools.Executor that echoes its JSON args back unchanged.
// It is used by the composition step to produce a deterministic output without
// invoking an LLM.
type echoE2EExecutor struct{}

func (e *echoE2EExecutor) Name() string { return "echo-e2e" }
func (e *echoE2EExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

// TestExecuteRun_CompositionState runs an Arena scenario whose workflow entry
// state has orchestration: composition. It asserts that:
//  1. The turn executes the composition's echo step (no LLM call).
//  2. The saved conversation contains an assistant message whose content is the
//     JSON-encoded echo output (i.e. the args the composition passed to the tool).
func TestExecuteRun_CompositionState(t *testing.T) {
	ctx := context.Background()

	// --- 1. Build the composition: one tool step ("echo-step") that calls the
	//        echo-e2e tool with a fixed arg.
	comp := &composition.Composition{
		Version: 1,
		Steps: []*composition.Step{
			{
				ID:   "echo-step",
				Kind: composition.KindTool,
				Tool: "echo-e2e",
				Args: map[string]any{"greeting": "hello from composition state"},
			},
		},
	}

	// --- 2. Build the workflow spec: entry state has orchestration: composition.
	wfSpec := &workflow.Spec{
		Version: 1,
		Entry:   "compose",
		States: map[string]*workflow.State{
			"compose": {
				PromptTask:    "compose-task",
				Orchestration: workflow.OrchestrationComposition,
				Composition:   "my-comp",
			},
		},
	}

	// --- 3. Build the tool registry and register the echo executor + descriptor.
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name:        "echo-e2e",
		Description: "echoes args",
		Mode:        "echo-e2e",
	}))
	reg.RegisterExecutor(&echoE2EExecutor{})

	// --- 4. Build a LoadedPack carrying the composition.
	pack := &prompt.Pack{
		ID:       "composition-test-pack",
		Workflow: wfSpec,
		Compositions: map[string]*composition.Composition{
			"my-comp": comp,
		},
	}

	// --- 5. Wire the Arena engine manually.
	//        We use a mock provider for registration (it is never called because
	//        CompositionStage replaces ProviderStage entirely).
	mockProv := mock.NewProvider("mock-comp", "test-model", false)
	provReg := providers.NewRegistry()
	provReg.Register(mockProv)

	arenaStore := statestore.NewArenaStateStore()

	pipelineExec := turnexecutors.NewPipelineExecutor(reg, nil)
	scriptedExec := turnexecutors.NewScriptedExecutor(pipelineExec)
	convExec := NewDefaultConversationExecutor(scriptedExec, nil, nil, nil, nil)

	transExec := newWorkflowTransitionExecutor(wfSpec, reg)

	e := &Engine{
		config: &arenaconfig.Config{
			LoadedPack: pack,
		},
		scenarios: map[string]*arenaconfig.Scenario{
			"comp-scenario": {
				ID:       "comp-scenario",
				TaskType: "compose-task",
				Turns: []arenaconfig.TurnDefinition{
					{Role: "user", Content: `{"msg":"run it"}`},
				},
			},
		},
		evals:                make(map[string]*arenaconfig.Eval),
		providers:            make(map[string]*config.Provider),
		providerRegistry:     provReg,
		stateStore:           arenaStore,
		conversationExecutor: convExec,
		toolRegistry:         reg,
		workflowSpec:         wfSpec,
		workflowTransExec:    transExec,
	}

	// --- 6. Execute the run.
	combo := RunCombination{
		ScenarioID: "comp-scenario",
		ProviderID: "mock-comp",
		Region:     "default",
	}
	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err)
	require.NotEmpty(t, runID)

	// --- 7. Load the saved conversation and assert the composition ran.
	result, err := arenaStore.GetResult(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, result, "run result must be stored")

	// The run must not have failed — a provider-invocation failure would indicate
	// the composition path was NOT taken.
	assert.Empty(t, result.Error, "composition run must not error")

	// Find the assistant message — it carries the composition's output.
	var assistantContent string
	for _, msg := range result.Messages {
		if msg.Role == "assistant" {
			assistantContent = msg.Content
			break
		}
	}
	require.NotEmpty(t, assistantContent, "expected an assistant message from the composition")

	// The echo step returns its args verbatim as JSON. The composition engine
	// wraps the last step's output as the result. Confirm "greeting" key is present.
	assert.Contains(t, assistantContent, "greeting",
		"composition output must contain the echo step's arg key")
	assert.Contains(t, assistantContent, "hello from composition state",
		"composition output must contain the echo step's arg value")
}
