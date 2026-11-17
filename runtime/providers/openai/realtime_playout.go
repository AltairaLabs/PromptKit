package openai

import "time"

// realtimePlayout estimates how far the caller has got through the assistant
// audio the session delivered. The realtime API sends a reply's audio faster
// than real time, so most of a long reply is still playing after
// response.done: speech then must still count as barge-in, and the model's
// copy of the reply must be cut to what was heard.
//
// The estimate assumes the player starts each chunk as it arrives and plays
// it in real time (the same assumption the cascaded pipeline's interruption
// handler makes about synthesized audio): each chunk extends the playout end
// by its duration, from now or from the current end, whichever is later.
// Receive goroutine only.
type realtimePlayout struct {
	// until is when the audio delivered so far finishes playing.
	until time.Time
	// itemID and contentIndex name the assistant audio item delivered last;
	// itemAudio is how much of its audio was delivered.
	itemID       string
	contentIndex int
	itemAudio    time.Duration
}

// pcm16Duration is the duration of n bytes of mono PCM16 at rate Hz.
func pcm16Duration(n, rate int) time.Duration {
	if rate <= 0 {
		return 0
	}
	return time.Duration(n/2) * time.Second / time.Duration(rate)
}

// add records d of audio for the item delivered at now.
func (p *realtimePlayout) add(now time.Time, itemID string, contentIndex int, d time.Duration) {
	if itemID != p.itemID || contentIndex != p.contentIndex {
		p.itemID, p.contentIndex, p.itemAudio = itemID, contentIndex, 0
	}
	p.itemAudio += d
	start := p.until
	if now.After(start) {
		start = now
	}
	p.until = start.Add(d)
}

// playing reports whether delivered audio is still playing at now.
func (p *realtimePlayout) playing(now time.Time) bool {
	return now.Before(p.until)
}

// stop ends the playout at now. It returns the item delivered last and how
// much of it the caller heard, and whether some of it was not heard (the
// item needs truncating).
func (p *realtimePlayout) stop(now time.Time) (itemID string, contentIndex int, heard time.Duration, cut bool) {
	itemID, contentIndex, heard = p.itemID, p.contentIndex, p.itemAudio
	if rest := p.until.Sub(now); rest > 0 {
		heard -= rest
		cut = itemID != ""
	}
	*p = realtimePlayout{}
	return itemID, contentIndex, max(heard, 0), cut
}
