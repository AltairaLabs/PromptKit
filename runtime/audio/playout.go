package audio

import "time"

// bytesPerPCM16Sample is the size of one PCM16 sample.
const bytesPerPCM16Sample = 2

// PCM16Duration is the playing time of n bytes of PCM16 audio with the given
// sample rate and channel count (a channel count below 1 counts as mono). It
// is 0 for a non-positive sample rate.
func PCM16Duration(n, sampleRate, channels int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	if channels < 1 {
		channels = 1
	}
	samples := n / (bytesPerPCM16Sample * channels)
	return time.Duration(samples) * time.Second / time.Duration(sampleRate)
}

// Playout estimates how much of the audio handed to a real-time player has
// been heard. A realtime model sends a reply's audio faster than it plays, so
// most of a long reply is still playing after the model has finished it.
//
// It assumes the player starts each chunk as it arrives, or as soon as the
// audio ahead of it finishes, and plays it in real time: each chunk extends
// the playout end by its duration, from now or the current end, whichever is
// later (as InterruptionHandler.ExtendPlayback does for synthesized speech).
// Audio is tracked per item, so an interruption can say how much of each item
// the listener heard.
//
// A Playout is not safe for concurrent use.
type Playout struct {
	now   func() time.Time
	until time.Time
	// items are the stretches of audio not yet fully heard, in playout order.
	items []playoutItem
	// before is how much of each item played in its earlier stretches: audio
	// for an item that arrives after its earlier audio ran out (a stall)
	// starts a new stretch.
	before map[itemKey]time.Duration
}

type itemKey struct {
	id           string
	contentIndex int
}

type playoutItem struct {
	itemKey
	start time.Time
	dur   time.Duration
}

// PlayoutCut is an item the listener had not fully heard when the playout
// stopped, and how much of it they had heard.
type PlayoutCut struct {
	ItemID       string
	ContentIndex int
	Heard        time.Duration
}

// NewPlayout returns an empty Playout on the clock now (nil means time.Now).
func NewPlayout(now func() time.Time) *Playout {
	if now == nil {
		now = time.Now
	}
	return &Playout{now: now}
}

// Add records d of audio for the item (itemID, contentIndex) handed to the
// player now. Non-positive d is ignored.
func (p *Playout) Add(itemID string, contentIndex int, d time.Duration) {
	if d <= 0 {
		return
	}
	now := p.now()
	p.prune(now)
	start := p.until
	if now.After(start) {
		start = now
	}
	key := itemKey{id: itemID, contentIndex: contentIndex}
	if n := len(p.items); n > 0 {
		last := &p.items[n-1]
		if last.itemKey == key && last.start.Add(last.dur).Equal(start) {
			last.dur += d
			p.until = start.Add(d)
			return
		}
	}
	p.items = append(p.items, playoutItem{itemKey: key, start: start, dur: d})
	p.until = start.Add(d)
}

// Playing reports whether audio handed to the player is still playing.
func (p *Playout) Playing() bool {
	return p.now().Before(p.until)
}

// Stop ends the playout now and returns the items the listener had not fully
// heard, in playout order, each with how much of it they had heard (0 for an
// item that had not started playing).
func (p *Playout) Stop() []PlayoutCut {
	now := p.now()
	p.prune(now) // count the stretches heard by now into their items
	var cuts []PlayoutCut
	for _, it := range p.items {
		heard := min(max(now.Sub(it.start), 0), it.dur)
		if heard < it.dur {
			cuts = append(cuts, PlayoutCut{ItemID: it.id, ContentIndex: it.contentIndex,
				Heard: p.before[it.itemKey] + heard})
		}
	}
	p.items = nil
	p.before = nil
	p.until = time.Time{}
	return cuts
}

// prune drops the stretches fully heard by now, remembering how much of
// their item they played. It forgets that for items with no stretch left,
// except the one that played last (it may resume after a stall), so the
// record stays bounded.
func (p *Playout) prune(now time.Time) {
	i := 0
	var last itemKey
	for i < len(p.items) && !p.items[i].start.Add(p.items[i].dur).After(now) {
		if p.before == nil {
			p.before = map[itemKey]time.Duration{}
		}
		last = p.items[i].itemKey
		p.before[last] += p.items[i].dur
		i++
	}
	p.items = p.items[i:]
	if i == 0 {
		return
	}
	for key := range p.before {
		if key != last && !p.holds(key) {
			delete(p.before, key)
		}
	}
}

// holds reports whether key has a stretch not yet fully heard.
func (p *Playout) holds(key itemKey) bool {
	for _, it := range p.items {
		if it.itemKey == key {
			return true
		}
	}
	return false
}

// tracked is how many items' earlier stretches the Playout remembers.
func (p *Playout) tracked() int { return len(p.before) }
