package integration

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// recordingExecutor stands in for an embedder's governed tool path: the thing
// that must sit between the model and the tool regardless of which protocol
// the caller arrived on.
type recordingExecutor struct {
	calls atomic.Int64
	args  atomic.Value // json.RawMessage of the last call
}

func (e *recordingExecutor) Name() string { return "governed" }

func (e *recordingExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	e.calls.Add(1)
	e.args.Store(args)
	return json.RawMessage(`{"forecast":"sunny"}`), nil
}

// weatherToolProvider scripts a tool call followed by a text answer.
func weatherToolProvider(t *testing.T) *mock.ToolProvider {
	t.Helper()
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Checking the weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "Paris"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "It is sunny in Paris.",
	})
	return mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
}

// TestWithToolExecutor_ReachesA2AOpenedConversation is the point of #2019:
// A2AOpener opens the conversation internally and hands back an adapter, so
// there is no *Conversation for the caller to call OnToolExecutor on. Without
// the option, an embedder's tool governance simply cannot reach agents served
// over A2A.
func TestWithToolExecutor_ReachesA2AOpenedConversation(t *testing.T) {
	executor := &recordingExecutor{}
	packPath := writePackFile(t, toolsPackWithAllowedToolsJSON)

	opener := sdk.A2AOpener(packPath, "chat",
		sdk.WithProvider(weatherToolProvider(t)),
		sdk.WithToolExecutor("get_weather", executor),
		sdk.WithSkipSchemaValidation(),
	)

	conv, err := opener("ctx-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	result, err := conv.Send(context.Background(), "What is the weather in Paris?")
	require.NoError(t, err)

	require.Equal(t, int64(1), executor.calls.Load(),
		"the executor given as an option never ran; the A2A tool path bypassed it")
	assert.JSONEq(t, `{"city":"Paris"}`, string(executor.args.Load().(json.RawMessage)))
	assert.Contains(t, result.Text(), "sunny")
}

// TestWithToolExecutor_ReachesOpenedConversation: the same option on the
// ordinary Open path, where OnToolExecutor would also have worked.
func TestWithToolExecutor_ReachesOpenedConversation(t *testing.T) {
	executor := &recordingExecutor{}
	packPath := writePackFile(t, toolsPackWithAllowedToolsJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(weatherToolProvider(t)),
		sdk.WithToolExecutor("get_weather", executor),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "What is the weather in Paris?")
	require.NoError(t, err)

	require.Equal(t, int64(1), executor.calls.Load())
	assert.Contains(t, resp.Text(), "sunny")
}

// TestWithToolExecutor_OnToolExecutorStillWins: a post-Open registration for
// the same tool replaces the one given as an option, so the existing API keeps
// its meaning for callers that own the conversation.
func TestWithToolExecutor_OnToolExecutorStillWins(t *testing.T) {
	fromOption := &recordingExecutor{}
	afterOpen := &recordingExecutor{}
	packPath := writePackFile(t, toolsPackWithAllowedToolsJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(weatherToolProvider(t)),
		sdk.WithToolExecutor("get_weather", fromOption),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	conv.OnToolExecutor("get_weather", afterOpen)

	_, err = conv.Send(context.Background(), "What is the weather in Paris?")
	require.NoError(t, err)

	assert.Equal(t, int64(1), afterOpen.calls.Load())
	assert.Zero(t, fromOption.calls.Load())
}
