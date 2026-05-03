package selfplay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// envTTSCacheDir is the environment variable that opts a registry into
// content-addressed TTS caching. When set to a directory path, every TTS
// service the registry hands out is transparently wrapped in a
// CachedTTSService backed by that directory. First call to Synthesize
// hits the upstream provider and persists the bytes; subsequent calls
// for the same (provider, voice, format, text) tuple read from disk.
//
// Intended for tests and demos: the OpenAI / Cartesia / ElevenLabs APIs
// charge per call, but their output is deterministic enough for the same
// input to be safely replayed.
const envTTSCacheDir = "TTS_CACHE_DIR"

// cachedSynthesisExt is the on-disk file extension for cached audio. The
// format is opaque PCM bytes — whatever the upstream provider returned —
// so the extension is purely informational.
const cachedSynthesisExt = ".bin"

// cacheDirPerm is the permission bits used for the cache directory tree.
// Owner read/write/execute, group read/execute, world none.
const cacheDirPerm os.FileMode = 0o750

// CachedTTSService wraps another tts.Service with a content-addressed
// disk cache. Cache keys hash the backend name, voice, format, and full
// text together so two backends or two voices for the same text never
// collide. Cache misses fall through to the backend and persist the
// returned bytes; cache hits read straight from disk.
//
// Thread-safe: a per-key sync.Mutex prevents two concurrent Synthesize
// calls for the same key from both hitting the backend, but otherwise
// requests fan out freely.
type CachedTTSService struct {
	backend tts.Service
	dir     string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewCachedTTSService wraps backend in a disk cache rooted at dir. The
// directory is created if missing. Returns the backend unwrapped if dir
// is empty (so callers can pass an env-var lookup directly).
func NewCachedTTSService(backend tts.Service, dir string) (tts.Service, error) {
	if dir == "" {
		return backend, nil
	}
	if err := os.MkdirAll(dir, cacheDirPerm); err != nil {
		return nil, fmt.Errorf("tts cache: create dir %s: %w", dir, err)
	}
	return &CachedTTSService{
		backend: backend,
		dir:     dir,
		locks:   make(map[string]*sync.Mutex),
	}, nil
}

// Name returns the underlying backend's name unchanged so consumers can't
// tell whether they're hitting the cache.
func (c *CachedTTSService) Name() string { return c.backend.Name() }

// SupportedVoices proxies to the backend.
func (c *CachedTTSService) SupportedVoices() []tts.Voice { return c.backend.SupportedVoices() }

// SupportedFormats proxies to the backend.
func (c *CachedTTSService) SupportedFormats() []tts.AudioFormat {
	return c.backend.SupportedFormats()
}

// Synthesize returns cached audio when available; otherwise calls the
// backend and persists its output before returning a reader for it.
//
//nolint:gocritic // hugeParam: interface compliance requires value receiver for config
func (c *CachedTTSService) Synthesize(
	ctx context.Context, text string, cfg tts.SynthesisConfig,
) (io.ReadCloser, error) {
	key := cacheKey(c.backend.Name(), cfg.Voice, cfg.Format, text)
	path := filepath.Join(c.dir, key+cachedSynthesisExt)

	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path = c.dir + sha256 hex; sandboxed
		logger.Debug("tts cache: hit", "provider", c.backend.Name(), "voice", cfg.Voice, "key", key)
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	keyLock := c.lockFor(key)
	keyLock.Lock()
	defer keyLock.Unlock()

	// Re-check under lock — another goroutine may have populated the cache.
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path = c.dir + sha256 hex; sandboxed
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	reader, err := c.backend.Synthesize(ctx, text, cfg)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("tts cache: read backend stream: %w", err)
	}

	if err := writeCacheFile(path, data); err != nil {
		// Don't fail the synthesis on a cache write error; just log and
		// return the bytes we already have. The user's call still succeeds;
		// the next call will retry the cache write.
		logger.Warn("tts cache: write failed", "path", path, "error", err)
	} else {
		logger.Debug("tts cache: miss persisted",
			"provider", c.backend.Name(), "voice", cfg.Voice, "bytes", len(data), "key", key)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// lockFor returns the per-key mutex, creating it on first use. The map
// itself is guarded by c.mu; the returned mutex lives forever for the
// life of the cache, which is fine since cache keys are bounded by the
// product of (provider × voice × format × distinct text), all small.
func (c *CachedTTSService) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lk, ok := c.locks[key]
	if !ok {
		lk = &sync.Mutex{}
		c.locks[key] = lk
	}
	return lk
}

// cacheKey produces a stable hex digest for a synthesis request. Inputs
// are joined with NUL bytes that cannot appear in any single field so
// the concatenation is unambiguous.
func cacheKey(provider, voice string, format tts.AudioFormat, text string) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte{0})
	h.Write([]byte(voice))
	h.Write([]byte{0})
	h.Write([]byte(format.Name))
	h.Write([]byte{0})
	_, _ = fmt.Fprintf(h, "%d/%d/%d", format.SampleRate, format.BitDepth, format.Channels)
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// writeCacheFile persists data atomically by writing to a temp sibling
// then renaming. Concurrent writers for the same key are serialized by
// CachedTTSService.lockFor; this guards against partial files on crash.
func writeCacheFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// resolveTTSCacheDir reports the cache directory the registry should
// wrap services with, or "" to skip caching. Looks at the TTS_CACHE_DIR
// environment variable.
func resolveTTSCacheDir() string {
	return strings.TrimSpace(os.Getenv(envTTSCacheDir))
}
