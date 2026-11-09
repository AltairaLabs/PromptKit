package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// weatherCallingProvider asks for get_weather on its first call and answers
// in text after that.
type weatherCallingProvider struct {
	*mock.Provider
	mu    sync.Mutex
	calls int
}

func (p *weatherCallingProvider) SupportsStreaming() bool { return false }

func (p *weatherCallingProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == 1 {
		return providers.PredictionResponse{ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
		}}, nil
	}
	return providers.PredictionResponse{Content: "it is sunny"}, nil
}

// countingExecutor counts the tool executions it is asked for.
type countingExecutor struct{ runs atomic.Int32 }

func (e *countingExecutor) Name() string { return "counting-exec" }

func (e *countingExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	e.runs.Add(1)
	return json.RawMessage(`"sunny"`), nil
}

// A tool the conversation's ApprovalChecker holds for human approval in a
// normal turn must not run unapproved when an agent step of a composition
// calls it (#2603). A composition step cannot suspend for approval, so the
// step fails closed, naming the tool, and the tool never executes.
func TestComposition_AgentStepHonorsTheApprovalChecker(t *testing.T) {
	exec := &countingExecutor{}
	reg := tools.NewRegistry()
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "get_weather", Description: "weather", Mode: exec.Name(), InputSchema: []byte(`{"type":"object"}`),
	}))
	var held atomic.Int32
	cfg := &Config{
		Provider:       &weatherCallingProvider{Provider: mock.NewProvider("p", "m", false)},
		ToolRegistry:   reg,
		PromptRegistry: createTestRegistry("chat"),
		ApprovalChecker: func(_ context.Context, _, name string, _ map[string]any) *tools.PendingToolInfo {
			held.Add(1)
			return &tools.PendingToolInfo{Reason: "requires_approval", Message: "approve " + name}
		},
		ActiveComposition: &composition.Composition{
			Version: 1, Output: "s",
			Steps: []*composition.Step{{
				ID: "s", Kind: composition.KindAgent, PromptTask: "chat",
				Tools:       []string{"get_weather"},
				Termination: &composition.Termination{MaxSteps: packspec.Ptr(3)},
			}},
		},
	}
	stages, err := buildProviderStages(cfg, stage.NewTurnState())
	require.NoError(t, err)

	input := make(chan stage.StreamElement, 1)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: "weather in Paris?"})
	close(input)
	output := make(chan stage.StreamElement, 32)
	procErr := stages[0].Process(context.Background(), input, output)

	var errs []error
	if procErr != nil {
		errs = append(errs, procErr)
	}
	for e := range output {
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
	}

	assert.Zero(t, exec.runs.Load(), "a tool held for approval must not run inside a composition step")
	assert.Equal(t, int32(1), held.Load(), "the conversation's ApprovalChecker must be asked")
	require.NotEmpty(t, errs, "the step must fail, not answer without the tool")
	assert.ErrorIs(t, errs[0], stage.ErrCompositionToolNeedsApproval)
	assert.Contains(t, errs[0].Error(), "get_weather")
}
