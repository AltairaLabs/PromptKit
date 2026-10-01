package agui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// A ToolResultProvider answered one of two pending calls, so its answer waits
// in the conversation for the rest. The user then sends a new message instead
// of answering: the waiting answer belongs to the abandoned turn and must not
// reach the model when a later turn resumes.

// toolContents returns the content of every tool result for callID in msgs.
func toolContents(msgs []types.Message, callID string) []string {
	var out []string
	for i := range msgs {
		if r := msgs[i].ToolResult; r != nil && r.ID == callID {
			out = append(out, r.GetTextContent())
		}
	}
	return out
}

func abandonThenResume(t *testing.T, firstIDs [2]string, laterID string) *scriptedProvider {
	t.Helper()
	provider := newScriptedProvider(
		say("Two things.", call(firstIDs[0], "get_location", `{}`), call(firstIDs[1], "send_message", `{"body":"hi"}`)),
		say("Something else?", call(laterID, "get_location", `{}`)),
		say("Done."),
	)
	conv := openConv(t, provider)
	bindClientTool(t, conv, "get_location")
	bindClientTool(t, conv, "send_message")

	answerFirst := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: firstIDs[0], Result: "STALE"}}, nil
	}
	_, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answerFirst)), "do two things")
	require.NoError(t, err)

	// The user moves on instead of answering the second call.
	_, err = sendAndCollect(t, NewEventAdapter(conv), "actually, something else")
	require.NoError(t, err)

	b := NewEventAdapter(conv)
	_, err = runAndCollect(t, b, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: laterID, Result: "NEW"}})
	})
	require.NoError(t, err)
	return provider
}

// Gemini numbers call ids per response, so the later call reuses call_0: the
// abandoned turn's answer must not win over the real one.
func TestE2E_AbandonedAnswerDoesNotLeak_ReusedID(t *testing.T) {
	provider := abandonThenResume(t, [2]string{"call_0", "call_1"}, "call_0")

	last := provider.seen[len(provider.seen)-1]
	contents := toolContents(last, "call_0")
	require.NotEmpty(t, contents)
	assert.Equal(t, `"NEW"`, contents[len(contents)-1], "the later call gets the later answer")
	assert.NotContains(t, contents, `"STALE"`)
}

// With unique ids the abandoned answer would be a result for a call the last
// assistant message never made, which providers reject.
func TestE2E_AbandonedAnswerDoesNotLeak_UniqueIDs(t *testing.T) {
	provider := abandonThenResume(t, [2]string{"c1", "c2"}, "c3")

	last := provider.seen[len(provider.seen)-1]
	assert.Empty(t, toolContents(last, "c1"), "no result for the abandoned turn's call")
	assert.Equal(t, []string{`"NEW"`}, toolContents(last, "c3"))
}

// The approval-hold variant: a held call resolved but never continued, then a
// new message, then a later hold continued.
func TestE2E_AbandonedApprovalDoesNotLeak(t *testing.T) {
	provider := newScriptedProvider(
		say("Sending.", call("h1", "send_message", `{"body":"first"}`)),
		say("Sending again.", call("h2", "send_message", `{"body":"second"}`)),
		say("Sent."),
	)
	conv := openConv(t, provider)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) { return args["body"], nil },
	)
	ctx := context.Background()

	_, err := sendAndCollect(t, NewEventAdapter(conv), "send first")
	require.NoError(t, err)
	_, err = conv.ResolveTool(ctx, "h1")
	require.NoError(t, err)

	// The user moves on instead of continuing.
	run2, err := sendAndCollect(t, NewEventAdapter(conv), "send second instead")
	require.NoError(t, err)
	_, err = conv.ResolveTool(ctx, "h2")
	require.NoError(t, err)

	b := NewEventAdapter(conv)
	_, err = continueAndCollect(t, b, run2, b.RunContinue)
	require.NoError(t, err)

	last := provider.seen[len(provider.seen)-1]
	assert.Empty(t, toolContents(last, "h1"), "the abandoned hold's result does not reach the model")
	assert.Equal(t, []string{`"second"`}, toolContents(last, "h2"))
}

// A provider answered call_0 in run 1 and its TOOL_CALL_RESULT went out
// then; call_1 waited for the application. Run 2 resumes with the
// application's answer, and the resumed turn feeds both answers to the model.
// Neither is reported again: call_0 was reported in run 1, call_1 is the
// application's own.
func TestE2E_ResumeDoesNotRepeatProviderAnswers(t *testing.T) {
	provider := newScriptedProvider(
		say("Two things.", call("call_0", "get_location", `{}`), call("call_1", "send_message", `{"body":"hi"}`)),
		say("Done."),
	)
	conv := openConv(t, provider)
	bindClientTool(t, conv, "get_location")
	bindClientTool(t, conv, "send_message")
	answerFirst := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "call_0", Result: "Paris"}}, nil
	}

	run1, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answerFirst)), "go")
	require.NoError(t, err)
	assert.Equal(t, []string{`call_0=Paris`}, resultIDs(run1))

	b := NewEventAdapter(conv)
	run2, err := continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "call_1", Result: "sent"}})
	})
	require.NoError(t, err)

	assert.Empty(t, resultIDs(run2), "no answer is reported twice")
	assert.Equal(t, map[string]int{"call_0": 1, "call_1": 1}, toolResultCounts(provider.seen[len(provider.seen)-1]))
}
