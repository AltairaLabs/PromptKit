package stage

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// DirClipCacheOption configures NewDirClipCache.
type DirClipCacheOption func(*dirClipCache)

// ReadOnly makes Put of a directory clip cache a no-op that returns nil, so a
// test or an arena run never writes. A clip the directory lacks is a miss.
func ReadOnly() DirClipCacheOption { return func(c *dirClipCache) { c.readOnly = true } }

// NewDirClipCache is a ClipCache over dir. Each clip is two files named by the
// key's hex: <key>.pcm (raw PCM16LE) and <key>.json, a sidecar naming the text,
// voice, source and format, so the directory can be read and checked into a
// repo:
//
//	{"text": "...", "voice": "alloy", "source": "tts:openai",
//	 "encoding": "pcm16", "sample_rate": 24000, "channels": 1, "duration_ms": 1830}
//
// Writes are atomic (temp file and rename, .pcm first, .json last), so a .pcm
// without a .json, what a write cut short leaves, is a miss. Many writers, in
// one process or several, can share a directory. Get is a miss for a sidecar
// that is not the key's own: one whose text, voice, source or format does not
// make the key, or whose duration the .pcm does not fill.
//
// A writable cache creates dir; a ReadOnly one needs it to exist.
func NewDirClipCache(dir string, opts ...DirClipCacheOption) (ClipCache, error) {
	c := &dirClipCache{dir: dir}
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
	return c, nil
}

type dirClipCache struct {
	dir      string
	readOnly bool
	warnOnce sync.Once
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
}

const (
	dirClipEncoding     = "pcm16"
	dirClipMode         = 0o750
	bytesPerPCM16Sample = 2
)

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
		return CueClip{}, false, err
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
		return CueClip{}, false, err
	}
	frame := bytesPerPCM16Sample * side.Channels
	if side.SampleRate <= 0 || frame <= 0 || len(pcm) == 0 || len(pcm)%frame != 0 ||
		audio.PCM16Duration(len(pcm), side.SampleRate, side.Channels).Milliseconds() != side.DurationMs {
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

// Put implements ClipCache. Only a clip with PCM16 audio can be kept.
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
	side, err := json.MarshalIndent(clipSidecar{
		Text: clip.Meta.Text, Voice: clip.Meta.Voice, Source: clip.Meta.Source, Encoding: dirClipEncoding,
		SampleRate: a.SampleRate, Channels: a.Channels,
		DurationMs: audio.PCM16Duration(len(a.Samples), a.SampleRate, a.Channels).Milliseconds(),
	}, "", "  ")
	if err != nil {
		return err
	}
	// The sidecar goes last: it is what makes the clip visible to Get.
	if err := c.writeAtomic(c.path(key, ".pcm"), a.Samples); err != nil {
		return err
	}
	return c.writeAtomic(c.path(key, ".json"), append(side, '\n'))
}

// writeAtomic writes data to a temp file in the directory and renames it to
// path, so a reader sees all of a file or none.
func (c *dirClipCache) writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
