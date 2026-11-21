package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A cut reply is swapped into the history for what was heard, the latest
// match only, on a copy: the original slice, whose messages elements already
// handed on point into, is left as it was.
func TestHeardReplies_ApplySwapsTheLatestMatchOnACopy(t *testing.T) {
	h := NewHeardReplies()
	history := []types.Message{
		{Role: roleAssistant, Content: "Hello."},
		{Role: roleUser, Content: "again"},
		{Role: roleAssistant, Content: "Hello."},
		{Role: roleUser, Content: "Hello."}, // the caller's words are never swapped
	}
	heard := types.Message{Role: roleAssistant, Content: "", FinishReason: types.FinishReasonInterrupted}
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

	send(NewMessageElement(&types.Message{Role: roleUser, Content: "tell me a story"}))
	send(NewEndOfTurnElement())
	for e := recvWithin(t, output); !e.EndOfTurn; e = recvWithin(t, output) {
	}
	send(NewInterruptElement()) // the caller barges in on "reply-1"
	recvWithin(t, output)

	cut := types.Message{Role: roleAssistant, Content: "rep", FinishReason: types.FinishReasonInterrupted,
		Meta: map[string]interface{}{types.MetaPlayoutCut: types.PlayoutCutBargeIn, types.MetaHeardPrefix: true}}
	go func() {
		time.Sleep(20 * time.Millisecond) // the speech-out stage handles the Interrupt
		heard.record("reply-1", &cut)
		heard.settle()
	}()
	send(NewMessageElement(&types.Message{Role: roleUser, Content: "stop"}))
	send(NewEndOfTurnElement())
	close(input)
	require.NoError(t, recvWithin(t, done))

	require.Len(t, prov.requests, 2)
	var assistant []string
	for _, m := range prov.requests[1] {
		if m.Role == roleAssistant {
			assistant = append(assistant, m.Content)
		}
	}
	assert.Equal(t, []string{"rep"}, assistant, "the next turn sees what was heard, not what was written")
}
