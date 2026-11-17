package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
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
	conv.recordAdmitted("call_0", "lookup_order", map[string]any{"order_id": "ORD-1"})
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
	conv.recordAdmitted("call_9", "lookup_order", map[string]any{"order_id": "ORD-1"})
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
	conv.recordAdmitted("call_9", "lookup_order", map[string]any{"order_id": "ORD-1"})
	conv.resolvedStore.Add(&sdktools.ToolResolution{ID: "call_9", ResultJSON: []byte(`"shipped"`)})

	require.NoError(t, conv.ContinueDuplex(context.Background()))
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "call_9", Result: "<redacted:shipped>"}}}, sess.submitted)
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "lookup_order", reqs[0].Name)
	assert.JSONEq(t, `{"order_id":"ORD-1"}`, string(reqs[0].Args))
}

// Rewriting a text part keeps the part's other fields.
func TestRewriteTextParts_KeepsThePartsOtherFields(t *testing.T) {
	text := "a photo"
	media := &types.MediaContent{MIMEType: "text/plain"}
	parts := []types.ContentPart{{Type: types.ContentTypeText, Text: &text, Media: media}}
	got, err := rewriteTextParts(parts, func(s string) (string, error) { return "<" + s + ">", nil })
	require.NoError(t, err)
	want := "<a photo>"
	assert.Equal(t, []types.ContentPart{{Type: types.ContentTypeText, Text: &want, Media: media}}, got)
	assert.Equal(t, "a photo", *parts[0].Text, "the caller's part is not changed")
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

// ToolResultForModel tells the result Resume gives the model for a stored
// answer, and the rewriter sees the answer once.
func TestToolResultForModel_IsWhatResumeGives(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "c-cli", Name: "locate", Args: []byte(`{}`)}}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	rw := &wrappingRewriter{}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(rw))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	ctx := context.Background()
	_, err = conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	_, found := conv.ToolResultForModel(ctx, "c-cli")
	assert.False(t, found, "no answer is stored yet")
	require.NoError(t, conv.SendToolResult(ctx, "c-cli", "Paris"))

	preview, found := conv.ToolResultForModel(ctx, "c-cli")
	require.True(t, found)
	assert.Equal(t, `<redacted:"Paris">`, preview.GetTextContent())
	assert.Equal(t, "locate", preview.Name)

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	provider.mu.Lock()
	var seen *types.MessageToolResult
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			seen = m.ToolResult
		}
	}
	provider.mu.Unlock()
	require.NotNil(t, seen)
	assert.Equal(t, *preview, *seen)
	assert.Len(t, rw.requests(), 1, "the rewriter sees the answer once")
	_, found = conv.ToolResultForModel(ctx, "c-cli")
	assert.False(t, found, "the answer was given to the model")
}

// RewritesToolResults reports a tool hook that rewrites results.
func TestRewritesToolResults(t *testing.T) {
	conv := newTestConversation()
	assert.False(t, conv.RewritesToolResults())
	conv.hookRegistry = hooks.NewRegistry(hooks.WithToolHook(&wrappingRewriter{}))
	assert.True(t, conv.RewritesToolResults())
}
