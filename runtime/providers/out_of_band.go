package providers

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// OutOfBandSpeaker is an optional StreamInputSession interface for a session
// that can speak text in its own voice outside the conversation: the model's
// context never holds it, and it is not a reply on Response(). The pipeline
// renders a cue's clip with it (a guardrail's message), so the clip sounds
// exactly like the session.
//
// The model renders the speech, so it can say more than it was asked to:
// Transcript is what it said, for the caller to check against text. The
// render is billed: Cost is what it cost, also when the transcript does not
// match.
//
// OpenAI Realtime implements it with a response outside the conversation
// (response.conversation "none"). Gemini Live has no equivalent.
//
// When ctx ends first, the render is canceled and SpeakOutOfBand returns
// ctx's error; it may wait briefly past ctx for the canceled response's end,
// so that what it cost is still returned.
type OutOfBandSpeaker interface {
	SpeakOutOfBand(ctx context.Context, text string) (OutOfBandSpeech, error)
}

// ErrOutOfBandSessionClosed is the error (errors.Is) of a render that failed
// because its session closed: the text is not at fault, and another session
// may render it.
var ErrOutOfBandSessionClosed = errors.New("the session closed before it finished speaking")

// OutOfBandSpeech is what an OutOfBandSpeaker rendered.
type OutOfBandSpeech struct {
	// Audio is PCM16 mono at SampleRate.
	Audio      []byte
	SampleRate int
	// Transcript is what the model said.
	Transcript string
	// Cost is what the render cost, nil when it is not known.
	Cost *types.CostInfo
}
