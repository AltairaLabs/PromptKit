package sdk

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An executor registered with OnToolExecutor that implements
// tools.AsyncToolExecutor or tools.AsyncCallExecutor is run through that entry
// point, as the tool registry runs it: its content parts reach the model and
// a pending status suspends the call (#2772).

// asyncMediaExecutor is an AsyncToolExecutor whose ExecuteAsync returns a
// caption and an image, or holds the call when pending is set. Execute, which
// the async paths must not use for it, returns text only and counts its runs.
type asyncMediaExecutor struct {
	pending  bool
	syncRuns atomic.Int32
}

func (*asyncMediaExecutor) Name() string { return "async-media-exec" }

func (e *asyncMediaExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	e.syncRuns.Add(1)
	return json.RawMessage(`{"caption":"text only"}`), nil
}

func (e *asyncMediaExecutor) ExecuteAsync(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	if e.pending {
		return &tools.ToolExecutionResult{
			Status:      tools.ToolStatusPending,
			PendingInfo: &tools.PendingToolInfo{Reason: "review", ToolName: d.Name},
		}, nil
	}
	return &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: json.RawMessage(`{"caption":"badge"}`),
		Parts:   []types.ContentPart{types.NewImagePartFromData(badgePNG, types.MIMETypeImagePNG, nil)},
	}, nil
}

// asyncCallMediaExecutor is an AsyncCallExecutor: ExecuteCallAsync returns an
// image and records the call it was given.
type asyncCallMediaExecutor struct {
	asyncMediaExecutor
	gotCallID atomic.Value
}

func (e *asyncCallMediaExecutor) ExecuteCallAsync(
	ctx context.Context, d *tools.ToolDescriptor, call tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	e.gotCallID.Store(call.ID)
	return e.ExecuteAsync(ctx, d, call.Args)
}

func openAsyncExecutorConv(t *testing.T, exec tools.Executor) (*Conversation, *requestRecordingProvider) {
	t.Helper()
	prov := &requestRecordingProvider{callerToolProvider: newCallerToolProvider()}
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", exec)
	return conv, prov
}

// toolResultInRequest is the tool result at the end of the provider's call-th
// request.
func toolResultInRequest(t *testing.T, prov *requestRecordingProvider, call int) *types.MessageToolResult {
	t.Helper()
	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.Greater(t, len(prov.reqs), call)
	msgs := prov.reqs[call]
	last := msgs[len(msgs)-1]
	require.Equal(t, "tool", last.Role)
	require.NotNil(t, last.ToolResult)
	return last.ToolResult
}

func TestOnToolExecutor_AsyncExecutorPartsReachTheModel(t *testing.T) {
	exec := &asyncMediaExecutor{}
	conv, prov := openAsyncExecutorConv(t, exec)

	resp, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, "done", resp.Text())

	result := toolResultInRequest(t, prov, 1)
	assert.Equal(t, []string{badgePNG}, imageData(result.Parts), "the image goes to the model")
	assert.NotContains(t, result.GetTextContent(), "text only", "the result is ExecuteAsync's, not Execute's")
	assert.EqualValues(t, 0, exec.syncRuns.Load(), "Execute does not run")
}

func TestOnToolExecutor_AsyncCallExecutorGetsTheCallAndItsPartsReachTheModel(t *testing.T) {
	exec := &asyncCallMediaExecutor{}
	conv, prov := openAsyncExecutorConv(t, exec)

	_, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)

	assert.Equal(t, callerCallID, exec.gotCallID.Load(), "ExecuteCallAsync is given the call")
	result := toolResultInRequest(t, prov, 1)
	assert.Equal(t, []string{badgePNG}, imageData(result.Parts))
}

// A call the executor holds suspends the turn as any held call does: it is
// reported for the host to answer, and SendToolResult then Resume gives the
// model the host's result.
func TestOnToolExecutor_AsyncExecutorPendingSuspendsAndResumes(t *testing.T) {
	exec := &asyncMediaExecutor{pending: true}
	conv, prov := openAsyncExecutorConv(t, exec)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "ada")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1, "the held call is reported")
	assert.Equal(t, callerCallID, resp.ClientTools()[0].CallID)
	assert.Equal(t, "lookup", resp.ClientTools()[0].ToolName)
	assert.EqualValues(t, 0, exec.syncRuns.Load(), "a held call does not run")

	require.NoError(t, conv.SendToolResult(ctx, callerCallID, map[string]any{"approved": true}))
	final, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, "done", final.Text())
	assert.JSONEq(t, `{"approved":true}`, toolResultInRequest(t, prov, 1).GetTextContent())
}

// A later registration for the name replaces the executor's async entry point
// as it replaces its other ones.
func TestOnToolExecutor_LaterOnToolCtxReplacesTheAsyncEntryPoint(t *testing.T) {
	exec := &asyncMediaExecutor{pending: true}
	conv, prov := openAsyncExecutorConv(t, exec)
	conv.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"from": "ctx"}, nil
	})

	resp, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Empty(t, resp.ClientTools())
	assert.JSONEq(t, `{"from":"ctx"}`, toolResultInRequest(t, prov, 1).GetTextContent())
}

func TestOnToolExecutor_LaterOnToolCallReplacesTheAsyncEntryPoint(t *testing.T) {
	exec := &asyncMediaExecutor{pending: true}
	conv, prov := openAsyncExecutorConv(t, exec)
	conv.OnToolCall("lookup", func(context.Context, ToolCall) (any, error) {
		return map[string]any{"from": "call"}, nil
	})

	resp, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Empty(t, resp.ClientTools())
	assert.JSONEq(t, `{"from":"call"}`, toolResultInRequest(t, prov, 1).GetTextContent())
}

// A later synchronous executor for the name replaces the earlier executor's
// async entry point.
func TestOnToolExecutor_LaterSyncExecutorReplacesTheAsyncEntryPoint(t *testing.T) {
	conv, prov := openAsyncExecutorConv(t, &asyncMediaExecutor{pending: true})
	conv.OnToolExecutor("lookup", mediaExecutor{})

	resp, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Empty(t, resp.ClientTools(), "the replaced executor holds nothing")
	assert.Equal(t, []string{badgePNG}, imageData(toolResultInRequest(t, prov, 1).Parts),
		"the later executor's ExecuteMultimodal runs")
}

// A fork keeps the executor's async entry point.
func TestOnToolExecutor_ForkKeepsTheAsyncEntryPoint(t *testing.T) {
	conv, prov := openAsyncExecutorConv(t, &asyncMediaExecutor{})
	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })

	_, err = fork.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, []string{badgePNG}, imageData(toolResultInRequest(t, prov, 1).Parts))
}

// A realtime conversation runs the executor's async entry point too, and its
// parts reach the realtime model.
func TestOnToolExecutor_AsyncExecutorPartsReachARealtimeModel(t *testing.T) {
	sess := &partsSession{callerToolSession: &callerToolSession{MockStreamSession: mock.NewMockStreamSession()}}
	conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(&partsStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: sess}),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", &asyncMediaExecutor{})
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "ada"))
	require.Eventually(t, func() bool { return len(sess.sent()) > 0 }, 5*time.Second, 5*time.Millisecond)

	got := sess.sent()[0]
	assert.Equal(t, callerCallID, got.ToolCallID)
	assert.Equal(t, []string{badgePNG}, imageData(got.Parts), "the image goes to the realtime model")
}
