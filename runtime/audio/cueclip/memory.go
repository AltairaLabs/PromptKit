// Package cueclip keeps rendered cue clips: the short pieces of speech a
// realtime session plays for a cue, such as a guardrail's message. A clip is
// rendered once and kept in a Cache under a content Key, so sessions that need
// the same clip (the same text, voice, source and format) play it without
// rendering it again.
//
// The cue stage (runtime/pipeline/stage) reads and writes the cache; a host
// chooses it: NewMemoryCache for one process, NewDirCache for a directory that
// can be filled offline and checked in, or its own Cache over a shared store.
package cueclip

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
)

// keyEncoding is the encoding named in every key: a clip is PCM16.
const keyEncoding = "pcm16"

// Format is the format of a clip's audio, which is always PCM16
// little-endian.
type Format struct {
	SampleRate int
	Channels   int
}

// Key identifies a rendered clip by what determines its audio.
type Key [32]byte

// KeyFor is the key of the clip that says text in voice from the source kind
// sourceKind ("tts:<service name>", "session:<family>" or "earcon"), in
// format. It is SHA-256 over the text (trimmed and in Unicode NFC, otherwise
// exact: punctuation changes how it is said), the voice, the source kind and
// the format. No model name goes into it: a model change behind the same
// source and voice keeps the key, so a host that wants fresh clips after one
// clears its cache.
func KeyFor(text, voice, sourceKind string, format Format) Key {
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
	field(keyEncoding)
	num(format.SampleRate)
	num(format.Channels)
	var key Key
	copy(key[:], h.Sum(nil))
	return key
}

// Meta is what a clip says and who said it: the inputs of its Key besides the
// format.
type Meta struct {
	Text   string // the text the key was made from
	Voice  string
	Source string // the source kind: "tts:<service>", "session:<family>", "earcon"
}

// Clip is a rendered clip, ready to play: trimmed, faded out and in Format.
type Clip struct {
	// PCM is the audio, PCM16 little-endian in Format.
	PCM    []byte
	Format Format
	// Meta says where the clip came from. A cache that keeps clips where a
	// person can read them (the directory cache's sidecar) records it; it is
	// not part of what plays.
	Meta Meta
}

// Duration is how long the clip plays.
func (c Clip) Duration() time.Duration {
	return audio.PCM16Duration(len(c.PCM), c.Format.SampleRate, c.Format.Channels)
}

// Cache stores rendered clips by content key, so sessions that need the same
// clip render it once. Implementations must be safe for concurrent use. Get
// returns a miss as (zero, false, nil); an error is a cache fault, which the
// cue stage logs and treats as a miss.
//
// The cue stage renders through a single-flight group within its process, so
// one process renders a key once at a time. A store shared across processes
// (many pods serving the same pack) does not get that across processes: each
// process can render a key once before the store holds it. An implementation
// that wants no duplicate writes should make Put put-if-absent (Redis SET NX,
// a blob store's create-if-absent).
//
// A cached clip is ready to play at the format in its key, so a hit needs no
// processing. Its bytes are shared: no one modifies them.
type Cache interface {
	Get(ctx context.Context, key Key) (Clip, bool, error)
	Put(ctx context.Context, key Key, clip Clip) error
}

// DefaultCacheBytes is the size of the process-wide in-memory cache a cue
// stage uses when it is given none.
const DefaultCacheBytes = 64 << 20

// NewMemoryCache is an in-process Cache that keeps at most maxBytes of clip
// audio, evicting the least recently used clips first. A clip larger than
// maxBytes is not kept.
func NewMemoryCache(maxBytes int64) Cache {
	return &memoryCache{max: maxBytes, entries: map[Key]*list.Element{}, order: list.New()}
}

type memoryCache struct {
	mu      sync.Mutex
	max     int64
	size    int64
	entries map[Key]*list.Element
	// order holds *memoryEntry, most recently used first.
	order *list.List
}

type memoryEntry struct {
	key  Key
	clip Clip
	size int64
}

// Get implements Cache.
func (c *memoryCache) Get(_ context.Context, key Key) (Clip, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return Clip{}, false, nil
	}
	c.order.MoveToFront(el)
	return el.Value.(*memoryEntry).clip, true, nil
}

// Put implements Cache.
func (c *memoryCache) Put(_ context.Context, key Key, clip Clip) error {
	size := int64(len(clip.PCM))
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
	if size > c.max {
		return nil
	}
	c.entries[key] = c.order.PushFront(&memoryEntry{key: key, clip: clip, size: size})
	c.size += size
	for c.size > c.max {
		c.remove(c.order.Back())
	}
	return nil
}

func (c *memoryCache) remove(el *list.Element) {
	e := c.order.Remove(el).(*memoryEntry)
	delete(c.entries, e.key)
	c.size -= e.size
}
