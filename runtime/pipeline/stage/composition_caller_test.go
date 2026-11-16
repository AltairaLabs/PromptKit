package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callRecordingExecutor is a tools.CallExecutor that records the calls it runs.
type callRecordingExecutor struct {
	calls []tools.ToolCall
}

func (e *callRecordingExecutor) Name() string { return "call-recording" }

func (e *callRecordingExecutor) Execute(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{"via":"execute"}`), nil
}

func (e *callRecordingExecutor) ExecuteCall(
	_ context.Context, _ *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.calls = append(e.calls, call)
	return json.RawMessage(`{"via":"call"}`), nil, nil
}

var compositionCaller = types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}

// A composition tool step runs its tool for the conversation's caller.
func TestCompositionExecutor_ToolStepCarriesTheCaller(t *testing.T) {
	reg := tools.NewRegistry()
	exec := &callRecordingExecutor{}
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "lookup", Description: "lookup", Mode: exec.Name(), InputSchema: []byte(`{"type":"object"}`),
	}))

	run := NewCompositionStepExecutor(CompositionExecutorDeps{ToolRegistry: reg, Caller: compositionCaller})
	out, err := run(context.Background(),
		&composition.Step{ID: "t", Kind: composition.KindTool, Tool: "lookup"}, json.RawMessage(`{"x":1}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"via":"call"}`, string(out))
	require.Len(t, exec.calls, 1)
	assert.Equal(t, "lookup", exec.calls[0].Name)
	assert.Equal(t, compositionCaller, exec.calls[0].Caller)
	assert.JSONEq(t, `{"x":1}`, string(exec.calls[0].Args))
}

// A prompt or agent step's provider stage acts for the conversation's caller.
func TestCompositionExecutor_StepProviderConfigCarriesTheCaller(t *testing.T) {
	reg := prompt.NewRegistryWithRepository(newMockRepo())
	registerSimplePrompt(t, reg, "a")
	deps := CompositionExecutorDeps{PromptRegistry: reg, Caller: compositionCaller}

	cfg, err := deps.stepProviderConfig(
		&composition.Step{ID: "s", Kind: composition.KindAgent, PromptTask: "a"},
		mock.NewProvider("m", "m", false))
	require.NoError(t, err)
	assert.Equal(t, compositionCaller, cfg.Caller)
}
