package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A cut reply is swapped into the history for what was heard, the latest
// match only, on a copy: the original slice, whose messages elements already
// handed on point into, is left as it was.
func TestHeardReplies_ApplySwapsTheLatestMatchOnACopy(t *testing.T) {
	h := NewHeardReplies()
	history := []types.Message{
		{Role: core.RoleAssistant, Content: "Hello."},
		{Role: core.RoleUser, Content: "again"},
		{Role: core.RoleAssistant, Content: "Hello."},
		{Role: core.RoleUser, Content: "Hello."}, // the caller's words are never swapped
	}
	heard := types.Message{Role: core.RoleAssistant, Content: "", FinishReason: types.FinishReasonInterrupted}
	h.record("Hello.", &heard)
	h.record("Never written.", &heard)

	got := h.apply(history)

	assert.Equal(t, []string{"Hello.", "again", "", "Hello."}, contents(got))
	assert.Equal(t, types.FinishReasonInterrupted, got[2].FinishReason)
	assert.Equal(t, "Hello.", history[2].Content, "the history handed in is not written")
	assert.Equal(t, contents(got), contents(h.apply(got)), "a cut is applied once")
}

func contents(msgs []types.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

// The provider stage waits for every Interrupt it passed on to be handled
// downstream before it uses the history, and no longer than the bound when
// one never is.
func TestHeardReplies_AwaitWaitsForEachInterruptToSettle(t *testing.T) {
	h := NewHeardReplies()
	h.expect()
	returned := make(chan struct{})
	go func() {
		h.await(context.Background())
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("await returned before the Interrupt was handled")
	case <-time.After(20 * time.Millisecond):
	}
	h.settle()
	recvWithin(t, returned)

	old := heardSettleTimeout
	heardSettleTimeout = 10 * time.Millisecond
	t.Cleanup(func() { heardSettleTimeout = old })
	h.expect() // never settled
	within(t, "await", func() bool { h.await(context.Background()); return true })

	var none *HeardReplies
	require.NotPanics(t, func() {
		none.expect()
		none.settle()
		none.record("x", &types.Message{})
		none.await(context.Background())
		assert.Nil(t, none.apply(nil))
	})
}

// The provider stage's next turn sends the model what the caller heard of a
// reply the speech-out stage cut, once that stage has handled the barge-in's
// Interrupt; and a heard prefix stays in the request (types.ExcludeInterrupted
// keeps it).
func TestProviderStage_NextTurnSeesWhatTheCallerHeard(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	heard := NewHeardReplies()
	st := NewProviderStageWithTurnState(prov, nil, nil,
		&ProviderConfig{Streaming: true, HeardReplies: heard}, nil, nil, NewTurnState())

	input := make(chan StreamElement)
	output := make(chan StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- st.Process(context.Background(), input, output) }()
	send := func(e StreamElement) { within(t, "a send", func() bool { input <- e; return true }) }

	send(NewMessageElement(&types.Message{Role: core.RoleUser, Content: "tell me a story"}))
	send(NewEndOfTurnElement())
	for e := recvWithin(t, output); !e.EndOfTurn; e = recvWithin(t, output) {
	}
	send(NewInterruptElement()) // the caller barges in on "reply-1"
	recvWithin(t, output)

	cut := types.Message{Role: core.RoleAssistant, Content: "rep", FinishReason: types.FinishReasonInterrupted,
		Meta: map[string]interface{}{types.MetaPlayoutCut: types.PlayoutCutBargeIn, types.MetaHeardPrefix: true}}
	go func() {
		time.Sleep(20 * time.Millisecond) // the speech-out stage handles the Interrupt
		heard.record("reply-1", &cut)
		heard.settle()
	}()
	send(NewMessageElement(&types.Message{Role: core.RoleUser, Content: "stop"}))
	send(NewEndOfTurnElement())
	close(input)
	require.NoError(t, recvWithin(t, done))

	require.Len(t, prov.requests, 2)
	var assistant []string
	for _, m := range prov.requests[1] {
		if m.Role == core.RoleAssistant {
			assistant = append(assistant, m.Content)
		}
	}
	assert.Equal(t, []string{"rep"}, assistant, "the next turn sees what was heard, not what was written")
}

// A barge-in keeps the rounds of a turn that completed: each assistant
// message whose every tool call was answered, with its results, up to the
// first round that did not complete.
func TestCompletedRounds(t *testing.T) {
	call := func(ids ...string) types.Message {
		m := types.Message{Role: core.RoleAssistant, Content: "Let me check."}
		for _, id := range ids {
			m.ToolCalls = append(m.ToolCalls, types.MessageToolCall{ID: id, Name: "t"})
		}
		return m
	}
	result := func(id string) types.Message {
		return types.Message{Role: core.RoleTool, ToolResult: &types.MessageToolResult{ID: id}}
	}
	// A scan that makes no progress loops forever: every call is bounded.
	kept := func(msgs []types.Message) []types.Message {
		return within(t, "completedRounds", func() []types.Message { return completedRounds(msgs) })
	}
	fragment := types.Message{Role: core.RoleAssistant, Content: "It is", FinishReason: types.FinishReasonInterrupted}
	text := types.Message{Role: core.RoleAssistant, Content: "Sure."}

	assert.Nil(t, kept(nil))
	assert.Nil(t, kept([]types.Message{call("c1")}), "a call with no result is not a completed round")
	assert.Nil(t, kept([]types.Message{call("c1", "c2"), result("c1")}), "every call must be answered")
	assert.Nil(t, kept([]types.Message{fragment}), "an unfinished fragment is not a round")
	assert.Equal(t, []types.Message{call("c1"), result("c1")},
		kept([]types.Message{call("c1"), result("c1"), call("c2")}), "up to the round that did not complete")
	assert.Equal(t, []types.Message{call("c1", "c2"), result("c2"), result("c1")},
		kept([]types.Message{call("c1", "c2"), result("c2"), result("c1"), fragment}))
	assert.Equal(t, []types.Message{call("c1"), result("c1"), text},
		kept([]types.Message{call("c1"), result("c1"), text}))
}
