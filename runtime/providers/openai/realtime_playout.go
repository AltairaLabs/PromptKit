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
// caller (ReleaseAudio), not as it arrives.
func (s *RealtimeSession) HoldPlayout() {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	s.playoutHeld = true
}

// ReleaseAudio implements providers.PlayoutHolder: chunk's audio, as the
// session delivered it, reaches the caller now, so it plays from now on,
// unless the caller already spoke over the held audio.
func (s *RealtimeSession) ReleaseAudio(chunk *providers.StreamChunk) bool {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	if chunk == nil {
		return !s.interruptedFor(0)
	}
	if s.interruptedFor(chunk.Generation) {
		return false
	}
	if chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return true
	}
	itemID, _ := chunk.Metadata[keyItemID].(string)
	contentIndex, _ := chunk.Metadata[keyContentIndex].(int)
	media := chunk.MediaData
	d := audio.PCM16Duration(len(media.Data), media.SampleRate, media.Channels)
	s.playoutEstimate().Add(itemID, contentIndex, d)
	s.holdItemAudio(itemID, contentIndex, d, true)
	return true
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
	key heldItemKey
	// gen is the response that delivered it.
	gen                 int64
	delivered, released time.Duration
	// cut is set once the model's copy of the item was cut to what was
	// heard of it, so it is not cut again.
	cut bool
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
		s.heldItems = append(s.heldItems, heldItem{key: key, gen: s.gen, delivered: d})
	}
}

// interruptHeldAudio handles the caller starting to speak with no reply
// generating or playing: if audio the pipeline holds is still waiting for
// them, their speech is over it, so it reports true, marks the held audio
// interrupted (HeldAudioInterrupted) and cuts the model's copy of each held
// item to what was released of it. Called with recvMu held.
func (s *RealtimeSession) interruptHeldAudio() bool {
	waiting := s.markHeldInterrupted()
	s.cutHeldItems(nil)
	return waiting
}

// markHeldInterrupted marks each response whose held audio still waits for
// the caller interrupted (HeldAudioInterrupted), and reports whether there
// was one. Called with recvMu held.
func (s *RealtimeSession) markHeldInterrupted() bool {
	found := false
	for _, it := range s.heldItems {
		if !it.cut && it.delivered > it.released {
			if s.heldInterrupted == nil {
				s.heldInterrupted = map[int64]bool{}
			}
			s.heldInterrupted[it.gen] = true
			found = true
		}
	}
	return found
}

// interruptedFor reports whether the caller spoke over held audio of
// response generation, or of any response for 0. Called with recvMu held.
func (s *RealtimeSession) interruptedFor(generation int64) bool {
	if generation == 0 {
		return len(s.heldInterrupted) > 0
	}
	return s.heldInterrupted[generation]
}

// cutHeldItems cuts the model's copy of each held item some of whose audio
// was never released, and not cut already (cut, the items the playout
// estimate cut to what was heard), to what was released of it: the caller
// never heard the rest. Reports whether there was one. Called with recvMu
// held.
func (s *RealtimeSession) cutHeldItems(cut map[heldItemKey]bool) bool {
	found := false
	for i := range s.heldItems {
		it := &s.heldItems[i]
		if cut[it.key] {
			it.cut = true // to what was heard of it
		}
		if it.cut || it.delivered <= it.released {
			continue
		}
		found, it.cut = true, true
		s.sendTruncate(it.key.itemID, it.key.contentIndex, it.released)
	}
	return found
}

// HeldAudioInterrupted implements providers.PlayoutHolder: whether the caller
// began speaking over audio of response generation (any, for 0) the pipeline
// still held, since it dropped what it held of it.
func (s *RealtimeSession) HeldAudioInterrupted(generation int64) bool {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	return s.interruptedFor(generation)
}

// HeldAudioDropped implements providers.PlayoutHolder: nothing of a reply is
// held any longer, so none of it waits for the caller.
func (s *RealtimeSession) HeldAudioDropped(generation int64) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	// What was never released of the dropped response was never heard: its
	// copy is cut to what was, now, or, while it is still in progress, once
	// it is canceled (InterruptResponse) or ends and is dropped again.
	// Other responses' audio, a newer one's above all, is left as it is.
	kept := s.heldItems[:0]
	for _, it := range s.heldItems {
		if generation != 0 && it.gen != generation || s.responseActive && it.gen == s.gen {
			kept = append(kept, it)
			continue
		}
		if !it.cut && it.delivered > it.released {
			s.sendTruncate(it.key.itemID, it.key.contentIndex, it.released)
		}
	}
	s.heldItems = kept
	if generation == 0 {
		s.heldInterrupted = nil
	} else {
		delete(s.heldInterrupted, generation)
	}
}
