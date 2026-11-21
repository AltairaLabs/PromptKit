package openai

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// maxPendingTruncates bounds the truncate events awaiting the server's
// answer; past it the oldest is forgotten (its rejection, if one still
// came, would then surface as an ordinary server error).
const maxPendingTruncates = 32

// truncateTracker remembers the conversation.item.truncate events sent and
// not yet answered. An error names the event it rejects by its ID. The
// server's conversation.item.truncated does not echo the event ID, only the
// item, so it acknowledges the item's oldest pending truncate (the server
// answers an item's truncates in order). Guarded by the session's recvMu.
type truncateTracker struct {
	pending []pendingTruncate
}

type pendingTruncate struct {
	eventID, itemID string
}

// sent records a truncate event for itemID.
func (t *truncateTracker) sent(eventID, itemID string) {
	if len(t.pending) == maxPendingTruncates {
		t.pending = t.pending[1:]
	}
	t.pending = append(t.pending, pendingTruncate{eventID: eventID, itemID: itemID})
}

// done forgets itemID's oldest pending truncate, which the server
// acknowledged.
func (t *truncateTracker) done(itemID string) {
	t.remove(func(p pendingTruncate) bool { return p.itemID == itemID })
}

// rejected reports whether eventID is a pending truncate, forgetting it.
func (t *truncateTracker) rejected(eventID string) bool {
	return eventID != "" && t.remove(func(p pendingTruncate) bool { return p.eventID == eventID })
}

// remove drops the first pending truncate match accepts, and reports
// whether there was one.
func (t *truncateTracker) remove(match func(pendingTruncate) bool) bool {
	for i, p := range t.pending {
		if match(p) {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return true
		}
	}
	return false
}

// len is the number of truncates awaiting an answer.
func (t *truncateTracker) len() int { return len(t.pending) }

var _ providers.PlayoutHolder = (*RealtimeSession)(nil)

// HoldPlayout implements providers.PlayoutHolder: from now on a reply's audio
// counts toward the playout estimate only as the pipeline releases it to the
// caller (AudioReleased), not as it arrives.
func (s *RealtimeSession) HoldPlayout() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.playoutHeld = true
}

// AudioReleased implements providers.PlayoutHolder: chunk's audio, as the
// session delivered it, reached the caller now, so it plays from now on.
func (s *RealtimeSession) AudioReleased(chunk *providers.StreamChunk) {
	if chunk == nil || chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return
	}
	itemID, _ := chunk.Metadata[keyItemID].(string)
	contentIndex, _ := chunk.Metadata[keyContentIndex].(int)
	media := chunk.MediaData
	d := audio.PCM16Duration(len(media.Data), media.SampleRate, media.Channels)
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.playoutEstimate().Add(itemID, contentIndex, d)
	s.holdItemAudio(itemID, contentIndex, d, true)
}

// countDeliveredAudio counts audio for item (itemID, contentIndex) toward
// the playout estimate as it is delivered, unless the pipeline holds it back
// (HoldPlayout). Called with recvMu held.
func (s *RealtimeSession) countDeliveredAudio(itemID string, contentIndex int, d time.Duration) {
	if s.playoutHeld {
		s.holdItemAudio(itemID, contentIndex, d, false)
		return
	}
	s.playoutEstimate().Add(itemID, contentIndex, d)
}

// heldItem is reply audio of one item the session delivered while the
// pipeline holds playout back: how much it delivered and how much the
// pipeline has released.
type heldItem struct {
	key                 heldItemKey
	delivered, released time.Duration
}

// heldItemKey names an audio item's content part.
type heldItemKey struct {
	itemID       string
	contentIndex int
}

// holdItemAudio records d of item (itemID, contentIndex) as delivered and
// held (released false) or released. Called with recvMu held.
func (s *RealtimeSession) holdItemAudio(itemID string, contentIndex int, d time.Duration, released bool) {
	key := heldItemKey{itemID: itemID, contentIndex: contentIndex}
	for i := range s.heldItems {
		if it := &s.heldItems[i]; it.key == key {
			if released {
				it.released += d
			} else {
				it.delivered += d
			}
			return
		}
	}
	if !released {
		s.heldItems = append(s.heldItems, heldItem{key: key, delivered: d})
	}
}

// interruptHeldAudio handles the caller starting to speak with no reply
// generating or playing: if audio the pipeline holds is still waiting for
// them, their speech is over it, so it reports true, marks the held audio
// interrupted (HeldAudioInterrupted) and cuts the model's copy of each held
// item to what was released of it. Called with recvMu held.
func (s *RealtimeSession) interruptHeldAudio() bool {
	var waiting []heldItem
	for _, it := range s.heldItems {
		if it.delivered > it.released {
			waiting = append(waiting, it)
		}
	}
	if len(waiting) == 0 {
		return false
	}
	s.heldInterrupted = true
	for _, it := range waiting {
		s.sendTruncate(it.key.itemID, it.key.contentIndex, it.released)
	}
	s.heldItems = nil
	return true
}

// HeldAudioInterrupted implements providers.PlayoutHolder: whether the caller
// began speaking over audio the pipeline still held, since it last dropped
// what it held.
func (s *RealtimeSession) HeldAudioInterrupted() bool {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	return s.heldInterrupted
}

// HeldAudioDropped implements providers.PlayoutHolder: nothing of a reply is
// held any longer, so none of it waits for the caller.
func (s *RealtimeSession) HeldAudioDropped() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.heldItems = nil
	s.heldInterrupted = false
}
