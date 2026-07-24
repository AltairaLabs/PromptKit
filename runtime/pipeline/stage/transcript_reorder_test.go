package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
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
		case e.Audio != nil:
			order = append(order, "AUDIO")
		case e.Text != nil:
			order = append(order, "TXT:"+*e.Text)
		case e.EndOfStream:
			order = append(order, "END")
		}
	}
	return order
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
