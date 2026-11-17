package sdk

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// callRecordingSession is a duplex session that records the calls the model
// made and counts its completed turns, as the SDK's duplex session does.
type callRecordingSession struct {
	fakeDuplexSession
	calls map[string]types.MessageToolCall
	turns int64
}

func (s *callRecordingSession) ToolCall(id string) (types.MessageToolCall, bool) {
	call, ok := s.calls[id]
	return call, ok
}

func (s *callRecordingSession) TurnsCompleted() int64 { return s.turns }

// duplexRewriteConv is a duplex conversation on sess whose history holds no
// tool-call message yet, with rw rewriting results.
func duplexRewriteConv(sess *callRecordingSession, rw *wrappingRewriter) *Conversation {
	conv := newTestConversation()
	conv.mode = DuplexMode
	conv.resolvedStore = sdktools.NewResolvedStore()
	conv.duplexSession = sess
	conv.hookRegistry = hooks.NewRegistry(hooks.WithToolHook(rw))
	return conv
}

// A client answers before the tool-call message reaches the history: the
// call is taken from the session's record of the calls the model made.
func TestContinueDuplex_CallFromTheSessionsRecord(t *testing.T) {
	sess := &callRecordingSession{calls: map[string]types.MessageToolCall{
		"call_1": {ID: "call_1", Name: "lookup_order", Args: []byte(`{"order_id":"ORD-5"}`)},
	}}
	rw := &wrappingRewriter{}
	conv := duplexRewriteConv(sess, rw)
	conv.resolvedStore.Add(&sdktools.ToolResolution{ID: "call_1", ResultJSON: []byte(`"shipped"`)})

	require.NoError(t, conv.ContinueDuplex(context.Background()))
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "call_1", Result: "<redacted:shipped>"}}}, sess.submitted)
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "lookup_order", reqs[0].Name)
	assert.Equal(t, `{"order_id":"ORD-5"}`, string(reqs[0].Args))
}

// Gemini numbers calls call_0, call_1, ... per response, so an id comes back
// in a later turn for another call. The session's record of the current call
// is used, not an approval recorded for the earlier one.
func TestContinueDuplex_ReusedCallIDIsTheCurrentCall(t *testing.T) {
	sess := &callRecordingSession{calls: map[string]types.MessageToolCall{
		"call_0": {ID: "call_0", Name: "get_location", Args: []byte(`{"accuracy":"high"}`)},
	}}
	rw := &wrappingRewriter{}
	conv := duplexRewriteConv(sess, rw)
	conv.recordAdmittedRaw("call_0", "lookup_order", nil, map[string]any{"order_id": "ORD-1"})
	sess.turns = 1 // the turn that held lookup_order's call_0 has ended
	conv.resolvedStore.Add(&sdktools.ToolResolution{ID: "call_0", ResultJSON: []byte(`"Paris"`)})

	require.NoError(t, conv.ContinueDuplex(context.Background()))
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "get_location", reqs[0].Name)
	assert.Equal(t, `{"accuracy":"high"}`, string(reqs[0].Args))
}

// An approval recorded in a turn that has since ended, and never continued,
// does not name a call: with no other record of the call, the result fails
// closed.
func TestContinueDuplex_StaleApprovalDoesNotNameACall(t *testing.T) {
	sess := &callRecordingSession{calls: map[string]types.MessageToolCall{}}
	rw := &wrappingRewriter{}
	conv := duplexRewriteConv(sess, rw)
	conv.recordAdmittedRaw("call_9", "lookup_order", nil, map[string]any{"order_id": "ORD-1"})
	sess.turns = 1
	conv.resolvedStore.Add(&sdktools.ToolResolution{ID: "call_9", ResultJSON: []byte(`"secret"`)})

	require.NoError(t, conv.ContinueDuplex(context.Background()))
	assert.Equal(t, [][]providers.ToolResponse{{{
		ToolCallID: "call_9",
		Result:     "Error: the result of call call_9 was not delivered: no call it answers is known to rewrite it for",
		IsError:    true,
	}}}, sess.submitted)
	assert.Empty(t, rw.requests())
}

// An approval recorded in the current turn names the call when neither the
// session nor the history does.
func TestContinueDuplex_CurrentApprovalNamesTheCall(t *testing.T) {
	sess := &callRecordingSession{calls: map[string]types.MessageToolCall{}}
	rw := &wrappingRewriter{}
	conv := duplexRewriteConv(sess, rw)
	conv.recordAdmittedRaw("call_9", "lookup_order", nil, map[string]any{"order_id": "ORD-1"})
	conv.resolvedStore.Add(&sdktools.ToolResolution{ID: "call_9", ResultJSON: []byte(`"shipped"`)})

	require.NoError(t, conv.ContinueDuplex(context.Background()))
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "call_9", Result: "<redacted:shipped>"}}}, sess.submitted)
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "lookup_order", reqs[0].Name)
	assert.JSONEq(t, `{"order_id":"ORD-1"}`, string(reqs[0].Args))
}

// A failed client tool's partial output is rewritten before it reaches the
// model and the history.
func TestResume_RewritesAFailedClientToolsPartialOutput(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "c-cli", Name: "locate", Args: []byte(`{}`)}}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(&wrappingRewriter{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	ctx := context.Background()
	_, err = conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	require.NoError(t, conv.FailClientTool(ctx, "c-cli", "near Paris", errDiskFull))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	const want = "<redacted:\"near Paris\">\n\nTool error: disk full"
	provider.mu.Lock()
	var seen string
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			seen = m.ToolResult.GetTextContent()
		}
	}
	provider.mu.Unlock()
	assert.Equal(t, want, seen)
	for _, m := range conv.Messages(ctx) {
		if m.ToolResult != nil {
			assert.Equal(t, want, m.ToolResult.GetTextContent())
			assert.Equal(t, "disk full", m.ToolResult.Error)
		}
	}
}

var errDiskFull = errors.New("disk full")

// A settled answer's name and rejection text come from the call it settles,
// here one the history no longer names but the conversation approved.
func TestResumeResult_NamesTheCallFromItsApproval(t *testing.T) {
	conv := newTestConversation()
	conv.toolRegistry = tools.NewRegistry()
	require.NoError(t, conv.toolRegistry.Register(&tools.ToolDescriptor{
		Name: "send_message", Description: "send", Mode: "local", InputSchema: []byte(`{"type":"object"}`),
		ClientConfig: &tools.ClientConfig{Consent: &tools.ConsentConfig{DeclineStrategy: tools.DeclineStrategyRetry}},
	}))
	conv.recordAdmittedRaw("c1", "send_message", nil, map[string]any{"body": "hi"})
	noHistory := func() map[string]types.MessageToolCall { return map[string]types.MessageToolCall{} }

	got := conv.resumeResult(context.Background(), noHistory,
		&sdktools.ToolResolution{ID: "c1", Rejected: true, RejectionReason: "not now"})
	want := types.NewTextToolResult("c1", "send_message",
		"Tool send_message denied: user declined consent. Try a different approach. Reason: not now")
	assert.Equal(t, want, got)
}

// openLocateConv opens a conversation suspended on client calls to locate,
// with rw rewriting results.
func openLocateConv(
	t *testing.T, rw hooks.ToolHook, calls []types.MessageToolCall, opts ...Option,
) (*Conversation, *roundsProvider) {
	t.Helper()
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	opts = append([]Option{WithProvider(provider), WithSkipSchemaValidation(), WithToolHook(rw)}, opts...)
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"
	_, err = conv.Send(context.Background(), "where am I?")
	require.NoError(t, err)
	return conv, provider
}

func lastResults(p *roundsProvider) map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]string{}
	for _, m := range p.seen[len(p.seen)-1] {
		if m.ToolResult != nil {
			out[m.ToolResult.ID] = m.ToolResult.GetTextContent()
		}
	}
	return out
}

func rewrittenIDs(rw *wrappingRewriter) []string {
	var ids []string
	for _, req := range rw.requests() {
		ids = append(ids, req.CallID)
	}
	return ids
}

// The rewriter runs once for each result delivered, when it is delivered:
// not when the answer is given.
func TestResume_RewriterRunsOncePerDeliveredResult(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, provider := openLocateConv(t, rw, []types.MessageToolCall{{ID: "c1", Name: "locate", Args: []byte(`{}`)}})
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c1", "Paris"))
	assert.Empty(t, rw.requests(), "an answer is not rewritten when it is given")
	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"c1"}, rewrittenIDs(rw))
	assert.Equal(t, map[string]string{"c1": `<redacted:"Paris">`}, lastResults(provider))
}

// A second answer to the same call is dropped, and only the delivered one is
// rewritten.
func TestResume_DuplicateAnswerIsRewrittenOnce(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, provider := openLocateConv(t, rw, []types.MessageToolCall{{ID: "c1", Name: "locate", Args: []byte(`{}`)}})
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c1", "Paris"))
	require.NoError(t, conv.SendToolResult(ctx, "c1", "Lyon"))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"c1"}, rewrittenIDs(rw))
	assert.Equal(t, map[string]string{"c1": `<redacted:"Paris">`}, lastResults(provider))
}

// An answer the call-rate limit refuses is not rewritten: the model gets the
// limit error.
func TestResume_RateLimitedAnswerIsNotRewritten(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, provider := openLocateConv(t, rw, []types.MessageToolCall{
		{ID: "c1", Name: "locate", Args: []byte(`{"n":1}`)}, {ID: "c2", Name: "locate", Args: []byte(`{"n":2}`)},
	}, WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: 1, MaxParallelToolCalls: 1}))
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c1", "Paris"))
	require.NoError(t, conv.SendToolResult(ctx, "c2", "Lyon"))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"c1"}, rewrittenIDs(rw), "the refused answer never reaches the rewriter")
	results := lastResults(provider)
	assert.Equal(t, `<redacted:"Paris">`, results["c1"])
	assert.Equal(t, "Tool error: Tool locate not executed: its limit of 1 calls per minute is reached", results["c2"])
}

// emptyingRewriter rewrites every result to the empty string.
type emptyingRewriter struct{ wrappingRewriter }

func (h *emptyingRewriter) RewriteResult(ctx context.Context, req hooks.ToolRequest, content string) (string, error) {
	_, _ = h.wrappingRewriter.RewriteResult(ctx, req, content)
	return "", nil
}

// A rewriter that empties a client's answer on Resume gives the model `""`,
// never an empty text block, as on a call the pipeline ran.
func TestResume_RewriterEmptyingTheAnswerGivesTheEmptyStringText(t *testing.T) {
	conv, provider := openLocateConv(t, &emptyingRewriter{},
		[]types.MessageToolCall{{ID: "c1", Name: "locate", Args: []byte(`{}`)}})
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c1", "Paris"))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"c1": `""`}, lastResults(provider))
}

// An approved call is rewritten with its arguments exactly as the model sent
// them: a large integer and the key order survive.
func TestContinue_RewriterSeesTheModelsRawArguments(t *testing.T) {
	const raw = `{"z": 12345678901234567890, "a": 1}`
	rw := &wrappingRewriter{}
	conv, provider := openSettlementConv(t, 0, []string{"Done."}, WithToolHook(rw))
	provider.mu.Lock()
	provider.rounds = append([]providers.PredictionResponse{{Content: "Sending.", ToolCalls: []types.MessageToolCall{
		{ID: "call-1", Name: "send_message", Args: []byte(raw)},
	}}}, provider.rounds...)
	provider.mu.Unlock()
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(map[string]any) (any, error) { return "sent", nil })
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.ResolveTool(ctx, "call-1")
	require.NoError(t, err)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)

	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, raw, string(reqs[0].Args))
}

// failingLoadStore is a memory store whose Load fails while fail is set.
type failingLoadStore struct {
	*statestore.MemoryStore
	fail atomic.Bool
}

func (s *failingLoadStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	if s.fail.Load() {
		return nil, errors.New("store unreachable")
	}
	return s.MemoryStore.Load(ctx, id)
}

// A history the store cannot read fails Continue, rather than reading as an
// empty one; the approved result stays to be continued once it can be read.
func TestContinue_HistoryReadErrorFailsTheBatch(t *testing.T) {
	store := &failingLoadStore{MemoryStore: statestore.NewMemoryStore()}
	conv, provider := openSettlementConv(t, 1, []string{"Done."}, WithStateStore(store))
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(map[string]any) (any, error) { return "sent", nil })
	ctx := context.Background()
	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)

	store.fail.Store(true)
	_, err = conv.Continue(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read the conversation history: store unreachable")

	store.fail.Store(false)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"call-1": "sent"}, lastResults(provider))
}

// An empty text reaches the model as `""` on Resume, with or without a
// rewriter: providers reject an empty text block.
func TestResume_EmptyTextGivesTheEmptyStringText(t *testing.T) {
	for name, rw := range map[string]hooks.ToolHook{"no rewriter": denyNothingHook{}, "rewriter": &wrappingRewriter{}} {
		t.Run(name, func(t *testing.T) {
			conv, provider := openLocateConv(t, rw, []types.MessageToolCall{{ID: "c1", Name: "locate", Args: []byte(`{}`)}})
			ctx := context.Background()
			require.NoError(t, conv.SendToolResultMultimodal(ctx, "c1", []types.ContentPart{types.NewTextPart("")}))
			_, err := conv.Resume(ctx)
			require.NoError(t, err)
			want := `""`
			if name == "rewriter" {
				want = "<redacted:>"
			}
			assert.Equal(t, map[string]string{"c1": want}, lastResults(provider))
		})
	}
}

// denyNothingHook is a tool hook that rewrites nothing.
type denyNothingHook struct{}

func (denyNothingHook) Name() string { return "nothing" }

func (denyNothingHook) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (denyNothingHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}
