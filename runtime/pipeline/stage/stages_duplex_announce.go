package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// announceState is where a spoken blocked message stands. After a guardrail
// cuts a spoken reply with a message of its own, the session is asked to
// speak that message (providers.Announcer), so the caller hears why the reply
// stopped instead of silence.
//
// The message is asked for once the blocked reply has ended (announcePending
// until then): asking earlier would start a new reply under the one being
// canceled. A session that cannot stop a reply (Gemini Live) is asked at once,
// which is what ends the blocked reply. The first reply after the blocked one
// is the spoken message (announceActive): its audio is played and its text is
// checked by the output guardrails, but it is not a turn of its own. Its text
// and Message are dropped (the blocked turn already carries the text), and it
// does not count as an assistant turn.
type announceState int

const (
	announceNone announceState = iota
	announcePending
	announceActive
)

// duplexAnnouncement is the spoken blocked message's state (response
// goroutine only).
type duplexAnnouncement struct {
	state announceState
	// text is the message to speak, and asked whether the session was asked.
	text  string
	asked bool
	// after is the generation of the blocked reply; the first reply after it
	// is the message, gen once it has started (0 for a session that does not
	// number its replies).
	after int64
	gen   int64
	// owesTurnEnd is set when the blocked reply never completed (the provider
	// abandoned it), so the message's end closes its turn.
	owesTurnEnd bool
}

// queueAnnouncement has the blocked message text spoken, once per block: a
// session that can stop the blocked reply is asked once it has ended
// (advanceAnnouncement), one that cannot is asked now. Not on the scenario
// path, whose turns are counted by their own ids.
func (s *DuplexProviderStage) queueAnnouncement(ctx context.Context, text string) {
	if _, ok := s.session.(providers.Announcer); !ok || text == "" ||
		s.announce.state != announceNone || s.hasQueuedTurnID() {
		return
	}
	s.announce = duplexAnnouncement{state: announcePending, text: text}
	if _, canStop := s.session.(providers.ResponseInterrupter); !canStop {
		s.askAnnouncement(ctx)
	}
}

// askAnnouncement asks the session to speak the message. A session that
// cannot, whose replies are not spoken, or that fails to, leaves the caller
// with the text alone.
func (s *DuplexProviderStage) askAnnouncement(ctx context.Context) {
	s.announce.asked = true
	spoken, err := s.session.(providers.Announcer).Announce(ctx, s.announce.text)
	if err != nil {
		logger.Warn("DuplexProviderStage: the provider did not speak the blocked message", "error", err)
	}
	if err != nil || !spoken {
		s.announce = duplexAnnouncement{}
	}
}

// takeAnnouncementChunk reports whether chunk is part of the spoken blocked
// message: a chunk the session marked as the message's (AnnouncementTagger),
// or, from a session that marks none, a reply chunk of the first generation
// after the blocked reply's.
func (s *DuplexProviderStage) takeAnnouncementChunk(chunk *providers.StreamChunk) bool {
	a := &s.announce
	if !s.chunkIsReply || a.state != announceActive {
		return false
	}
	if tagger, ok := s.session.(providers.AnnouncementTagger); ok && tagger.TagsAnnouncements() {
		marked, _ := chunk.Metadata[providers.MetaKeyAnnouncement].(bool)
		if marked && a.gen == 0 {
			a.gen = chunk.Generation
		}
		return marked
	}
	if chunk.Generation == 0 {
		return true
	}
	if a.gen == 0 && chunk.Generation > a.after {
		a.gen = chunk.Generation
	}
	return chunk.Generation == a.gen
}

// guardReplyChunk runs the streaming output guardrails on a reply chunk, and
// then, for the spoken message, drops what it adds beyond its audio
// (stripAnnouncementChunk).
func (s *DuplexProviderStage) guardReplyChunk(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription, announcing bool,
) {
	s.guardOutputChunk(ctx, chunk, isOutputTranscription)
	if announcing {
		stripAnnouncementChunk(chunk, isOutputTranscription)
	}
}

// stripAnnouncementChunk drops what the spoken message's chunk adds beyond
// its audio, once the output guardrails have seen its text: the blocked turn
// already carries the text, and the message makes no tool calls.
func stripAnnouncementChunk(chunk *providers.StreamChunk, isOutputTranscription bool) {
	chunk.Content = ""
	if isOutputTranscription {
		chunk.Delta = ""
	}
	chunk.ToolCalls = nil
}

// advanceAnnouncement moves the spoken message on as replies end. When the
// blocked reply ends (completed, canceled, or abandoned by a provider that
// cannot stop it), the session is asked for the message if it was not yet,
// and the next reply is the message. When the message ends (or is
// interrupted), replies are turns again. The message's elements carry no
// Message and no EndOfStream, which a caller takes for an empty reply. A
// blocked reply the provider abandoned never completed, so the caller was not
// told its turn ended: the end of the message tells it, an EndOfTurn marked
// safety.
func (s *DuplexProviderStage) advanceAnnouncement(
	ctx context.Context, elem *StreamElement, announcing, turnDone bool,
) {
	a := &s.announce
	ended := turnDone || elem.Meta.Interrupted
	switch {
	case announcing:
		elem.Message = nil
		elem.EndOfStream = false
		if ended {
			owes := a.owesTurnEnd
			s.announce = duplexAnnouncement{}
			if owes {
				fr := types.FinishReasonSafety
				elem.EndOfTurn = true
				elem.Meta.FinishReason = &fr
			}
		}
	case a.state == announcePending && ended:
		if !a.asked {
			s.askAnnouncement(ctx)
		}
		if a.state == announcePending {
			a.state = announceActive
			a.after = s.lastReplyGen
			a.owesTurnEnd = !turnDone
		}
	}
}

// endAnnouncementTurn ends the spoken message's reply without counting it as
// an assistant turn.
func (s *DuplexProviderStage) endAnnouncementTurn() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.replyStreaming = false
}
