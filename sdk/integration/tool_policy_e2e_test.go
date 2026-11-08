package integration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// weatherEveryTurnProvider scripts a get_weather call and a text answer for
// each of n user turns (the mock tool provider numbers a turn by the
// assistant and tool messages it has seen: 1, 3, 5 ... are the calls).
func weatherEveryTurnProvider(n int) *mock.ToolProvider {
	repo := newTestTurnRepository()
	for i := 0; i < n; i++ {
		repo.addTurn("default", 2*i+1, mock.Turn{
			Type:      "tool_calls",
			ToolCalls: []mock.ToolCall{{Name: "get_weather", Arguments: map[string]interface{}{"city": "Paris"}}},
		})
		repo.addTurn("default", 2*i+2, mock.Turn{Type: "text", Content: "answered"})
	}
	return mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
}

// toolResultTexts returns the content of every tool result in the history.
func toolResultTexts(msgs []types.Message) []string {
	var out []string
	for i := range msgs {
		if msgs[i].Role == "tool" {
			out = append(out, msgs[i].GetContent())
		}
	}
	return out
}

// TestToolPolicy_MaxCallsPerMinuteLimitsTheLoop sets a per-minute cap with
// WithToolPolicy and lets the model call the tool once per turn. The call over
// the cap does not run, the model is told why, and the failure is emitted.
func TestToolPolicy_MaxCallsPerMinuteLimitsTheLoop(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ec := newEventCollector(bus)

	packPath := writePackFile(t, toolPromptPackJSON)
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(weatherEveryTurnProvider(3)),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCallsPerMinute: 2}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	var runs atomic.Int32
	conv.OnTool("get_weather", func(map[string]any) (any, error) {
		runs.Add(1)
		return map[string]any{"temp": 20}, nil
	})

	ctx := context.Background()
	for _, msg := range []string{"one", "two", "three"} {
		resp, err := conv.Send(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "answered", resp.Text())
	}

	assert.Equal(t, int32(2), runs.Load(), "the third call is over the limit and must not run")
	results := toolResultTexts(conv.Messages(ctx))
	require.Len(t, results, 3, "every call gets a result")
	assert.Contains(t, results[2], "Tool get_weather not executed: its limit of 2 calls per minute is reached")
	assert.True(t, ec.waitForEvent(events.EventToolCallFailed, 2*time.Second), "tool.call.failed was not emitted")
}

// TestToolPolicy_MaxCallsPerMinuteCountsAtApproval holds every call for
// approval. A rejected call never counts, so with a limit of 1 the call
// approved after a rejection runs. The next approval is over the limit: its
// resolution carries a CallRateLimitError, the handler is not called, and
// the model is told.
func TestToolPolicy_MaxCallsPerMinuteCountsAtApproval(t *testing.T) {
	packPath := writePackFile(t, toolPromptPackJSON)
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(weatherEveryTurnProvider(3)),
		sdk.WithSkipSchemaValidation(),
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCallsPerMinute: 1}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	var runs atomic.Int32
	conv.OnToolAsync("get_weather",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "review", Message: "needs approval"}
		},
		func(map[string]any) (any, error) {
			runs.Add(1)
			return map[string]any{"temp": 20}, nil
		})

	ctx := context.Background()
	pendingID := func(msg string) string {
		t.Helper()
		resp, err := conv.Send(ctx, msg)
		require.NoError(t, err)
		require.Len(t, resp.PendingTools(), 1, "the call is held for approval")
		return resp.PendingTools()[0].ID
	}

	// Turn 1: rejected. It must not use the one slot.
	_, err = conv.RejectTool(ctx, pendingID("one"), "no")
	require.NoError(t, err)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)

	// Turn 2: approved, admitted, runs.
	res, err := conv.ResolveTool(ctx, pendingID("two"))
	require.NoError(t, err)
	require.NoError(t, res.Error, "a rejected call was counted against the limit")
	_, err = conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), runs.Load())

	// Turn 3: approved, but over the limit.
	res, err = conv.ResolveTool(ctx, pendingID("three"))
	require.NoError(t, err)
	var limitErr *stage.CallRateLimitError
	require.True(t, errors.As(res.Error, &limitErr), "the over-limit approval does not carry CallRateLimitError: %v", res.Error)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), runs.Load(), "the handler ran for a call over the limit")

	results := toolResultTexts(conv.Messages(ctx))
	require.Len(t, results, 3)
	assert.True(t, strings.Contains(results[2], "limit of 1 calls per minute is reached"),
		"the model was not told the call hit the limit: %q", results[2])
}

// TestToolPolicy_MaxRoundsMergesWithPrompt checks WithToolPolicy reaches the
// tool loop: with MaxRounds 1 the model may call tools for one round only.
func TestToolPolicy_MaxRoundsReachesTheLoop(t *testing.T) {
	repo := newTestTurnRepository()
	for i := 1; i <= 4; i++ {
		repo.addTurn("default", i, mock.Turn{
			Type:      "tool_calls",
			ToolCalls: []mock.ToolCall{{Name: "get_weather", Arguments: map[string]interface{}{"city": "Paris"}}},
		})
	}
	packPath := writePackFile(t, toolPromptPackJSON)
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		sdk.WithSkipSchemaValidation(),
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxRounds: 1}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	var runs atomic.Int32
	conv.OnTool("get_weather", func(map[string]any) (any, error) {
		runs.Add(1)
		return "sunny", nil
	})
	_, _ = conv.Send(context.Background(), "weather?")
	assert.Equal(t, int32(1), runs.Load(), "MaxRounds from WithToolPolicy did not bound the tool loop")
}
