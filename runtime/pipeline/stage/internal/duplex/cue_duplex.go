package duplex

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
)

// newCueStageFor creates a cue stage that renders its clips for duplex's
// session, and wires duplex to it: when duplex creates its session the stage
// learns the session's voice and output rate and starts rendering the
// configured texts and the messages of duplex's guardrails. Clips come from
// the first source that renders them: the session's own out-of-band audio
// (providers.OutOfBandSpeaker, OpenAI Realtime), in its exact voice and
// outside the conversation; then the TTS service; then a built-in earcon (a
// short tone). The session's audio is checked against the text by its
// transcript (case, punctuation and spacing aside) and asked for once more
// on a miss before the text falls to the next source. Its clips are cached
// under the session's provider family and voice, so only the first session
// of a family and voice renders them; a clip put in the cache for the TTS
// service (a pre-warmed one) is not played on such a session, since its
// own audio comes first. A text not known in advance renders when first
// cued; past OnDemandBudget the earcon plays in its place. A session whose
// replies are not spoken gets no cue audio; one that does not report its
// output audio (providers.OutputAudioReporter) plays its configured clips and
// renders as PCM16 at 24 kHz in the TTS service's default voice.
func newCueStageFor(duplex *DuplexProviderStage, cfg cue.Config) *cue.CueStage {
	s := cue.NewStage(cfg)
	duplex.cues = cue.BankOf(s)
	return s
}

// NewDuplexCueStage creates the stage that speaks cues for duplex's realtime
// session: a guardrail's message, when a guardrail cuts a spoken reply or
// blocks typed input. It goes directly after duplex in the pipeline, ahead of
// transcript reordering, playout tracking, recording and pacing, which treat
// its audio as reply audio (marked Meta.CueAudio). Every element goes on
// unchanged and in order; after one carrying a Cue the stage plays the cue's
// clip, PCM16 mono at the reply audio's rate, in chunks of 100ms.
//
// When duplex creates its session the stage learns the session's voice and
// output rate and starts rendering the messages of duplex's guardrails. Clips
// come from the first source that renders them: the session's own
// out-of-band audio (providers.OutOfBandSpeaker, OpenAI Realtime), in its
// exact voice and outside the conversation; then opts.Voice; then, with no
// Voice, the clips pre-warmed for the session's voice; then a built-in
// earcon (a short tone). The session's audio is checked against the text by
// its transcript (case, punctuation and spacing aside) and asked for once
// more on a miss before the text falls to the next source. Clips are kept in
// opts.Cache under their source and voice, so only the first session of a
// source and voice renders them.
//
// While a clip renders, reply output waits behind it for at most 1.5s; past
// that the earcon plays in its place, and the clip is kept for the next cue.
// A cut (barge-in) while it renders drops the cue. A session whose replies
// are not spoken gets no cue audio; one that does not report its output audio
// (providers.OutputAudioReporter) renders PCM16 at 24 kHz in the voice
// service's default voice.
func NewDuplexCueStage(duplex *DuplexProviderStage, opts cue.CueOptions) core.Stage {
	return newCueStageFor(duplex, cue.Config{TTS: opts.Voice, Cache: opts.Cache})
}

// DetachCues unwires s from the cue stage it plays its cues on, if any. The
// realtime chain calls it before it builds its cue slot, whose stage wires its
// own.
func DetachCues(s *DuplexProviderStage) {
	s.cues = nil
}

// startCues tells the cue stage after s, if any, of the session just created.
// The clips render on their own goroutine: the first reply never waits for
// them.
func (s *DuplexProviderStage) startCues(ctx context.Context) {
	if s.cues != nil {
		s.cues.SessionStarted(ctx, s.session, s.emitter, s.hookRegistry.BlockMessages())
	}
}

// isCueOnly reports whether elem's only payload is a Cue (a backchannel a
// mic-side stage asks for): it is for a cue stage, not for a provider session.
func isCueOnly(elem *core.StreamElement) bool {
	return elem.Cue != nil && !elem.HasContent() && !elem.IsControl()
}
