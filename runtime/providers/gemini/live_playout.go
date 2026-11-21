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
	s.withPlayout(func(p *audio.Playout) { p.NewResponse() })
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
	playing := false
	s.withPlayout(func(p *audio.Playout) {
		if playing = p.Playing(); playing {
			p.Stop()
		} else if s.heldPending > 0 {
			// Audio the pipeline still holds waits for the caller, though
			// they hear nothing of it yet: speech now is over it.
			playing, s.heldInterrupted, s.heldPending = true, true, 0
		}
	})
	if !playing {
		return nil
	}
	logger.Debug("StreamSession: barge-in while the finished reply is still playing")
	s.Barge()
	marker := providers.BargeMarker()
	err := s.sendChunk(&marker)
	s.ClearDrop()
	if err != nil {
		return err
	}
	return s.sendChunk(&providers.StreamChunk{Interrupted: true})
}

// withPlayout runs fn on the playout estimate under playoutMu.
func (s *StreamSession) withPlayout(fn func(*audio.Playout)) {
	s.playoutMu.Lock()
	defer s.playoutMu.Unlock()
	fn(s.playoutEstimate())
}

// countDeliveredAudio counts d of reply audio toward the playout estimate as
// it is delivered, unless the pipeline holds reply audio back (HoldPlayout).
func (s *StreamSession) countDeliveredAudio(d time.Duration) {
	s.withPlayout(func(p *audio.Playout) {
		if s.playoutHeld {
			s.heldPending += d
			return
		}
		p.Add("", 0, d)
	})
}

var (
	_ providers.PlayoutHolder    = (*StreamSession)(nil)
	_ providers.TranscriptLagger = (*StreamSession)(nil)
)

// HoldPlayout implements providers.PlayoutHolder: from now on a reply's audio
// counts toward the playout estimate only as the pipeline releases it to the
// caller (AudioReleased), not as it arrives.
func (s *StreamSession) HoldPlayout() {
	s.playoutMu.Lock()
	defer s.playoutMu.Unlock()
	s.playoutHeld = true
}

// AudioReleased implements providers.PlayoutHolder: chunk's audio reached the
// caller now, so it plays from now on.
func (s *StreamSession) AudioReleased(chunk *providers.StreamChunk) {
	if chunk == nil || chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return
	}
	m := chunk.MediaData
	d := audio.PCM16Duration(len(m.Data), m.SampleRate, m.Channels)
	s.withPlayout(func(p *audio.Playout) {
		p.Add("", 0, d)
		s.heldPending = max(0, s.heldPending-d)
	})
}

// outputTranscriptLag is the most reply audio Gemini Live's output transcript
// was measured to trail (1.34 s over 96 sentences), rounded up.
const outputTranscriptLag = 1500 * time.Millisecond

// OutputTranscriptLag implements providers.TranscriptLagger: Gemini Live's
// output transcript usually leads its audio, but has been seen to trail it by
// up to outputTranscriptLag.
func (s *StreamSession) OutputTranscriptLag() time.Duration { return outputTranscriptLag }

// HeldAudioInterrupted implements providers.PlayoutHolder: whether the caller
// began speaking over audio the pipeline still held, since it last dropped
// what it held.
func (s *StreamSession) HeldAudioInterrupted() bool {
	s.playoutMu.Lock()
	defer s.playoutMu.Unlock()
	return s.heldInterrupted
}

// HeldAudioDropped implements providers.PlayoutHolder: nothing of a reply is
// held any longer, so none of it waits for the caller.
func (s *StreamSession) HeldAudioDropped() {
	s.playoutMu.Lock()
	defer s.playoutMu.Unlock()
	s.heldPending = 0
	s.heldInterrupted = false
}
