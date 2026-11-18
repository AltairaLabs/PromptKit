package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Edge cases of an OnToolExecutor executor's async entry point (#2772).

// funcAsyncExecutor is an AsyncToolExecutor whose ExecuteAsync is run, and
// which records the call ID its context carries.
type funcAsyncExecutor struct {
	run     func(args json.RawMessage) (*tools.ToolExecutionResult, error)
	mu      sync.Mutex
	callIDs []string
}

func (*funcAsyncExecutor) Name() string { return "func-async-exec" }

func (*funcAsyncExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"from":"Execute"}`), nil
}

func (e *funcAsyncExecutor) ExecuteAsync(
	ctx context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	e.mu.Lock()
	e.callIDs = append(e.callIDs, tools.CallIDFromContext(ctx))
	e.mu.Unlock()
	return e.run(args)
}

func (e *funcAsyncExecutor) seenCallIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.callIDs...)
}

// imageResult is a completed result with a caption and an image.
func imageResult() *tools.ToolExecutionResult {
	return &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: json.RawMessage(`{"caption":"badge"}`),
		Parts:   []types.ContentPart{types.NewImagePartFromData(badgePNG, types.MIMETypeImagePNG, nil)},
	}
}

// holdResult is a pending result for a call held for review.
func holdResult() *tools.ToolExecutionResult {
	return &tools.ToolExecutionResult{
		Status:      tools.ToolStatusPending,
		PendingInfo: &tools.PendingToolInfo{Reason: "review", ToolName: "lookup"},
	}
}

// resultsByID returns the tool results in a provider request, by call ID.
func resultsByID(msgs []types.Message) map[string]*types.MessageToolResult {
	out := map[string]*types.MessageToolResult{}
	for i := range msgs {
		if msgs[i].ToolResult != nil {
			out[msgs[i].ToolResult.ID] = msgs[i].ToolResult
		}
	}
	return out
}

// Of two calls in one round, the one the executor holds is reported for the
// host, the other completes, and Resume gives the model both results: the
// host's answer and the completed call's image.
func TestOnToolExecutor_AsyncParallelCallsOneHeld(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c-hold", Name: "lookup", Args: []byte(`{"who":"hold"}`)},
		{ID: "c-run", Name: "lookup", Args: []byte(`{"who":"ada"}`)},
	}
	prov := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{ToolCalls: calls}, {Content: "done"}},
	}
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	exec := &funcAsyncExecutor{run: func(args json.RawMessage) (*tools.ToolExecutionResult, error) {
		if strings.Contains(string(args), "hold") {
			return holdResult(), nil
		}
		return imageResult(), nil
	}}
	conv.OnToolExecutor("lookup", exec)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "ada")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1, "only the held call is reported")
	assert.Equal(t, "c-hold", resp.ClientTools()[0].CallID)

	require.NoError(t, conv.SendToolResult(ctx, "c-hold", map[string]any{"approved": true}))
	final, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, "done", final.Text())

	prov.mu.Lock()
	results := resultsByID(prov.seen[len(prov.seen)-1])
	prov.mu.Unlock()
	require.Contains(t, results, "c-hold")
	require.Contains(t, results, "c-run")
	assert.JSONEq(t, `{"approved":true}`, results["c-hold"].GetTextContent())
	assert.Equal(t, []string{badgePNG}, imageData(results["c-run"].Parts))
	assert.ElementsMatch(t, []string{"c-hold", "c-run"}, exec.seenCallIDs(),
		"ExecuteAsync reads each call's ID from its context")
}

// A failure on the async entry point reaches the model as a failed tool
// result: an error, a failed status, and a nil result with no error.
func TestOnToolExecutor_AsyncFailuresReachTheModel(t *testing.T) {
	for name, tc := range map[string]struct {
		run  func(json.RawMessage) (*tools.ToolExecutionResult, error)
		want string
	}{
		"error": {
			run:  func(json.RawMessage) (*tools.ToolExecutionResult, error) { return nil, errors.New("boom") },
			want: "boom",
		},
		"failed status": {
			run: func(json.RawMessage) (*tools.ToolExecutionResult, error) {
				return &tools.ToolExecutionResult{Status: tools.ToolStatusFailed, Error: "quota exhausted"}, nil
			},
			want: "quota exhausted",
		},
		"no result": {
			run:  func(json.RawMessage) (*tools.ToolExecutionResult, error) { return nil, nil },
			want: "tool lookup: executor returned no result",
		},
	} {
		t.Run(name, func(t *testing.T) {
			conv, prov := openAsyncExecutorConv(t, &funcAsyncExecutor{run: tc.run})

			resp, err := conv.Send(context.Background(), "ada")
			require.NoError(t, err)
			assert.Equal(t, "done", resp.Text())
			assert.Empty(t, resp.ClientTools())
			result := toolResultInRequest(t, prov, 1)
			assert.Equal(t, tc.want, result.Error)
		})
	}
}

// A realtime conversation reports a held call on its response stream, and
// SendToolResult then ContinueDuplex gives the session the host's answer.
func TestOnToolExecutor_AsyncPendingOnARealtimeModel(t *testing.T) {
	sess := &partsSession{callerToolSession: &callerToolSession{MockStreamSession: mock.NewMockStreamSession()}}
	conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(&partsStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: sess}),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", &funcAsyncExecutor{run: func(json.RawMessage) (*tools.ToolExecutionResult, error) {
		return holdResult(), nil
	}})
	ch, err := conv.Response()
	require.NoError(t, err)
	held := make(chan []tools.PendingToolExecution, 1)
	go func() {
		for chunk := range ch {
			if len(chunk.PendingTools) > 0 {
				held <- chunk.PendingTools
			}
		}
	}()
	ctx := context.Background()

	require.NoError(t, conv.SendText(ctx, "ada"))
	var pending []tools.PendingToolExecution
	select {
	case pending = <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the held call was never reported")
	}
	require.Len(t, pending, 1)
	assert.Equal(t, callerCallID, pending[0].CallID)
	assert.Empty(t, sess.sent(), "a held call sends the session nothing")

	require.NoError(t, conv.SendToolResult(ctx, callerCallID, map[string]any{"approved": true}))
	require.NoError(t, conv.ContinueDuplex(ctx))
	require.Eventually(t, func() bool { return len(sess.sent()) > 0 }, 5*time.Second, 5*time.Millisecond)
	got := sess.sent()[0]
	assert.Equal(t, callerCallID, got.ToolCallID)
	assert.JSONEq(t, `{"approved":true}`, got.Result)
}
