package stage

import (
	"context"
	"testing"
	"time"

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
		case e.Message != nil && e.Message.Role == "tool":
			order = append(order, "TOOL:"+e.Message.ToolResult.ID)
		case e.Audio != nil:
			order = append(order, "AUDIO")
		case e.Text != nil:
			order = append(order, "TXT:"+*e.Text)
		case e.EndOfStream && e.Message != nil && len(e.Message.ToolCalls) > 0:
			order = append(order, "END:calls")
		case e.EndOfStream:
			order = append(order, "END")
		}
	}
	return order
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
			switch {
			case e.Message != nil && e.Message.Role == "user":
				order = append(order, "USER:"+e.Message.Content)
			case e.Message != nil && e.Message.Role == "tool":
				order = append(order, "TOOL:"+e.Message.ToolResult.ID)
			case e.Audio != nil:
				order = append(order, "AUDIO")
			case e.Text != nil:
				order = append(order, "TXT:"+*e.Text)
			case e.EndOfStream:
				order = append(order, "END")
				ends++
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
