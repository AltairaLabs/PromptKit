package providers

import (
	"context"

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
type OutOfBandSpeaker interface {
	SpeakOutOfBand(ctx context.Context, text string) (OutOfBandSpeech, error)
}

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
