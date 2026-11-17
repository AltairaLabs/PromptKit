package stage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// txtElem wraps assistant text in a StreamElement. Each call captures a distinct
// string variable, so &x is safe to take.
func txtElem(x string) StreamElement { return StreamElement{Text: &x} }

func userElem(content string) StreamElement {
	return StreamElement{Message: &types.Message{Role: "user", Content: content}}
}

func asstEndElem(content string) StreamElement {
	return StreamElement{EndOfStream: true, Message: &types.Message{Role: "assistant", Content: content}}
}

// drainOrder records the observable order of user turns, assistant text, audio,
// and turn ends from the stage output.
func drainOrder(out <-chan StreamElement) []string {
	var order []string
	for e := range out {
		switch {
		case e.Message != nil && e.Message.Role == "user":
			order = append(order, "USER:"+e.Message.Content)
		default:
			if l := elemLabel(&e); l != "" {
				order = append(order, l)
			}
		}
	}
	return order
}

// elemLabel names a non-user element for an order assertion ("" for one the
// tests don't track).
func elemLabel(e *StreamElement) string {
	switch {
	case e.Message != nil && e.Message.Role == "tool":
		return "TOOL:" + e.Message.ToolResult.ID
	case e.Audio != nil:
		return "AUDIO"
	case e.Text != nil:
		return "TXT:" + *e.Text
	case e.EndOfStream && e.Message != nil && len(e.Message.ToolCalls) > 0:
		return "END:calls"
	case e.EndOfStream:
		return "END"
	case len(e.Meta.PendingTools) > 0:
		return "PENDING:" + e.Meta.PendingTools[0].CallID
	case e.Error != nil:
		return "ERR"
	case e.Message != nil:
		return strings.ToUpper(e.Message.Role) + ":" + e.Message.Content
	}
	return ""
}

// lateUserElem is a user transcript the duplex stage marked late: it belongs
// to the turn whose reply already completed.
func lateUserElem(content string) StreamElement {
	e := userElem(content)
	e.Meta.LateTranscript = true
	return e
}

// runningToolCallEndElem is a tool-call turn-end whose calls the duplex stage
// is running.
func runningToolCallEndElem(id string) StreamElement {
	e := toolCallEndElem(id)
	e.Meta.ToolCallsRunning = true
	return e
}

// reorderRun drives a reorder stage element by element and collects its
// output; wait gives the stage time to act on a timer.
type reorderRun struct {
	in  chan StreamElement
	out chan StreamElement
}

func startReorder(s *TranscriptReorderStage) *reorderRun {
	r := &reorderRun{in: make(chan StreamElement, 32), out: make(chan StreamElement, 64)}
	go func() { _ = s.Process(context.Background(), r.in, r.out) }()
	return r
}

// finish closes the input and returns everything the stage emitted.
func (r *reorderRun) finish() []string {
	close(r.in)
	return drainOrder(r.out)
}

// toolCallEndElem is the turn-end of a model response that made a tool call.
func toolCallEndElem(id string) StreamElement {
	return StreamElement{EndOfStream: true, Message: &types.Message{
		Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: id, Name: "lookup"}}}}
}

// toolResultElem is the tool result message for call id.
func toolResultElem(id string) StreamElement {
	msg := types.NewToolResultMessage(types.NewTextToolResult(id, "lookup", "found"))
	return StreamElement{Message: &msg}
}

// TestTranscriptReorderStage_UserBeforeAssistant is the core guarantee: when the
// provider delivers assistant text BEFORE the user's transcript (OpenAI Realtime
// ordering), the stage emits the user turn first, then the buffered assistant
// text, then streams the rest live.
func TestTranscriptReorderStage_UserBeforeAssistant(t *testing.T) {
	s := NewTranscriptReorderStage("[no transcription available]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	in <- txtElem("Hi ")   // assistant starts before the transcript
	in <- txtElem("there") // still buffered
	in <- userElem("Hello!")
	in <- txtElem("!") // after the user turn — streams live
	in <- asstEndElem("Hi there!")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))

	assert.Equal(t, []string{"USER:Hello!", "TXT:Hi ", "TXT:there", "TXT:!", "END"}, drainOrder(out),
		"the user transcript must precede that turn's assistant text")
}

// TestTranscriptReorderStage_MissingTranscriptPlaceholder: a turn ends with no
// transcript — the stage emits the configured placeholder user turn so the reply
// isn't shown with no prompt, then flushes the buffered assistant text.
func TestTranscriptReorderStage_MissingTranscriptPlaceholder(t *testing.T) {
	s := NewTranscriptReorderStage("[no transcription available]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	in <- txtElem("Reply ")
	in <- txtElem("only")
	in <- asstEndElem("Reply only")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))

	assert.Equal(t, []string{"USER:[no transcription available]", "TXT:Reply ", "TXT:only", "END"}, drainOrder(out),
		"a turn with no transcript must get the placeholder user turn before its assistant text")
}

// TestTranscriptReorderStage_EmptyPlaceholderOmitsUserTurn: with an empty
// placeholder, a transcript-less turn simply flushes the assistant text with no
// user turn.
func TestTranscriptReorderStage_EmptyPlaceholderOmitsUserTurn(t *testing.T) {
	s := NewTranscriptReorderStage("")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	in <- txtElem("Reply")
	in <- asstEndElem("Reply")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"TXT:Reply", "END"}, drainOrder(out))
}

// TestTranscriptReorderStage_AudioNotBuffered: audio must pass through
// immediately (realtime playback), never held waiting for the transcript.
func TestTranscriptReorderStage_AudioNotBuffered(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	in <- StreamElement{Audio: &AudioData{Samples: []byte{1, 2}, SampleRate: 24000, Channels: 1}}
	in <- txtElem("text")
	in <- userElem("hi")
	in <- asstEndElem("text")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	// Audio is forwarded immediately (before the user turn); text is reordered
	// after the user turn.
	assert.Equal(t, []string{"AUDIO", "USER:hi", "TXT:text", "END"}, drainOrder(out))
}

// collectUntil reads elements until it has seen `endMarks` END markers or the
// deadline elapses, returning the observable order.
func collectUntil(out <-chan StreamElement, endMarks int, deadline time.Duration) []string {
	var order []string
	ends := 0
	timeout := time.After(deadline)
	for ends < endMarks {
		select {
		case e := <-out:
			if e.Message != nil && e.Message.Role == "user" {
				order = append(order, "USER:"+e.Message.Content)
				continue
			}
			if e.EndOfStream {
				ends++
				order = append(order, "END")
				continue
			}
			if l := elemLabel(&e); l != "" {
				order = append(order, l)
			}
		case <-timeout:
			return order
		}
	}
	return order
}

// TestTranscriptReorderStage_LateTranscriptHeldInOrder is the short-reply fix: the
// turn ends BEFORE the transcript arrives, so the stage must HOLD the turn-end,
// then emit the late transcript ahead of the assistant text — never the
// placeholder, and never a user turn after the turn boundary.
func TestTranscriptReorderStage_LateTranscriptHeldInOrder(t *testing.T) {
	s := NewTranscriptReorderStage("[no transcription available]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	in <- txtElem("Hi!")          // assistant text
	in <- asstEndElem("Hi!")      // turn ends — no transcript yet (held)
	in <- userElem("hello there") // transcript arrives AFTER the turn end
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:hello there", "TXT:Hi!", "END"}, drainOrder(out),
		"a late transcript must be reordered ahead of the reply, with no placeholder")
}

// TestTranscriptReorderStage_HoldTimeoutFallsBackToPlaceholder: when no transcript
// arrives within the hold timeout, the placeholder is emitted.
func TestTranscriptReorderStage_HoldTimeoutFallsBackToPlaceholder(t *testing.T) {
	t.Parallel()
	s := NewTranscriptReorderStageWithTimeout("[none]", 40*time.Millisecond)
	in := make(chan StreamElement, 4)
	out := make(chan StreamElement, 16)

	go func() { _ = s.Process(context.Background(), in, out) }()
	in <- txtElem("Reply")
	in <- asstEndElem("Reply")
	// Do not send a transcript — the hold timeout must fire and emit the placeholder.
	got := collectUntil(out, 1, 2*time.Second)
	close(in)
	assert.Equal(t, []string{"USER:[none]", "TXT:Reply", "END"}, got)
}

// TestTranscriptReorderStage_NextTurnResolvesPending: if the next turn's content
// arrives while still awaiting a transcript, the previous turn resolves with the
// placeholder before the new turn is handled.
func TestTranscriptReorderStage_NextTurnResolvesPending(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	in <- txtElem("A1")
	in <- asstEndElem("A1") // held, no transcript
	in <- txtElem("A2")     // next turn starts before A1's transcript → resolve A1
	in <- userElem("q2")
	in <- asstEndElem("A2")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "TXT:A1", "END", "USER:q2", "TXT:A2", "END"}, drainOrder(out))
}

// TestTranscriptReorderStage_MultiTurnResets: state resets per turn so each
// turn's transcript precedes its own assistant text.
func TestTranscriptReorderStage_MultiTurnResets(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	in <- txtElem("A1")
	in <- userElem("q1")
	in <- asstEndElem("A1")
	in <- txtElem("A2") // next turn, buffered again
	in <- userElem("q2")
	in <- asstEndElem("A2")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:q1", "TXT:A1", "END", "USER:q2", "TXT:A2", "END"}, drainOrder(out))
}

// A tool round that completes before the late transcript is held with the
// tool-call turn-end: the tool result follows the call it answers, and the
// reply after the round answers the same user turn (no placeholder for it).
func TestTranscriptReorderStage_ToolRoundHeldBehindLateTranscript(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	in <- txtElem("Let me check.")
	in <- toolCallEndElem("c1") // held: no transcript yet
	in <- toolResultElem("c1")  // the round's result, before the transcript
	in <- txtElem("Shipped.")   // the reply to the tool result
	in <- userElem("where is my order")
	in <- txtElem(" Today.") // after the user turn: live
	in <- asstEndElem("Shipped. Today.")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{
		"USER:where is my order", "TXT:Let me check.", "END:calls", "TOOL:c1", "TXT:Shipped.", "TXT: Today.", "END",
	}, drainOrder(out))
}

// The transcript arrives after the whole tool turn: both turn-ends and the
// tool result are held behind it, in order.
func TestTranscriptReorderStage_TranscriptAfterWholeToolTurn(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	in <- toolCallEndElem("c1")
	in <- toolResultElem("c1")
	in <- txtElem("Shipped.")
	in <- asstEndElem("Shipped.")
	in <- userElem("where is my order")
	in <- txtElem("Next.") // the next turn
	in <- userElem("thanks")
	in <- asstEndElem("Next.")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{
		"USER:where is my order", "END:calls", "TOOL:c1", "TXT:Shipped.", "END",
		"USER:thanks", "TXT:Next.", "END",
	}, drainOrder(out))
}

// With the user turn already shown, a tool-call turn-end does not end it:
// the reply after the round streams live instead of waiting for (and then
// getting a placeholder for) a transcript of its own.
func TestTranscriptReorderStage_ToolTurnAfterUserStreamsLive(t *testing.T) {
	s := NewTranscriptReorderStageWithTimeout("[none]", time.Hour)
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	go func() { _ = s.Process(context.Background(), in, out) }()
	in <- userElem("where is my order")
	in <- toolCallEndElem("c1")
	in <- toolResultElem("c1")
	in <- txtElem("Shipped.")
	in <- asstEndElem("Shipped.")
	got := collectUntil(out, 2, 2*time.Second)
	close(in)
	assert.Equal(t, []string{"USER:where is my order", "END", "TOOL:c1", "TXT:Shipped.", "END"}, got,
		"the reply to a tool round must not be held for a transcript of its own")
}

// When the hold times out on a tool-call turn-end, the placeholder stands in
// for the transcript and the rest of the turn streams live.
func TestTranscriptReorderStage_ToolTurnHoldTimeout(t *testing.T) {
	t.Parallel()
	s := NewTranscriptReorderStageWithTimeout("[none]", 40*time.Millisecond)
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	go func() { _ = s.Process(context.Background(), in, out) }()
	in <- toolCallEndElem("c1")
	got := collectUntil(out, 1, 2*time.Second)
	in <- toolResultElem("c1")
	in <- txtElem("Shipped.")
	in <- asstEndElem("Shipped.")
	got = append(got, collectUntil(out, 1, 2*time.Second)...)
	close(in)
	assert.Equal(t, []string{"USER:[none]", "END", "TOOL:c1", "TXT:Shipped.", "END"}, got)
}

// An interrupted response with tool calls ends the turn: the next turn's text
// resolves it with the placeholder.
func TestTranscriptReorderStage_InterruptedToolTurnEndsTurn(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	cut := toolCallEndElem("c1")
	cut.Meta.Interrupted = true
	in <- cut
	in <- txtElem("A2")
	in <- userElem("q2")
	in <- asstEndElem("A2")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "END:calls", "USER:q2", "TXT:A2", "END"}, drainOrder(out))
}

// Two finished turns with no transcript and no text between them (an
// audio-only reply) each keep their turn-end: the second resolves the first.
func TestTranscriptReorderStage_ConsecutiveTurnEndsBothKept(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)

	in <- asstEndElem("A1")
	in <- asstEndElem("A2")
	in <- userElem("q2")
	close(in)

	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "END", "USER:q2", "END"}, drainOrder(out))
}

// A transcript marked late for a turn the stage already resolved (its hold
// timed out and the placeholder was emitted) is dropped: it is that turn's,
// not the next turn's user message.
func TestTranscriptReorderStage_LateTranscriptAfterTimeoutDropped(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- txtElem("A1")
	r.in <- asstEndElem("A1")
	time.Sleep(80 * time.Millisecond) // the hold times out
	r.in <- lateUserElem("q1")        // turn 1's transcript, too late
	r.in <- txtElem("A2")
	r.in <- userElem("q2")
	r.in <- asstEndElem("A2")
	assert.Equal(t, []string{"USER:[none]", "TXT:A1", "END", "USER:q2", "TXT:A2", "END"}, r.finish())
}

// A held turn is not timed out while the tool round it started is still
// running (a slow tool): its transcript, arriving after the round, still
// takes its place ahead of the reply.
func TestTranscriptReorderStage_HoldPausedWhileToolRoundRuns(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- runningToolCallEndElem("c1")
	time.Sleep(100 * time.Millisecond) // the tool takes five hold windows
	r.in <- toolResultElem("c1")
	r.in <- lateUserElem("where is my order")
	r.in <- txtElem("Shipped.")
	r.in <- asstEndElem("Shipped.")
	assert.Equal(t, []string{"USER:where is my order", "END:calls", "TOOL:c1", "TXT:Shipped.", "END"}, r.finish())
}

// Once the round's results are in, the hold window runs again.
func TestTranscriptReorderStage_HoldResumesAfterToolRound(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- runningToolCallEndElem("c1")
	r.in <- toolResultElem("c1")
	time.Sleep(80 * time.Millisecond)
	r.in <- lateUserElem("too late") // the hold timed out after the round
	r.in <- txtElem("Shipped.")
	r.in <- asstEndElem("Shipped.")
	assert.Equal(t, []string{"USER:[none]", "END:calls", "TOOL:c1", "TXT:Shipped.", "END"}, r.finish())
}

// When a held tool turn does time out, its transcript arriving later is
// dropped rather than emitted in the middle of the turn.
func TestTranscriptReorderStage_LateTranscriptAfterToolTurnTimeoutDropped(t *testing.T) {
	r := startReorder(NewTranscriptReorderStageWithTimeout("[none]", 20*time.Millisecond))
	r.in <- toolCallEndElem("c1") // the host runs this call: no pause
	time.Sleep(80 * time.Millisecond)
	r.in <- toolResultElem("c1")
	r.in <- lateUserElem("where is my order")
	r.in <- txtElem("Shipped.")
	r.in <- asstEndElem("Shipped.")
	assert.Equal(t, []string{"USER:[none]", "END:calls", "TOOL:c1", "TXT:Shipped.", "END"}, r.finish())
}

// A tool round that never gets its results (pending, rejected, abandoned)
// does not swallow the next turn: the next reply closes the tool turn, so
// that reply is still reordered behind its own transcript.
func TestTranscriptReorderStage_UnansweredToolTurnClosedByNextReply(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)
	in <- userElem("q1")
	in <- runningToolCallEndElem("c1") // no result ever comes
	in <- txtElem("A2")                // the next turn's reply
	in <- userElem("q2")               // its transcript, late
	in <- asstEndElem("A2")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:q1", "END:calls", "USER:q2", "TXT:A2", "END"}, drainOrder(out))
}

// The same when the unanswered tool turn is still held for its transcript:
// it is resolved with the placeholder, then the next turn is reordered.
func TestTranscriptReorderStage_HeldUnansweredToolTurnClosedByNextReply(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)
	in <- runningToolCallEndElem("c1")
	in <- txtElem("A2")
	in <- userElem("q2")
	in <- asstEndElem("A2")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "END:calls", "USER:q2", "TXT:A2", "END"}, drainOrder(out))
}

// A held tool round's other elements (a call held for the caller, an error)
// stay with the round, after its call and results.
func TestTranscriptReorderStage_HeldToolRoundKeepsItsOtherElements(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)
	in <- runningToolCallEndElem("c1")
	in <- toolResultElem("c1")
	pending := StreamElement{}
	pending.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "c2", ToolName: "approve"}}
	in <- pending
	in <- StreamElement{Error: errors.New("tool limit")}
	in <- lateUserElem("where is my order")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:where is my order", "END:calls", "TOOL:c1", "PENDING:c2", "ERR"}, drainOrder(out))
}

// A message for a later turn resolves a held finished turn first instead of
// joining it: a non-user message, and a typed user turn.
func TestTranscriptReorderStage_LaterTurnMessageResolvesHeldTurn(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 16)
	out := make(chan StreamElement, 32)
	in <- txtElem("A1")
	in <- asstEndElem("A1") // held for its transcript
	in <- StreamElement{Message: &types.Message{Role: "system", Content: "note"}}
	in <- txtElem("A2")
	in <- asstEndElem("A2") // held
	typed := userElem("typed")
	typed.Source = TextInputTurnSource
	in <- typed
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{
		"USER:[none]", "TXT:A1", "END", "SYSTEM:note",
		"USER:[none]", "TXT:A2", "END", "USER:typed",
	}, drainOrder(out))
}

// A typed user turn arriving while reply text is buffered (no turn-end held)
// keeps that text: it is the typed turn's reply, released at its turn-end.
func TestTranscriptReorderStage_TypedTurnKeepsBufferedText(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)
	in <- txtElem("A")
	typed := userElem("typed")
	typed.Source = TextInputTurnSource
	in <- typed
	in <- asstEndElem("A")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:typed", "TXT:A", "END"}, drainOrder(out))
}

// A tool result for a later round resolves a held finished turn before it
// passes, so a transcript after it is not taken for the finished turn.
func TestTranscriptReorderStage_ToolResultResolvesHeldFinishedTurn(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)
	in <- txtElem("A1")
	in <- asstEndElem("A1")
	in <- toolResultElem("c9")
	in <- userElem("q")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "TXT:A1", "END", "TOOL:c9", "USER:q"}, drainOrder(out))
}

// A control element (calls held for the caller) passes a held finished turn
// at once: only a held tool round keeps such elements.
func TestTranscriptReorderStage_PendingPassesHeldFinishedTurn(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)
	in <- txtElem("A1")
	in <- asstEndElem("A1")
	pending := StreamElement{}
	pending.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "x", ToolName: "approve"}}
	in <- pending
	in <- userElem("q")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"PENDING:x", "USER:q", "TXT:A1", "END"}, drainOrder(out))
}

// An interrupted response that had made tool calls finished its turn: its
// round's result resolves it, and a late transcript afterwards is dropped.
func TestTranscriptReorderStage_InterruptedToolCallEndIsFinished(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)
	cut := toolCallEndElem("c1")
	cut.Meta.Interrupted = true
	in <- cut
	in <- toolResultElem("c1")
	in <- lateUserElem("q1")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	assert.Equal(t, []string{"USER:[none]", "END:calls", "TOOL:c1"}, drainOrder(out))
}
