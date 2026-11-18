package stage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// DirClipCacheOption configures NewDirClipCache.
type DirClipCacheOption func(*dirClipCache)

// DirCacheReadOnly makes Put of a directory clip cache a no-op that returns
// nil, so a test or an arena run never writes. A clip the directory lacks is a
// miss.
func DirCacheReadOnly() DirClipCacheOption { return func(c *dirClipCache) { c.readOnly = true } }

// NewDirClipCache is a ClipCache over dir. Each clip is two files named by the
// key's hex: <key>.pcm (raw PCM16LE) and <key>.json, a sidecar naming the text,
// voice, source and format, so the directory can be read and checked into a
// repo:
//
//	{"text": "...", "voice": "alloy", "source": "tts:openai",
//	 "encoding": "pcm16", "sample_rate": 24000, "channels": 1, "duration_ms": 1830,
//	 "sha256": "..."}
//
// Writes are atomic (temp file and rename, .pcm first, .json last), so a .pcm
// without a .json, what a write cut short leaves, is a miss. Many writers, in
// one process or several, can share a directory. Get is a miss for a sidecar
// that is not the key's own (its text, voice, source or format does not make
// the key) and for a .pcm the sidecar does not vouch for (another length, or
// another digest). Put keeps only a clip Get would return, so it errors on one
// whose Meta does not make its key.
//
// A writable cache creates dir and sweeps temp files a crashed writer left
// more than an hour ago; a DirCacheReadOnly one needs dir to exist and
// changes nothing in it.
func NewDirClipCache(dir string, opts ...DirClipCacheOption) (ClipCache, error) {
	c := &dirClipCache{dir: dir, rename: os.Rename}
	for _, o := range opts {
		o(c)
	}
	if c.readOnly {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("clip cache directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("clip cache directory %s is not a directory", dir)
		}
		return c, nil
	}
	if err := os.MkdirAll(dir, dirClipMode); err != nil {
		return nil, fmt.Errorf("clip cache directory: %w", err)
	}
	c.sweepStaleTemps()
	return c, nil
}

type dirClipCache struct {
	dir      string
	readOnly bool
	// rename is os.Rename; a test replaces it.
	rename   func(oldpath, newpath string) error
	warnOnce sync.Once // a Put to a read-only cache
	readOnce sync.Once // a Get a read-only cache could not read
}

// clipSidecar is the .json beside a clip's .pcm.
type clipSidecar struct {
	Text       string `json:"text"`
	Voice      string `json:"voice"`
	Source     string `json:"source"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
	// SHA256 is the hex digest of the .pcm. A .pcm another writer replaced, or
	// one a crash left with the right length and other bytes, does not match.
	SHA256 string `json:"sha256"`
}

const (
	tempPrefix          = ".tmp-"
	dirClipEncoding     = "pcm16"
	dirClipMode         = 0o750
	bytesPerPCM16Sample = 2
	staleTempAge        = time.Hour
)

// sweepStaleTemps removes temp files a crashed writer left. A live writer's
// temp file is seconds old, so an hour-old one belongs to no one.
func (c *dirClipCache) sweepStaleTemps() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}
		if info, ierr := e.Info(); ierr == nil && time.Since(info.ModTime()) > staleTempAge {
			_ = os.Remove(filepath.Join(c.dir, e.Name()))
		}
	}
}

func pcmDigest(pcm []byte) string {
	sum := sha256.Sum256(pcm)
	return hex.EncodeToString(sum[:])
}

func keyHex(key ClipKey) string { return hex.EncodeToString(key[:]) }

func (c *dirClipCache) path(key ClipKey, ext string) string {
	return filepath.Join(c.dir, keyHex(key)+ext)
}

// Get implements ClipCache.
func (c *dirClipCache) Get(_ context.Context, key ClipKey) (CueClip, bool, error) {
	raw, err := os.ReadFile(c.path(key, ".json"))
	if errors.Is(err, os.ErrNotExist) {
		return CueClip{}, false, nil
	}
	if err != nil {
		return c.unreadable(key, err)
	}
	var side clipSidecar
	if uerr := json.Unmarshal(raw, &side); uerr != nil {
		logger.Warn("clip cache: unreadable sidecar; treating as a miss", "key", keyHex(key), "error", uerr)
		return CueClip{}, false, nil
	}
	format := ClipFormat{Encoding: AudioFormatPCM16, SampleRate: side.SampleRate, Channels: side.Channels}
	if side.Encoding != dirClipEncoding || ClipKeyFor(side.Text, side.Voice, side.Source, format) != key {
		logger.Warn("clip cache: sidecar does not match its key; treating as a miss", "key", keyHex(key))
		return CueClip{}, false, nil
	}
	pcm, err := os.ReadFile(c.path(key, ".pcm"))
	if errors.Is(err, os.ErrNotExist) {
		return CueClip{}, false, nil
	}
	if err != nil {
		return c.unreadable(key, err)
	}
	frame := bytesPerPCM16Sample * side.Channels
	if side.SampleRate <= 0 || frame <= 0 || len(pcm) == 0 || len(pcm)%frame != 0 ||
		audio.PCM16Duration(len(pcm), side.SampleRate, side.Channels).Milliseconds() != side.DurationMs ||
		side.SHA256 != pcmDigest(pcm) {
		logger.Warn("clip cache: audio does not match its sidecar; treating as a miss", "key", keyHex(key))
		return CueClip{}, false, nil
	}
	return CueClip{
		Audio: &AudioData{
			Samples: pcm, SampleRate: side.SampleRate, Channels: side.Channels, Format: AudioFormatPCM16,
			Duration: audio.PCM16Duration(len(pcm), side.SampleRate, side.Channels),
		},
		Meta: ClipMeta{Text: side.Text, Voice: side.Voice, Source: side.Source},
	}, true, nil
}

// unreadable is Get's answer for a file it could not read. A read-only cache
// is for runs that must not fail on a cache, so it warns once and misses; a
// writable one reports the fault, which the cue stage logs and treats as a miss.
func (c *dirClipCache) unreadable(key ClipKey, err error) (CueClip, bool, error) {
	if !c.readOnly {
		return CueClip{}, false, err
	}
	c.readOnce.Do(func() {
		logger.Warn("clip cache: the directory cannot be read; every clip is a miss", "dir", c.dir,
			"key", keyHex(key), "error", err)
	})
	return CueClip{}, false, nil
}

// Put implements ClipCache. It keeps only a clip Get would return: PCM16 audio
// in whole frames whose Meta and format make key.
func (c *dirClipCache) Put(_ context.Context, key ClipKey, clip CueClip) error {
	if c.readOnly {
		c.warnOnce.Do(func() {
			logger.Warn("clip cache: the directory is read-only; clips are not kept", "dir", c.dir)
		})
		return nil
	}
	a := clip.Audio
	if a == nil || a.Format != AudioFormatPCM16 || a.Channels <= 0 || a.SampleRate <= 0 {
		return errors.New("clip cache: only a clip with PCM16 audio can be kept in a directory")
	}
	if len(a.Samples) == 0 || len(a.Samples)%(bytesPerPCM16Sample*a.Channels) != 0 {
		return errors.New("clip cache: the clip's audio is empty or not whole PCM16 frames")
	}
	format := ClipFormat{Encoding: AudioFormatPCM16, SampleRate: a.SampleRate, Channels: a.Channels}
	if ClipKeyFor(clip.Meta.Text, clip.Meta.Voice, clip.Meta.Source, format) != key {
		return errors.New("clip cache: the clip's Meta and format do not make the key it is put under")
	}
	side, err := json.MarshalIndent(clipSidecar{
		Text: clip.Meta.Text, Voice: clip.Meta.Voice, Source: clip.Meta.Source, Encoding: dirClipEncoding,
		SampleRate: a.SampleRate, Channels: a.Channels,
		DurationMs: audio.PCM16Duration(len(a.Samples), a.SampleRate, a.Channels).Milliseconds(),
		SHA256:     pcmDigest(a.Samples),
	}, "", "  ")
	if err != nil {
		return err
	}
	// The sidecar goes last: it is what makes the clip visible to Get. Two
	// writers of different bytes under one key can leave one's .pcm under the
	// other's sidecar; the digest makes that a miss, and the next Put heals it.
	if err := c.writeAtomic(c.path(key, ".pcm"), a.Samples); err != nil {
		return err
	}
	return c.writeAtomic(c.path(key, ".json"), append(side, '\n'))
}

// writeAtomic writes data to a temp file in the directory and renames it to
// path, so a reader sees all of a file or none. A rename can fail where
// another writer or reader holds path (Windows will not replace an open
// file); that is success when path already holds exactly data.
func (c *dirClipCache) writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(c.dir, tempPrefix+"*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = c.rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		if have, rerr := os.ReadFile(filepath.Clean(path)); rerr == nil && bytes.Equal(have, data) {
			return nil
		}
	}
	return err
}
