package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore/file"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// bareBlockHook is a hand-written provider hook that blocks any turn whose
// latest message mentions Bruno. It returns no validator metadata, so no
// validation is recorded for the block: the blocked turn must be left out of
// model context without one.
type bareBlockHook struct{}

func (bareBlockHook) Name() string { return "bare-block" }

func (bareBlockHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	if n := len(req.Messages); n > 0 && strings.Contains(req.Messages[n-1].GetContent(), "Bruno") {
		req.Replacement = historyCannedReply
		return hooks.Enforced("mentions Bruno", nil)
	}
	return hooks.Allow
}

func (bareBlockHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func TestProviderHook_BlockedInputWithoutValidationIsNotSentLater(t *testing.T) {
	for name, send := range map[string]func(*testing.T) func(*Conversation, string) string{
		"send": sendText, "stream": streamText,
	} {
		t.Run(name, func(t *testing.T) {
			prov := newHistoryRecordingProvider()
			conv, err := Open(guardrailTestPack, "chat",
				WithProvider(prov), WithSkipSchemaValidation(), WithProviderHook(bareBlockHook{}))
			require.NoError(t, err)
			defer conv.Close()

			do := send(t)
			do(conv, historyCleanFirst)
			require.Equal(t, historyCannedReply, do(conv, historyBlockedInput))
			do(conv, historyCleanSecond)

			msgs := conv.Messages(context.Background())
			require.Len(t, msgs, 6)
			assert.Empty(t, msgs[3].Validations, "the hook supplies no validator, so nothing is recorded")
			assert.Equal(t, []string{
				"user:" + historyCleanFirst,
				"assistant:" + msgs[1].GetContent(),
				"user:" + historyBlockedInput,
				"assistant:" + historyCannedReply + " [safety]",
				"user:" + historyCleanSecond,
				"assistant:" + msgs[5].GetContent(),
			}, transcriptLines(msgs))
			assert.Equal(t, [][]string{
				{"user:" + historyCleanFirst},
				{"user:" + historyCleanFirst, "assistant:" + msgs[1].GetContent(), "user:" + historyCleanSecond},
			}, prov.requests())
		})
	}
}

// An output guardrail that blocks a streamed reply marks the turn safety, as
// it does a unary one: on the returned message and the stored one.
func TestGuardrail_StreamedOutputBlockIsMarkedSafety(t *testing.T) {
	for name, send := range map[string]func(*Conversation, string) *Response{
		"send": func(conv *Conversation, text string) *Response {
			resp, err := conv.Send(context.Background(), text)
			require.NoError(t, err)
			return resp
		},
		"stream": func(conv *Conversation, text string) *Response {
			return collectStreamDone(t, conv.Stream(context.Background(), text))
		},
	} {
		t.Run(name, func(t *testing.T) {
			conv, err := Open(blockedInputHistoryPack, "chat-output",
				WithProvider(newHistoryRecordingProvider()), WithSkipSchemaValidation())
			require.NoError(t, err)
			defer conv.Close()

			resp := send(conv, historyCleanFirst)
			require.Equal(t, "That reply was withheld.", resp.Text())
			require.NotNil(t, resp.Message())
			assert.Equal(t, types.FinishReasonSafety, resp.Message().FinishReason)

			msgs := conv.Messages(context.Background())
			require.Len(t, msgs, 2)
			assert.Equal(t, "That reply was withheld.", msgs[1].GetContent())
			assert.Equal(t, types.FinishReasonSafety, msgs[1].FinishReason)
		})
	}
}

// The carry-forward summary a workflow transition hands the next state is
// model context: it leaves out a blocked input turn, as the provider request
// does.
func TestWorkflowCarryForward_LeavesOutBlockedInputTurn(t *testing.T) {
	conv, err := Open(blockedInputHistoryPack, "chat",
		WithProvider(newHistoryRecordingProvider()), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer conv.Close()
	do := sendText(t)
	firstReply := do(conv, historyCleanFirst)
	require.Equal(t, historyCannedReply, do(conv, historyBlockedInput))

	assert.Equal(t,
		"[Previous state: intake, 2 messages]\n"+
			"user: "+historyCleanFirst+"\n"+
			"assistant: "+firstReply+"\n",
		buildContextSummary("intake", conv))
}

func TestProviderHook_BlockedInputWithoutValidationStaysOutAfterResume(t *testing.T) {
	store, err := file.NewStore(file.Options{Root: t.TempDir()})
	require.NoError(t, err)
	const convID = "bare-hook-resume"
	ctx := context.Background()

	first, err := Open(guardrailTestPack, "chat",
		WithProvider(newHistoryRecordingProvider()), WithSkipSchemaValidation(),
		WithProviderHook(bareBlockHook{}), WithStateStore(store), WithConversationID(convID))
	require.NoError(t, err)
	resp, err := first.Send(ctx, historyBlockedInput)
	require.NoError(t, err)
	require.Equal(t, historyCannedReply, resp.Text())
	require.NoError(t, first.Close())

	prov := newHistoryRecordingProvider()
	resumed, err := Resume(convID, guardrailTestPack, "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStateStore(store))
	require.NoError(t, err)
	defer resumed.Close()
	_, err = resumed.Send(ctx, historyCleanSecond)
	require.NoError(t, err)

	assert.Equal(t, [][]string{{"user:" + historyCleanSecond}}, prov.requests(),
		"the persisted mark must keep the blocked turn out, with no hook configured on resume")
	msgs := resumed.Messages(ctx)
	require.Len(t, msgs, 4)
	assert.Equal(t, types.FinishReasonSafety, msgs[1].FinishReason)
	assert.Equal(t, historyCannedReply, msgs[1].GetContent())
}
