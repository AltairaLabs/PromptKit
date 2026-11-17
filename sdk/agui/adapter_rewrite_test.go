package agui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// redactHook rewrites every tool result to "<redacted:...>".
type redactHook struct{}

func (redactHook) Name() string { return "redact" }

func (redactHook) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (redactHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (redactHook) RewriteResult(_ context.Context, _ hooks.ToolRequest, content string) (string, error) {
	return "<redacted:" + content + ">", nil
}

// openRewritingConv opens a conversation whose tool results redactHook
// rewrites, with get_location and send_message as client tools.
func openRewritingConv(t *testing.T, provider *scriptedProvider, opts ...sdk.Option) *sdk.Conversation {
	t.Helper()
	opts = append([]sdk.Option{sdk.WithProvider(provider), sdk.WithSkipSchemaValidation(),
		sdk.WithToolHook(redactHook{})}, opts...)
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	bindClientTool(t, conv, "get_location")
	bindClientTool(t, conv, "send_message")
	return conv
}

// A ToolResultProvider that answers only some calls of a conversation that
// rewrites tool results: the answer is not reported when it is given, since
// the client must not see what the rewriter removes. The run that resumes the
// turn reports it once, as the model was given it.
func TestE2E_PartialAnswerIsReportedRewrittenOnResume(t *testing.T) {
	provider := newScriptedProvider(
		say("Checking.", call("c1", "get_location", `{}`), call("c2", "send_message", `{"body":"hi"}`)),
		say("Done."))
	conv := openRewritingConv(t, provider)
	answerOne := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "Paris"}}, nil
	}

	run1, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answerOne)), "where am I?")
	require.NoError(t, err)
	assert.Empty(t, resultIDs(run1), "nothing is reported before the rewriter has run")

	b := NewEventAdapter(conv)
	run2, err := continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "c2", Result: "sent"}})
	})
	require.NoError(t, err)
	assert.Equal(t, []string{`c1=<redacted:"Paris">`}, resultIDs(run2),
		"the withheld answer, rewritten, once; the client's own answer is not echoed")
}

// An answer over ToolPolicy.MaxCallsPerMinute is replaced by the limit error
// when the turn resumes: that error, which the model was given, is what the
// client is shown.
func TestE2E_PartialAnswerOverTheCallRateShowsTheLimitError(t *testing.T) {
	provider := newScriptedProvider(
		say("Checking.", call("c1", "get_location", `{"n":1}`), call("c2", "get_location", `{"n":2}`),
			call("c3", "get_location", `{"n":3}`)),
		say("Done."))
	conv := openRewritingConv(t, provider,
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCallsPerMinute: 1, MaxParallelToolCalls: 1}))
	answerTwo := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "Paris"}, {CallID: "c2", Result: "Lyon"}}, nil
	}

	run1, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answerTwo)), "where am I?")
	require.NoError(t, err)
	assert.Empty(t, resultIDs(run1))

	b := NewEventAdapter(conv)
	run2, err := continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "c3", Result: "Nice"}})
	})
	require.NoError(t, err)
	const limited = "Tool error: Tool get_location not executed: its limit of 1 calls per minute is reached"
	assert.Equal(t, limited, toolResultIn(t, conv, "c2").GetTextContent(), "test premise: c2 is over the limit")
	assert.Equal(t, []string{`c1=<redacted:"Paris">`, "c2=" + limited}, resultIDs(run2))
}

// When every call is answered, the TOOL_CALL_RESULT is the rewritten text
// the model was given.
func TestE2E_FullAnswerEmitsTheRewrittenResult(t *testing.T) {
	provider := newScriptedProvider(say("Checking.", call("c1", "get_location", `{}`)), say("Done."))
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat", sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(), sdk.WithToolHook(redactHook{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	bindClientTool(t, conv, "get_location")
	answer := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "Paris"}}, nil
	}

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answer)), "where am I?")
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"Paris">`, resultContent(t, evts, "c1"))
}
