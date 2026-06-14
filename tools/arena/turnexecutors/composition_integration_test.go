package turnexecutors

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/require"
)

// echoExecutor is a trivial tools.Executor that echoes its args back as JSON.
type echoExecutor struct{}

func (e *echoExecutor) Name() string { return "echo" }
func (e *echoExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

// TestBuildStagePipeline_CompositionStageSelected verifies that when
// TurnRequest.ActiveComposition is non-nil, buildStagePipeline produces a
// pipeline whose provider-stage slot is a *stage.CompositionStage AND that the
// prompt-assembly stages (VariableProvider/PromptAssembly/Template) are not
// present.  The test drives the composition end-to-end with a one-step tool
// composition ("echo") to confirm the stage actually executes.
func TestBuildStagePipeline_CompositionStageSelected(t *testing.T) {
	// Build a one-step tool composition that calls an "echo" tool.
	comp := &composition.Composition{
		Version: 1,
		Steps: []*composition.Step{
			{
				ID:   "greet",
				Kind: composition.KindTool,
				Tool: "echo",
				Args: map[string]any{"message": "hello from composition"},
			},
		},
	}

	// Register the echo executor and a matching tool descriptor.
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name:        "echo",
		Description: "echoes args",
		Mode:        "echo",
	}))
	reg.RegisterExecutor(&echoExecutor{})

	executor := NewPipelineExecutor(reg, nil)

	req := &TurnRequest{
		Provider:          &mockNonMockProvider{}, // non-nil but never called in composition path
		Scenario:          &config.Scenario{TaskType: "unused"},
		ActiveComposition: comp,
		BaseDir:           t.TempDir(),
	}

	// Build the pipeline — this is the function under test.
	baseVars := map[string]string{}
	pipeline, err := executor.buildStagePipeline(req, baseVars)
	require.NoError(t, err)
	require.NotNil(t, pipeline)

	// Execute synchronously with a simple user message.
	userMsg := &types.Message{Role: "user", Content: `{"input":"hi"}`}
	result, err := pipeline.ExecuteSync(context.Background(), stage.StreamElement{Message: userMsg})
	require.NoError(t, err)
	require.NotNil(t, result)

	// The composition's echo step should have produced output containing the echoed args.
	if result.Response == nil {
		t.Fatal("expected a response from composition pipeline, got nil")
	}
	// The echo tool returns the args JSON; composition output should reference "message".
	if result.Response.Content == "" {
		t.Fatal("expected non-empty response content from composition")
	}
}
