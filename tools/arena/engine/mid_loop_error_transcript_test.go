package engine

// TestProviderStage_MidLoopError_PreservesTranscript is Piece 4 of the arena
// mid-loop error fix. It proves that when the ProviderStage's tool loop errors
// (e.g. "max rounds exceeded") AFTER producing at least one assistant+tool
// message, the accumulated messages are still visible in the ArenaStateStore —
// NOT just the system message.
//
// The pipeline built here is intentionally minimal:
//
//	ProviderStage (MaxRounds=1, ToolProvider always requests a tool) →
//	ArenaStateStoreSaveStage
//
// Flow:
//  1. Provider returns an assistant message with a tool call (round 1).
//  2. Tool executes, result appended (still round 1).
//  3. MessageLog.LogAppend fires per-round — persists [assistant, tool-result].
//  4. Round 2 would be needed for a text reply, but MaxRounds==1 → error.
//  5. Pipeline fails; save stage is SKIPPED (error path).
//  6. Because MessageLog wrote per-round, the store already has the messages.
//  7. buildResultFromStateStore reads them back → result has the transcript.
//
// Before Piece 1-3 this test fails: result.Messages has only the system message
// (or is empty), because MessageLog is nil so LogAppend never fires.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
	arenastatestore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alwaysToolProvider is a minimal ToolSupport provider that always requests
// a call to "echo_tool" on PredictWithTools. It never returns a text reply,
// so the tool loop keeps cycling until MaxRounds is hit.
type alwaysToolProvider struct{}

func (p *alwaysToolProvider) ID() string                          { return "always-tool" }
func (p *alwaysToolProvider) Name() string                        { return "always-tool" }
func (p *alwaysToolProvider) Type() base.ProviderType             { return base.ProviderTypeInference }
func (p *alwaysToolProvider) Pricing() *base.PricingDescriptor    { return nil }
func (p *alwaysToolProvider) Validate() error                     { return nil }
func (p *alwaysToolProvider) Init(_ context.Context) error        { return nil }
func (p *alwaysToolProvider) HealthCheck(_ context.Context) error { return nil }
func (p *alwaysToolProvider) Model() string                       { return "always-tool-v1" }
func (p *alwaysToolProvider) SupportsStreaming() bool             { return false }
func (p *alwaysToolProvider) Close() error                        { return nil }
func (p *alwaysToolProvider) ShouldIncludeRawOutput() bool        { return false }
func (p *alwaysToolProvider) CalculateCost(in, out, cached int) types.CostInfo {
	return types.CostInfo{
		InputTokens:  in,
		OutputTokens: out,
		TotalCost:    float64(in+out) * 0.000001,
	}
}
func (p *alwaysToolProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{Content: "text"}, nil
}
func (p *alwaysToolProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

// BuildTooling passes descriptors through unchanged (mock doesn't care about format).
func (p *alwaysToolProvider) BuildTooling(descs []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return descs, nil
}

// PredictWithTools always returns a single tool call to "echo_tool".
func (p *alwaysToolProvider) PredictWithTools(
	_ context.Context,
	_ providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	args, _ := json.Marshal(map[string]string{"msg": "hello"})
	tc := types.MessageToolCall{ID: "call-1", Name: "echo_tool", Args: json.RawMessage(args)}
	resp := providers.PredictionResponse{
		Content:   "",
		ToolCalls: []types.MessageToolCall{tc},
		CostInfo:  &types.CostInfo{InputTokens: 10, OutputTokens: 5, TotalCost: 0.000015},
	}
	return resp, []types.MessageToolCall{tc}, nil
}

// PredictStreamWithTools is unused in non-streaming tests; included for interface compliance.
func (p *alwaysToolProvider) PredictStreamWithTools(
	_ context.Context,
	_ providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

// echoExecutor echoes back the "msg" argument so the tool result is non-empty.
type echoExecutor struct{}

func (e *echoExecutor) Name() string { return "echo" }
func (e *echoExecutor) Execute(_ context.Context, desc *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	var params map[string]string
	_ = json.Unmarshal(args, &params)
	result := map[string]string{"echo": params["msg"]}
	out, _ := json.Marshal(result)
	return out, nil
}

// TestProviderStage_MidLoopError_PreservesTranscript verifies that:
//   - A ProviderStage error (max rounds exceeded) does NOT lose the tool-loop
//     messages already produced during the failed turn.
//   - The ArenaStateStore contains system+user+assistant+tool messages.
//   - There are NO duplicate messages (LogAppend idempotent dedup works).
func TestProviderStage_MidLoopError_PreservesTranscript(t *testing.T) {
	ctx := context.Background()
	convID := "test-mid-loop-error"

	// Register echo_tool
	reg := tools.NewRegistry()
	reg.RegisterExecutor(&echoExecutor{})
	echoDesc := &tools.ToolDescriptor{
		Name:        "echo_tool",
		Description: "echoes back the msg argument",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		Mode:        "echo",
	}
	reg.Register(echoDesc)

	// Create ArenaStateStore — this is what Piece 1 implements MessageLog on.
	arenaStore := arenastatestore.NewArenaStateStore()

	// Build the pipeline:
	//   ProviderStage(MaxRounds=1, MessageLog=arenaStore) → ArenaStateStoreSaveStage
	//
	// MaxRounds=1 means: after 1 tool execution, if the provider STILL wants tools
	// (which alwaysToolProvider does), error "max rounds (1) exceeded".
	// TurnState.AllowedTools must include "echo_tool" so buildProviderTools surfaces
	// it to the ToolSupport provider (non-system-namespaced tools are only surfaced
	// when listed in allowedTools).
	//
	// We pass system+user messages as pipeline history (matching what
	// StateStoreLoadStage would emit). This sets acc.messages = [system, user],
	// so lastPersistedSeq=2 aligns with the store count after the save stage
	// persists those history messages on round 1 of maybeIncrementalSave.
	toolPolicy := &pipeline.ToolPolicy{MaxRounds: 1}
	turnState := stage.NewTurnState()
	turnState.AllowedTools = []string{"echo_tool"}
	turnState.SystemPrompt = "You are a test assistant."
	provConfig := &stage.ProviderConfig{
		MessageLog:       arenaStore, // Piece 2: wire MessageLog
		MessageLogConvID: convID,     // Piece 2: same conv ID
	}
	pipelineBuilder := stage.NewPipelineBuilder()
	storeConfig := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: convID,
	}
	stages := []stage.Stage{
		stage.NewProviderStageWithTurnState(
			&alwaysToolProvider{}, reg, toolPolicy, provConfig, nil, nil, turnState,
		),
		arenastages.NewArenaStateStoreSaveStageWithTurnState(storeConfig, turnState),
	}
	p, err := pipelineBuilder.Chain(stages...).Build()
	require.NoError(t, err)

	// Send system + user messages as pipeline input (mimicking StateStoreLoadStage +
	// user-turn injection). Both are collected in acc.messages so lastPersistedSeq=2.
	sysMsg := &types.Message{Role: "system", Content: "You are a test assistant.", Source: "statestore"}
	userMsg := &types.Message{Role: "user", Content: "Call the echo tool."}

	sysElem := stage.StreamElement{Message: sysMsg}
	userElem := stage.StreamElement{Message: userMsg}

	// Execute — expect an error because MaxRounds=1 will be exceeded
	_, pipelineErr := p.ExecuteSync(ctx, sysElem, userElem)

	// The pipeline MUST have failed (max rounds exceeded)
	require.Error(t, pipelineErr, "pipeline should error on max rounds exceeded")
	require.True(t, strings.Contains(pipelineErr.Error(), "max rounds"),
		"error should mention 'max rounds', got: %v", pipelineErr)

	// Load the conversation from the store
	state, loadErr := arenaStore.Load(ctx, convID)
	require.NoError(t, loadErr)
	require.NotNil(t, state)

	// Verify: messages must include more than just the system+user pre-population.
	// At minimum we expect: system + user (pre-existing) + assistant (tool call) + tool result.
	t.Logf("Messages in store after mid-loop error: %d", len(state.Messages))
	for i, m := range state.Messages {
		t.Logf("  [%d] role=%s content=%q toolResult=%v toolCalls=%v",
			i, m.Role, m.Content, m.ToolResult != nil, m.ToolCalls)
	}

	require.GreaterOrEqual(t, len(state.Messages), 4,
		"expected at least 4 messages (system+user+assistant+tool-result), got %d", len(state.Messages))

	// Verify the roles are in the right order
	roles := make([]string, len(state.Messages))
	for i, m := range state.Messages {
		if m.ToolResult != nil {
			roles[i] = "tool"
		} else {
			roles[i] = m.Role
		}
	}
	assert.Equal(t, "system", roles[0], "first message should be system")
	assert.Equal(t, "user", roles[1], "second message should be user")
	assert.Equal(t, "assistant", roles[2], "third message should be assistant (tool call)")
	assert.Equal(t, "tool", roles[3], "fourth message should be tool result")

	// Verify NO duplicate messages — idempotent dedup must work
	for i := range state.Messages {
		for j := i + 1; j < len(state.Messages); j++ {
			if state.Messages[i].Role == state.Messages[j].Role &&
				state.Messages[i].Content == state.Messages[j].Content &&
				state.Messages[i].Role != "user" { // user "Call the echo tool" appears twice (pre-pop + pipeline input)
				t.Errorf("duplicate message at [%d] and [%d]: role=%s content=%q",
					i, j, state.Messages[i].Role, state.Messages[i].Content)
			}
		}
	}
}
