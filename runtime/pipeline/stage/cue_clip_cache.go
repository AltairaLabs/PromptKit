package stage

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// ClipFormat is the PCM format of a rendered clip.
type ClipFormat struct {
	Encoding   AudioFormat
	SampleRate int
	Channels   int
}

// ClipKey identifies a rendered clip by what determines its audio.
type ClipKey [32]byte

// ClipKeyFor is the key of the clip that says text in voice from the source
// kind sourceKind ("tts:<service name>", "session:<family>" or "earcon"), in
// format. It is SHA-256 over the text (trimmed and in Unicode NFC, otherwise
// exact: punctuation changes how it is said), the voice, the source kind and
// the format. No model name goes into it: a model change behind the same
// source and voice keeps the key, so a host that wants fresh clips after one
// clears its cache.
func ClipKeyFor(text, voice, sourceKind string, format ClipFormat) ClipKey {
	h := sha256.New()
	field := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(s))
	}
	num := func(v int) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(int64(v))) //nolint:gosec // a sign change still gives a distinct key
		_, _ = h.Write(n[:])
	}
	field(norm.NFC.String(strings.TrimSpace(text)))
	field(voice)
	field(sourceKind)
	field(format.Encoding.String())
	num(format.SampleRate)
	num(format.Channels)
	var key ClipKey
	copy(key[:], h.Sum(nil))
	return key
}

// ClipCache stores rendered clips by content key, so sessions that need the
// same clip render it once. Implementations must be safe for concurrent use.
// Get returns a miss as (zero, false, nil); an error is a cache fault, which
// the cue stage logs and treats as a miss.
//
// The cue stage renders through a single-flight group within its process, so
// one process renders a key once at a time. A store shared across processes
// (many pods serving the same pack) does not get that across processes: each
// process can render a key once before the store holds it. An implementation
// that wants no duplicate writes should make Put put-if-absent (Redis SET NX,
// a blob store's create-if-absent).
//
// A cached clip is ready to play: trimmed, faded out and at the format in its
// key, so a hit needs no processing. Its bytes are shared: no one modifies
// them.
type ClipCache interface {
	Get(ctx context.Context, key ClipKey) (CueClip, bool, error)
	Put(ctx context.Context, key ClipKey, clip CueClip) error
}

// DefaultClipCacheBytes is the size of the process-wide clip cache a cue
// stage uses when its config names none.
const DefaultClipCacheBytes = 64 << 20

var (
	defaultClipCacheOnce sync.Once
	defaultClipCacheInst ClipCache
)

// defaultClipCache is the process-wide in-memory clip cache.
func defaultClipCache() ClipCache {
	defaultClipCacheOnce.Do(func() { defaultClipCacheInst = NewMemoryClipCache(DefaultClipCacheBytes) })
	return defaultClipCacheInst
}

// NewMemoryClipCache is an in-process ClipCache that keeps at most maxBytes of
// clip media, evicting the least recently used clips first. A clip larger
// than maxBytes is not kept.
func NewMemoryClipCache(maxBytes int64) ClipCache {
	return &memoryClipCache{max: maxBytes, entries: map[ClipKey]*list.Element{}, order: list.New()}
}

type memoryClipCache struct {
	mu      sync.Mutex
	max     int64
	size    int64
	entries map[ClipKey]*list.Element
	// order holds *memoryClipEntry, most recently used first.
	order *list.List
}

type memoryClipEntry struct {
	key  ClipKey
	clip CueClip
	size int64
}

// clipBytes is the media a clip holds.
func clipBytes(clip CueClip) int64 {
	var n int64
	if clip.Audio != nil {
		n += int64(len(clip.Audio.Samples))
	}
	if clip.Video != nil {
		n += int64(len(clip.Video.Data))
	}
	return n
}

// Get implements ClipCache.
func (c *memoryClipCache) Get(_ context.Context, key ClipKey) (CueClip, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return CueClip{}, false, nil
	}
	c.order.MoveToFront(el)
	return el.Value.(*memoryClipEntry).clip, true, nil
}

// Put implements ClipCache.
func (c *memoryClipCache) Put(_ context.Context, key ClipKey, clip CueClip) error {
	size := clipBytes(clip)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
	if size > c.max {
		return nil
	}
	c.entries[key] = c.order.PushFront(&memoryClipEntry{key: key, clip: clip, size: size})
	c.size += size
	for c.size > c.max {
		c.remove(c.order.Back())
	}
	return nil
}

func (c *memoryClipCache) remove(el *list.Element) {
	e := c.order.Remove(el).(*memoryClipEntry)
	delete(c.entries, e.key)
	c.size -= e.size
}
