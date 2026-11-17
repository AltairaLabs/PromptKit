package gemini

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Gemini Live sends serverContent.interrupted only while the model is
// generating. A reply's audio can arrive faster than it plays; once the turn
// is complete there is nothing for the server to interrupt and it sends no
// speech-start event, so the first sign of the caller speaking over the
// still-playing reply is the transcript of their speech.
//
// That transcript must be of a new utterance. The caller's earlier speech can
// still be transcribed after the reply begins, so fragments continue that
// utterance while it was being transcribed when the reply began (or during
// generation) and they keep coming within inputSegmentGap of each other,
// unless Gemini marked its transcript finished.
const (
	// inputLateWindow is how recent a transcript fragment must be, when the
	// reply begins, for the caller's utterance to count as still being
	// transcribed.
	inputLateWindow = 1500 * time.Millisecond
	// inputSegmentGap is the pause between transcript fragments that starts
	// a new utterance.
	inputSegmentGap = time.Second
)

// clock returns the session's current time.
func (s *StreamSession) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// playoutEstimate returns the session's playout estimate, creating it on the
// session's clock on first use.
func (s *StreamSession) playoutEstimate() *audio.Playout {
	if s.playout == nil {
		s.playout = audio.NewPlayout(s.now)
	}
	return s.playout
}

// replyStarted records model output: the model is answering, and on its first
// output of a turn, whether the caller's utterance was still being transcribed.
func (s *StreamSession) replyStarted() {
	s.replyDone = false
	if s.turnOpen {
		return
	}
	s.turnOpen = true
	s.gen++ // a new generation: the reply
	s.lateInput = !s.inputClosed && !s.inputLastAt.IsZero() && s.clock().Sub(s.inputLastAt) < inputLateWindow
}

// turnEnded records the model's turn completing.
func (s *StreamSession) turnEnded() {
	s.replyDone = true
	s.turnOpen = false
}

// bargeInAfterReply handles the caller's transcribed speech: a new utterance
// while a reply that finished generating still plays is barge-in. The queued
// audio is dropped and the interruption reported, as for interrupted; nothing
// of the finished reply is still to arrive, so the audio drop ends at once and
// the reply to the caller's speech plays.
func (s *StreamSession) bargeInAfterReply(content *ServerContent) error {
	tx := content.InputTranscription
	if tx == nil || tx.Text == "" {
		return nil
	}
	now := s.clock()
	continues := s.lateInput && !s.inputClosed && now.Sub(s.inputLastAt) < inputSegmentGap
	s.inputLastAt, s.inputClosed = now, tx.Finished
	if !s.replyDone {
		// The model is generating (the server reports barge-in itself), or has
		// not answered yet: a fragment now is the caller's earlier speech.
		s.lateInput = s.lateInput || s.turnOpen
		return nil
	}
	if continues {
		return nil
	}
	s.lateInput = false
	if !s.playoutEstimate().Playing() {
		return nil
	}
	logger.Debug("StreamSession: barge-in while the finished reply is still playing")
	s.playoutEstimate().Stop()
	s.Barge()
	marker := providers.BargeMarker()
	err := s.sendChunk(&marker)
	s.ClearDrop()
	if err != nil {
		return err
	}
	return s.sendChunk(&providers.StreamChunk{Interrupted: true})
}
