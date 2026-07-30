package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// toolRouteProvider wraps a tool-capable mock provider and records which
// provider entry point the stage routed to. Only the tool-aware entry points
// serialize tool_calls / tool_call_id, so "which method was called" is the
// observable that decides whether history survives the round trip (#1735).
type toolRouteProvider struct {
	*mock.ToolProvider
	toolPath    bool
	nonToolPath bool
}

func newToolRouteProvider() *toolRouteProvider {
	return &toolRouteProvider{ToolProvider: mock.NewToolProvider("test", "model", false, nil)}
}

func (p *toolRouteProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.nonToolPath = true
	return p.ToolProvider.Predict(ctx, req)
}

func (p *toolRouteProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.nonToolPath = true
	return p.ToolProvider.PredictStream(ctx, req)
}

func (p *toolRouteProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest,
	tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.toolPath = true
	return p.ToolProvider.PredictWithTools(ctx, req, tools, toolChoice)
}

func (p *toolRouteProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest,
	tools providers.ProviderTools, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	p.toolPath = true
	return p.ToolProvider.PredictStreamWithTools(ctx, req, tools, toolChoice)
}

// historyWithToolLinkage returns a turn whose assistant message carries tool
// calls and whose follow-up carries the matching tool result — the shape that
// only the tool-aware serializers can represent.
func historyWithToolLinkage() []types.Message {
	denial := types.NewTextToolResult("call_A", "approve", "denied by policy")
	denial.Error = "denied by policy"
	return []types.Message{
		{Role: "user", Content: "approve the refund"},
		{Role: roleAssistant, ToolCalls: []types.MessageToolCall{
			{ID: "call_A", Name: "approve", Args: json.RawMessage(`{}`)},
		}},
		types.NewToolResultMessage(denial),
	}
}

func plainHistory() []types.Message {
	return []types.Message{{Role: "user", Content: "hello"}}
}

// Once every tool is excluded, buildProviderTools yields nil. Routing on that
// nil sent the turn to the tool-blind serializer, which drops tool_calls and
// tool_call_id and produces the array OpenAI rejects with
// "messages with role 'tool' must be a response to a preceding message with 'tool_calls'".
func TestStartStreamingRequest_ToolHistoryKeepsToolPathWhenToolsEmpty(t *testing.T) {
	provider := newToolRouteProvider()
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	_, err := stage.startStreamingRequest(
		context.Background(),
		providers.PredictionRequest{Messages: historyWithToolLinkage()},
		nil, // every tool excluded this round
		"",
	)
	require.NoError(t, err)

	assert.True(t, provider.toolPath,
		"history carries tool_calls, so the turn must stay on PredictStreamWithTools")
	assert.False(t, provider.nonToolPath,
		"PredictStream drops tool_calls/tool_call_id and produces a 400")
}

func TestExecuteRound_ToolHistoryKeepsToolPathWhenToolsEmpty(t *testing.T) {
	provider := newToolRouteProvider()
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	_, _, err := stage.executeRound(
		context.Background(), historyWithToolLinkage(), "", nil, "", 1, nil,
	)
	require.NoError(t, err)

	assert.True(t, provider.toolPath,
		"the non-streaming path has the same defect and needs the same fix")
	assert.False(t, provider.nonToolPath)
}

// Guards the fix against being too broad: a turn with no tool linkage has
// nothing for the tool-aware serializer to preserve, so it must keep using the
// plain path exactly as before.
func TestStartStreamingRequest_PlainHistoryStillUsesNonToolPath(t *testing.T) {
	provider := newToolRouteProvider()
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	_, err := stage.startStreamingRequest(
		context.Background(),
		providers.PredictionRequest{Messages: plainHistory()},
		nil,
		"",
	)
	require.NoError(t, err)

	assert.True(t, provider.nonToolPath,
		"no tool linkage in history — behavior must be unchanged")
	assert.False(t, provider.toolPath)
}

// A provider that cannot do tools must fall back rather than newly erroring.
func TestStartStreamingRequest_ToolHistoryOnNonToolProviderFallsBack(t *testing.T) {
	stage := NewProviderStage(mock.NewProvider("test", "model", false), nil, nil, &ProviderConfig{})

	_, err := stage.startStreamingRequest(
		context.Background(),
		providers.PredictionRequest{Messages: historyWithToolLinkage()},
		nil,
		"",
	)
	require.NoError(t, err,
		"a provider without ToolSupport must degrade to PredictStream, not error")
}

// Preserved behavior: real tools plus a provider that cannot serve them is
// still an error, not a silent downgrade.
func TestStartStreamingRequest_ToolsOnNonToolProviderStillErrors(t *testing.T) {
	stage := NewProviderStage(mock.NewProvider("test", "model", false), nil, nil, &ProviderConfig{})

	_, err := stage.startStreamingRequest(
		context.Background(),
		providers.PredictionRequest{Messages: plainHistory()},
		[]*providers.ToolDescriptor{{Name: "approve"}},
		toolChoiceAuto,
	)
	require.Error(t, err)
}
