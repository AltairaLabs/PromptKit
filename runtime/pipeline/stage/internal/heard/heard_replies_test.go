package heard

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// heardTestDeadline bounds every wait a test makes, so a hang fails fast.
const heardTestDeadline = 5 * time.Second

// within runs f and fails the test when it does not return within the
// deadline.
func within[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case v := <-done:
		return v
	case <-time.After(heardTestDeadline):
		t.Fatalf("%s did not return within %v", what, heardTestDeadline)
		var zero T
		return zero
	}
}

func recvWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	return within(t, "a receive", func() T { return <-ch })
}

func contents(msgs []types.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

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
	Record(h, "Hello.", &heard)
	Record(h, "Never written.", &heard)

	got := Apply(h, history)

	assert.Equal(t, []string{"Hello.", "again", "", "Hello."}, contents(got))
	assert.Equal(t, types.FinishReasonInterrupted, got[2].FinishReason)
	assert.Equal(t, "Hello.", history[2].Content, "the history handed in is not written")
	assert.Equal(t, contents(got), contents(Apply(h, got)), "a cut is applied once")
}

// The provider stage waits for every Interrupt it passed on to be handled
// downstream before it uses the history, and no longer than the bound when
// one never is.
func TestHeardReplies_AwaitWaitsForEachInterruptToSettle(t *testing.T) {
	// Only the settle may end the first wait, never the bound.
	old := SettleTimeout
	SettleTimeout = time.Minute
	t.Cleanup(func() { SettleTimeout = old })
	h := NewHeardReplies()
	Expect(h)
	returned := make(chan struct{})
	go func() {
		Await(context.Background(), h)
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("await returned before the Interrupt was handled")
	case <-time.After(20 * time.Millisecond):
	}
	Settle(h)
	recvWithin(t, returned)

	SettleTimeout = 10 * time.Millisecond
	Expect(h) // never settled
	within(t, "await", func() bool { Await(context.Background(), h); return true })

	var none *HeardReplies
	require.NotPanics(t, func() {
		Expect(none)
		Settle(none)
		Record(none, "x", &types.Message{})
		Await(context.Background(), none)
		assert.Nil(t, Apply(none, nil))
	})
}

// The scan reaches the first message of the history, and swaps only the
// assistant message whose text is the cut reply.
func TestHeardReplies_ApplySwapsOnlyTheMatchingReply(t *testing.T) {
	h := NewHeardReplies()
	history := []types.Message{
		{Role: core.RoleAssistant, Content: "First reply."},
		{Role: core.RoleUser, Content: "next"},
		{Role: core.RoleAssistant, Content: "Second reply."},
	}
	heard := types.Message{Role: core.RoleAssistant, Content: "First", FinishReason: types.FinishReasonInterrupted}
	Record(h, "First reply.", &heard)

	got := Apply(h, history)

	assert.Equal(t, []string{"First", "next", "Second reply."}, contents(got))
	assert.Equal(t, types.FinishReasonInterrupted, got[0].FinishReason)
}

// A wait for an Interrupt that is never handled ends with its context, well
// before the bound.
func TestHeardReplies_AwaitEndsWithItsContext(t *testing.T) {
	old := SettleTimeout
	SettleTimeout = time.Minute
	t.Cleanup(func() { SettleTimeout = old })
	h := NewHeardReplies()
	Expect(h) // never settled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	waited := within(t, "await", func() time.Duration {
		start := time.Now()
		Await(ctx, h)
		return time.Since(start)
	})
	assert.Less(t, waited, time.Second, "the cancelled context ends the wait, not the one-minute bound")
}
