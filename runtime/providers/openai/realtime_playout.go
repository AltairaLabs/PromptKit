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
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.playoutEstimate().Add(itemID, contentIndex,
		audio.PCM16Duration(len(media.Data), media.SampleRate, media.Channels))
}

// countDeliveredAudio counts audio for item (itemID, contentIndex) toward
// the playout estimate as it is delivered, unless the pipeline holds it back
// (HoldPlayout). Called with recvMu held.
func (s *RealtimeSession) countDeliveredAudio(itemID string, contentIndex int, d time.Duration) {
	if s.playoutHeld {
		return
	}
	s.playoutEstimate().Add(itemID, contentIndex, d)
}
