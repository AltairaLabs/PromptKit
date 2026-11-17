package agui

import (
	"context"
	"sync"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// countingRedactor rewrites every tool result to "<redacted:...>" and counts
// the rewrites of each call.
type countingRedactor struct {
	mu    sync.Mutex
	calls map[string]int
}

func (*countingRedactor) Name() string { return "counting-redact" }

func (*countingRedactor) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (*countingRedactor) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (r *countingRedactor) RewriteResult(_ context.Context, req hooks.ToolRequest, content string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	r.calls[req.CallID]++
	return "<redacted:" + content + ">", nil
}

func (r *countingRedactor) rewrites() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	for k, v := range r.calls {
		out[k] = v
	}
	return out
}

// openPartialConv opens a conversation whose first round calls three client
// tools, c1 and c3 (get_location) and c2 (send_message), then answers "Done.".
func openPartialConv(t *testing.T, opts ...sdk.Option) (*sdk.Conversation, *scriptedProvider) {
	t.Helper()
	provider := newScriptedProvider(
		say("Checking.", call("c1", "get_location", `{}`), call("c2", "send_message", `{}`),
			call("c3", "get_location", `{}`)),
		say("Done."),
	)
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat",
		append([]sdk.Option{sdk.WithProvider(provider), sdk.WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	bindClientTool(t, conv, "get_location")
	bindClientTool(t, conv, "send_message")
	return conv, provider
}

// partialRun runs a turn whose ToolResultProvider gives answers, leaving c2
// for the application, then resumes it with the application's answer to c2.
// It returns the first run's events and the tool results the model was given
// on resuming, by call ID.
func partialRun(
	t *testing.T, conv *sdk.Conversation, provider *scriptedProvider, answers []ToolResult,
) ([]aguievents.Event, map[string]string) {
	t.Helper()
	answer := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) { return answers, nil }
	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answer)), "where am I?")
	require.NoError(t, err)

	b := NewEventAdapter(conv)
	_, err = continueAndCollect(t, b, evts, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "c2", Result: "sent"}})
	})
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	given := map[string]string{}
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			given[m.ToolResult.ID] = m.ToolResult.GetTextContent()
		}
	}
	return evts, given
}

// A server-produced answer reported before the turn resumes is the result the
// model is given: rewritten before it is reported, and not rewritten again
// when the turn resumes.
func TestPartialAnswer_ReportsTheRewrittenResultTheModelIsGiven(t *testing.T) {
	rw := &countingRedactor{}
	conv, provider := openPartialConv(t, sdk.WithToolHook(rw))
	evts, given := partialRun(t, conv, provider, []ToolResult{
		{CallID: "c1", Result: "Paris"},
		{CallID: "c3", Error: "gps offline", Result: map[string]any{"fix": "none"}},
	})

	assert.Equal(t, `<redacted:"Paris">`, resultContent(t, evts, "c1"))
	assert.Equal(t, given["c1"], resultContent(t, evts, "c1"), "the client sees what the model is given")
	assert.Equal(t, "<redacted:{\"fix\":\"none\"}>\n\nTool error: gps offline", resultContent(t, evts, "c3"))
	assert.Equal(t, given["c3"], resultContent(t, evts, "c3"))
	assert.Equal(t, map[string]int{"c1": 1, "c2": 1, "c3": 1}, rw.rewrites(),
		"each answer is rewritten once, across the answer and the resume")
}

// A second answer to a call is ignored without being rewritten: the first is
// reported and given to the model.
func TestPartialAnswer_DuplicateAnswerIsNotRewritten(t *testing.T) {
	rw := &countingRedactor{}
	conv, provider := openPartialConv(t, sdk.WithToolHook(rw))
	evts, given := partialRun(t, conv, provider, []ToolResult{
		{CallID: "c1", Result: "Paris"},
		{CallID: "c1", Result: "Lyon"},
		{CallID: "c3", Result: "Rome"},
	})

	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 2, "one TOOL_CALL_RESULT per call")
	assert.Equal(t, `<redacted:"Paris">`, resultContent(t, evts, "c1"))
	assert.Equal(t, `<redacted:"Paris">`, given["c1"])
	assert.Equal(t, 1, rw.rewrites()["c1"], "the duplicate is not rewritten")
}

// An answer the call-rate limit refuses is reported as the limit's error, as
// the model is told, and never rewritten.
func TestPartialAnswer_RateLimitedAnswerIsNotRewritten(t *testing.T) {
	rw := &countingRedactor{}
	conv, provider := openPartialConv(t, sdk.WithToolHook(rw),
		sdk.WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: 1}))
	evts, given := partialRun(t, conv, provider, []ToolResult{
		{CallID: "c1", Result: "Paris"},
		{CallID: "c3", Result: "Rome"},
	})

	const limited = "Tool error: Tool get_location not executed: its limit of 1 calls per minute is reached"
	assert.Equal(t, limited, resultContent(t, evts, "c3"))
	assert.Equal(t, limited, given["c3"])
	assert.Equal(t, `<redacted:"Paris">`, resultContent(t, evts, "c1"))
	assert.Equal(t, map[string]int{"c1": 1, "c2": 1}, rw.rewrites(), "the refused answer is never rewritten")
}

// Without a rewriter, the answer is reported as it always was: the value the
// provider gave, before the turn resumes.
func TestPartialAnswer_WithoutARewriterReportsTheAnswerAsGiven(t *testing.T) {
	conv, provider := openPartialConv(t)
	evts, given := partialRun(t, conv, provider, []ToolResult{
		{CallID: "c1", Result: "Paris"},
		{CallID: "c3", Result: map[string]any{"city": "Rome"}},
	})

	assert.Equal(t, "Paris", resultContent(t, evts, "c1"))
	assert.Equal(t, `{"city":"Rome"}`, resultContent(t, evts, "c3"))
	assert.Equal(t, `"Paris"`, given["c1"])
}
