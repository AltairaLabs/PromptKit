package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// suspendOnClientSend opens a conversation suspended on one deferred
// send_message client call, with a call-rate limit and opts, and returns it
// with the call's ID.
func suspendOnClientSend(t *testing.T, limit int, opts ...Option) (*Conversation, string) {
	t.Helper()
	conv := openSendMessageConv(t, limit, 1, opts...)
	conv.DeferClientTool("send_message")
	resp, err := conv.Send(context.Background(), "send one")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)
	return conv, resp.ClientTools()[0].CallID
}

// A settled answer is prepared once: rewritten, admitted under the call-rate
// limit and given to the model as prepared, without being rewritten or
// counted again when the turn resumes.
func TestSettleClientTool_PreparesTheResultResumeGives(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, id := suspendOnClientSend(t, 2, WithToolHook(rw))
	ctx := context.Background()

	prepared, answered, err := conv.SettleClientTool(ctx, id, "ok", nil)
	require.NoError(t, err)
	require.True(t, answered)
	assert.Equal(t, `<redacted:"ok">`, prepared.GetTextContent())
	assert.Equal(t, "send_message", prepared.Name)

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	got := toolResultFor(t, conv, id)
	require.NotNil(t, got)
	assert.Equal(t, prepared.GetTextContent(), got.GetTextContent())
	assert.Len(t, rw.requests(), 1, "rewritten once")
	assert.True(t, roomFor(conv, 2), "the answer took one of the two slots, once")
	assert.False(t, roomFor(conv, 2))
}

// A second answer to a call is not answered: nothing is prepared or
// rewritten, and the first answer stands.
func TestSettleClientTool_IgnoresASecondAnswer(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, id := suspendOnClientSend(t, 0, WithToolHook(rw))
	ctx := context.Background()
	require.NoError(t, conv.SendToolResult(ctx, id, "first"))

	prepared, answered, err := conv.SettleClientTool(ctx, id, "second", nil)
	require.NoError(t, err)
	assert.False(t, answered)
	assert.Equal(t, types.MessageToolResult{}, prepared)
	assert.Empty(t, rw.requests())

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"first">`, toolResultFor(t, conv, id).GetTextContent())
}

// A failure is prepared as Resume gives FailClientTool's: the partial output
// rewritten, then the error.
func TestSettleClientTool_PreparesAFailure(t *testing.T) {
	conv, id := suspendOnClientSend(t, 0, WithToolHook(&wrappingRewriter{}))
	prepared, answered, err := conv.SettleClientTool(context.Background(), id,
		map[string]any{"rows": 3}, errors.New("disk full"))
	require.NoError(t, err)
	require.True(t, answered)
	assert.Equal(t, "<redacted:{\"rows\":3}>\n\nTool error: disk full", prepared.GetTextContent())
	assert.Equal(t, "disk full", prepared.Error)
}

// A call the turn is not waiting on is refused, as SendToolResult refuses it.
func TestSettleClientTool_RefusesACallNotAwaited(t *testing.T) {
	conv, _ := suspendOnClientSend(t, 0)
	_, answered, err := conv.SettleClientTool(context.Background(), "nope", "x", nil)
	assert.ErrorIs(t, err, ErrToolCallNotAwaited)
	assert.False(t, answered)
}

// HasToolResultRewriters reports a tool hook that rewrites results.
func TestHasToolResultRewriters(t *testing.T) {
	with, _ := suspendOnClientSend(t, 0, WithToolHook(&wrappingRewriter{}))
	without, _ := suspendOnClientSend(t, 0)
	assert.True(t, with.HasToolResultRewriters())
	assert.False(t, without.HasToolResultRewriters())
}

// A prepared result whose answer Resume drops unused (a second answer to its
// call) is forgotten with it, and its admissions taken back; the used one is
// kept.
func TestTakeSettlement_ForgetsThePreparedResultOfAnUnusedAnswer(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()
	first := &sdktools.ToolResolution{ID: "call_0", ResultJSON: []byte(`"a"`)}
	second := &sdktools.ToolResolution{ID: "call_0", ResultJSON: []byte(`"b"`)}
	conv.resolvedStore.Add(first)
	conv.resolvedStore.Add(second)
	var released []string
	conv.prepared = map[*sdktools.ToolResolution]preparedAnswer{
		first: {result: types.NewTextToolResult("call_0", "", "a"),
			release: func() { released = append(released, "a") }},
		second: {result: types.NewTextToolResult("call_0", "", "b"),
			release: func() { released = append(released, "b") }},
	}
	history := []types.Message{{Role: roleAssistant, ToolCalls: []types.MessageToolCall{{ID: "call_0"}}}}

	kept := conv.takeSettlement(history)
	assert.Equal(t, []*sdktools.ToolResolution{first}, kept)
	assert.True(t, conv.isPrepared(first))
	assert.False(t, conv.isPrepared(second))
	assert.Equal(t, []string{"b"}, released, "the unused answer's admissions are taken back")
}

// gatedRewriter wraps every result's text in "<redacted:...>", holding each
// rewrite until release is closed once entered has been signaled.
type gatedRewriter struct {
	wrappingRewriter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gatedRewriter) RewriteResult(ctx context.Context, req hooks.ToolRequest, content string) (string, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return g.wrappingRewriter.RewriteResult(ctx, req, content)
}

// An answer recorded while a settle prepares its own is a second answer: it
// is dropped at once, without waiting for the settle, and the settled answer
// is the one the model is given: the client is never shown an answer the
// model does not get.
func TestSettleClientTool_AnotherAnswerDuringASettleIsASecondAnswer(t *testing.T) {
	rw := &gatedRewriter{entered: make(chan struct{}), release: make(chan struct{})}
	conv, id := suspendOnClientSend(t, 0, WithToolHook(rw))
	ctx := context.Background()

	settled := make(chan types.MessageToolResult, 1)
	go func() {
		prepared, answered, err := conv.SettleClientTool(ctx, id, "settled", nil)
		assert.NoError(t, err)
		assert.True(t, answered)
		settled <- prepared
	}()
	<-rw.entered
	sent := make(chan error, 1)
	go func() { sent <- conv.SendToolResult(ctx, id, "raw") }()
	select {
	case err := <-sent:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("SendToolResult waited for the settle")
	}
	close(rw.release)
	prepared := <-settled

	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"settled">`, prepared.GetTextContent())
	assert.Equal(t, prepared.GetTextContent(), toolResultFor(t, conv, id).GetTextContent())
	assert.Len(t, rw.requests(), 1, "the dropped answer is never rewritten")
}

// answeringRewriter wraps every result's text in "<redacted:...>" and, on its
// first rewrite, records an answer to another pending call, as a hook that
// answers a call itself might.
type answeringRewriter struct {
	wrappingRewriter
	conv  *Conversation
	other string
	once  sync.Once
	err   error
}

func (a *answeringRewriter) RewriteResult(ctx context.Context, req hooks.ToolRequest, content string) (string, error) {
	a.once.Do(func() { a.err = a.conv.SendToolResult(context.Background(), a.other, "from the hook") })
	return a.wrappingRewriter.RewriteResult(ctx, req, content)
}

// A rewriter that records an answer to another pending call while a settle
// runs it does not deadlock: the settle completes and both answers reach the
// model.
func TestSettleClientTool_RewriterMayRecordAnotherAnswer(t *testing.T) {
	rw := &answeringRewriter{}
	conv := openSendMessageConv(t, 0, 2, WithToolHook(rw))
	conv.DeferClientTool("send_message")
	ctx := context.Background()
	resp, err := conv.Send(ctx, "send two")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 2)
	first, second := resp.ClientTools()[0].CallID, resp.ClientTools()[1].CallID
	rw.conv, rw.other = conv, second

	done := make(chan bool, 1)
	go func() {
		_, answered, settleErr := conv.SettleClientTool(ctx, first, "settled", nil)
		assert.NoError(t, settleErr)
		done <- answered
	}()
	select {
	case answered := <-done:
		require.True(t, answered)
	case <-time.After(5 * time.Second):
		t.Fatal("the settle deadlocked on its rewriter's answer")
	}
	require.NoError(t, rw.err)

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"settled">`, toolResultFor(t, conv, first).GetTextContent())
	assert.Equal(t, `<redacted:"from the hook">`, toolResultFor(t, conv, second).GetTextContent())
}

// A call ID a later turn reuses (call_0, call_0, ...) is answered afresh: the
// earlier turn's settled answer does not count as an answer to the new call.
func TestSettleClientTool_AnswersACallIDALaterTurnReuses(t *testing.T) {
	call := []types.MessageToolCall{{ID: "call_0", Name: "send_message", Args: []byte(`{"body":"x"}`)}}
	provider := &roundsProvider{ToolProvider: mock.NewToolProvider("x", "m", false, nil), rounds: []providers.PredictionResponse{
		{Content: "Sending.", ToolCalls: call}, {Content: "Done."},
		{Content: "Sending again.", ToolCalls: call}, {Content: "Done again."},
	}}
	conv, err := Open(writeGatePack(t), "assist", WithProvider(provider), WithSkipSchemaValidation(),
		WithToolHook(&wrappingRewriter{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.DeferClientTool("send_message")
	ctx := context.Background()

	for i, answer := range []string{"first", "second"} {
		_, err = conv.Send(ctx, "send")
		require.NoError(t, err)
		prepared, answered, err := conv.SettleClientTool(ctx, "call_0", answer, nil)
		require.NoError(t, err)
		require.True(t, answered, "turn %d's call is answered", i+1)
		assert.Equal(t, `<redacted:"`+answer+`">`, prepared.GetTextContent())
		_, err = conv.Resume(ctx)
		require.NoError(t, err)
		assert.Equal(t, prepared.GetTextContent(), toolResultFor(t, conv, "call_0").GetTextContent())
	}
}

// A settled answer whose turn is abandoned (Send starts a new one) gives back
// the room it took under both limits: the next answer is not refused for it.
func TestSettleClientTool_AbandonedAnswerGivesBackItsAdmission(t *testing.T) {
	conv, id := suspendOnClientSend(t, 1, sendMessageRegistry(t, 1))
	ctx := context.Background()
	_, answered, err := conv.SettleClientTool(ctx, id, "sent", nil)
	require.NoError(t, err)
	require.True(t, answered)
	require.False(t, roomFor(conv, 1), "the settled answer took the calls-per-minute slot")
	require.ErrorIs(t, conv.ToolRegistry().AdmitSettled("send_message", nil), tools.ErrRateLimitExceeded,
		"and the registry's")

	_, err = conv.Send(ctx, "never mind")
	require.NoError(t, err)
	assert.True(t, roomFor(conv, 1), "the abandoned answer gave its slot back")
	assert.NoError(t, conv.ToolRegistry().AdmitSettled("send_message", nil), "and the registry's")
}

// A settled rejection is the rejection the model is given, in the words of
// the tool's decline strategy, and a later answer to the call is not taken.
func TestSettleClientToolRejection_IsTheRejectionTheModelIsGiven(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, id := suspendOnClientSend(t, 0, WithToolHook(rw))
	desc, err := conv.ToolRegistry().GetTool("send_message")
	require.NoError(t, err)
	desc.ClientConfig = &tools.ClientConfig{Consent: &tools.ConsentConfig{DeclineStrategy: tools.DeclineStrategyRetry}}
	ctx := context.Background()

	prepared, answered, err := conv.SettleClientToolRejection(ctx, id, "not now")
	require.NoError(t, err)
	require.True(t, answered)
	assert.Equal(t,
		"Tool send_message denied: user declined consent. Try a different approach. Reason: not now",
		prepared.GetTextContent())
	_, answered, err = conv.SettleClientTool(ctx, id, "sent", nil)
	require.NoError(t, err)
	assert.False(t, answered, "the call has its answer")

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, prepared.GetTextContent(), toolResultFor(t, conv, id).GetTextContent())
	assert.Empty(t, rw.requests(), "a rejection is not rewritten")
}

// A settled answer's text is final when it is settled: capped at the
// registry's size limit then, and given to the model as it is by Continue and
// Resume alike, a plain-text or a long rewritten result byte for byte what
// the client was shown.
func TestSettleClientTool_ClientAndModelGetTheSameText(t *testing.T) {
	for name, tc := range map[string]struct {
		answer any
		want   string
	}{
		"long": {answer: "a long answer to cap",
			want: tools.TruncateResultText("send_message", `<redacted:"a long answer to cap">`, 15)},
		"plain text": {answer: map[string]any{"a": 1}, want: `<redacted:{"a":1}>`},
	} {
		for _, deliver := range []string{"Continue", "Resume"} {
			t.Run(name+"/"+deliver, func(t *testing.T) {
				rw := &wrappingRewriter{}
				limit := 15
				if name == "plain text" {
					limit = 1000
				}
				conv, id := suspendOnClientSend(t, 0, WithToolHook(rw), limitedTools(t, limit, "send_message"))
				ctx := context.Background()
				prepared, answered, err := conv.SettleClientTool(ctx, id, tc.answer, nil)
				require.NoError(t, err)
				require.True(t, answered)
				assert.Equal(t, tc.want, prepared.GetTextContent())

				if deliver == "Continue" {
					_, err = conv.Continue(ctx)
				} else {
					_, err = conv.Resume(ctx)
				}
				require.NoError(t, err)
				assert.Equal(t, prepared.GetTextContent(), toolResultFor(t, conv, id).GetTextContent(),
					"the model gets the text the client was shown")
				assert.Len(t, rw.requests(), 1, "rewritten once")
			})
		}
	}
}

// An answer for a call the history already answers (a server tool of the
// same round, which ran before the turn suspended) is not taken: nothing is
// prepared or rewritten for it.
func TestSettleClientTool_IgnoresACallTheHistoryAnswers(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c-srv", Name: "lookup", Args: []byte(`{}`)},
		{ID: "c-cli", Name: "locate", Args: []byte(`{}`)},
	}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Checking.", ToolCalls: calls}},
	}
	rw := &wrappingRewriter{}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(rw))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })
	conv.DeferClientTool("locate")
	ctx := context.Background()
	resp, err := conv.Send(ctx, "go")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	ran := len(rw.requests()) // the server tool's result, rewritten in the turn

	_, answered, err := conv.SettleClientTool(ctx, "c-srv", "again", nil)
	require.NoError(t, err)
	assert.False(t, answered, "the history answers c-srv")
	assert.Len(t, rw.requests(), ran, "nothing more is rewritten")
}

// panickingRewriter panics on its first rewrite, then wraps every result's
// text in "<redacted:...>".
type panickingRewriter struct {
	wrappingRewriter
	once sync.Once
}

func (p *panickingRewriter) RewriteResult(ctx context.Context, req hooks.ToolRequest, content string) (string, error) {
	p.once.Do(func() { panic("rewriter blew up") })
	return p.wrappingRewriter.RewriteResult(ctx, req, content)
}

// A settle whose rewriter panics frees its claim on the call and gives back
// the admission it took: a later answer to the call is taken, admitted under
// the limit, and the turn resumes with it.
func TestSettleClientTool_PanickingRewriterFreesTheCall(t *testing.T) {
	conv, id := suspendOnClientSend(t, 1, WithToolHook(&panickingRewriter{}))
	ctx := context.Background()
	func() {
		defer func() { assert.Equal(t, "rewriter blew up", recover()) }()
		_, _, _ = conv.SettleClientTool(ctx, id, "first", nil)
	}()

	require.NoError(t, conv.SendToolResult(ctx, id, "after"))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"after">`, toolResultFor(t, conv, id).GetTextContent(),
		"the later answer was taken and admitted")
}

// A settle whose rewriter fails records the failure the model is given and
// frees its claim: the turn resumes with that failure, and the call has its
// answer.
func TestSettleClientTool_FailingRewriterFreesTheCall(t *testing.T) {
	conv, id := suspendOnClientSend(t, 0, WithToolHook(&wrappingRewriter{err: errors.New("redactor offline")}))
	ctx := context.Background()
	prepared, answered, err := conv.SettleClientTool(ctx, id, "first", nil)
	require.NoError(t, err)
	require.True(t, answered)
	assert.NotEmpty(t, prepared.Error)
	conv.settleMu.Lock()
	assert.Empty(t, conv.settling, "the claim is freed")
	conv.settleMu.Unlock()
	_, answered, err = conv.SettleClientTool(ctx, id, "again", nil)
	require.NoError(t, err)
	assert.False(t, answered, "the call has its answer")

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, prepared.GetTextContent(), toolResultFor(t, conv, id).GetTextContent())
}
