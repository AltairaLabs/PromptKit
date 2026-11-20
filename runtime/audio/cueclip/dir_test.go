package cueclip

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

const dirTestText = "We don't talk about Bruno."

func dirTestClip(pcm []byte) Clip {
	return Clip{PCM: pcm, Format: pcm24k, Meta: Meta{Text: dirTestText, Voice: "alloy", Source: "tts:openai"}}
}

// cueRamp is n samples of PCM16 mono that never fall to silence.
func cueRamp(n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// lockedLog is a log sink safe for concurrent writes.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// captureLogs routes the package logger into a lockedLog for the test.
func captureLogs(t *testing.T) *lockedLog {
	t.Helper()
	l := &lockedLog{}
	logger.SetOutput(l)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return l
}

func dirTestKey() Key { return KeyFor(dirTestText, "alloy", "tts:openai", pcm24k) }

func dirTestNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// A clip put in one cache is read by a fresh cache on the same directory, byte
// for byte, and the sidecar says exactly what the clip is.
func TestDirCache_PutThenGetFromFreshInstance(t *testing.T) {
	dir := t.TempDir()
	pcm := cueRamp(2400) // 100 ms
	w, err := NewDirCache(dir)
	require.NoError(t, err)
	require.NoError(t, w.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))

	r, err := NewDirCache(dir)
	require.NoError(t, err)
	got, ok, err := r.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, pcm, got.PCM)
	assert.Equal(t, pcm24k, got.Format)
	assert.Equal(t, int64(100), got.Duration().Milliseconds())
	assert.Equal(t, Meta{Text: dirTestText, Voice: "alloy", Source: "tts:openai"}, got.Meta)

	hex := keyHex(dirTestKey())
	assert.Len(t, hex, 64)
	assert.ElementsMatch(t, []string{hex + ".pcm", hex + ".json"}, dirTestNames(t, dir))
	onDisk, err := os.ReadFile(filepath.Join(dir, hex+".pcm"))
	require.NoError(t, err)
	assert.Equal(t, pcm, onDisk)
	side, err := os.ReadFile(filepath.Join(dir, hex+".json"))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(side, &fields))
	assert.Equal(t, map[string]any{
		"text": dirTestText, "voice": "alloy", "source": "tts:openai",
		"encoding": "pcm16", "sample_rate": float64(24000), "channels": float64(1), "duration_ms": float64(100), "sha256": pcmDigest(pcm),
	}, fields)
}

func TestDirCache_MissOnEmptyDir(t *testing.T) {
	c, err := NewDirCache(t.TempDir())
	require.NoError(t, err)
	_, ok, err := c.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	assert.False(t, ok)
}

// A write interrupted between the .pcm and the .json leaves a .pcm alone: a miss.
func TestDirCache_PcmWithoutSidecarIsAMiss(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(cueRamp(2400))))
	require.NoError(t, os.Remove(filepath.Join(dir, keyHex(dirTestKey())+".json")))
	_, ok, err := c.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	assert.False(t, ok)
}

// A sidecar that disagrees with the key (hand-edited, stale), is not JSON or
// is empty is a miss, never a clip that plays at the wrong rate.
func TestDirCache_InconsistentSidecarIsAMiss(t *testing.T) {
	set := func(k string, v any) func(map[string]any) []byte {
		return func(m map[string]any) []byte { m[k] = v; return mustJSON(t, m) }
	}
	cases := map[string]func(m map[string]any) []byte{
		"sample rate": set("sample_rate", 16000),
		"channels":    set("channels", 2),
		"encoding":    set("encoding", "opus"),
		"text":        set("text", "Another text."),
		"voice":       set("voice", "verse"),
		"source":      set("source", "tts:elevenlabs"),
		"duration":    set("duration_ms", 99),
		"not json":    func(map[string]any) []byte { return []byte("{not json") },
		"empty":       func(map[string]any) []byte { return nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			c, err := NewDirCache(dir)
			require.NoError(t, err)
			require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(cueRamp(2400))))
			side := filepath.Join(dir, keyHex(dirTestKey())+".json")
			raw, err := os.ReadFile(side)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			require.NoError(t, os.WriteFile(side, edit(m), 0o600))

			_, ok, err := c.Get(context.Background(), dirTestKey())
			require.NoError(t, err)
			assert.False(t, ok)
		})
	}
}

// A .pcm cut short, or not whole samples, is a miss.
func TestDirCache_DamagedPcmIsAMiss(t *testing.T) {
	for name, pcm := range map[string][]byte{"truncated": cueRamp(2000), "odd length": append(cueRamp(2400), 7)} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			c, err := NewDirCache(dir)
			require.NoError(t, err)
			require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(cueRamp(2400))))
			require.NoError(t, os.WriteFile(filepath.Join(dir, keyHex(dirTestKey())+".pcm"), pcm, 0o600))
			_, ok, err := c.Get(context.Background(), dirTestKey())
			require.NoError(t, err)
			assert.False(t, ok)
		})
	}
}

// A read-only cache writes nothing and reads what is there.
func TestDirCache_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDirCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(2400)
	require.NoError(t, w.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))
	before := dirTestNames(t, dir)

	ro, err := NewDirCache(dir, ReadOnly())
	require.NoError(t, err)
	other := KeyFor("Other.", "alloy", "tts:openai", pcm24k)
	require.NoError(t, ro.Put(context.Background(), other, dirTestClip(cueRamp(100))))
	require.NoError(t, ro.Put(context.Background(), other, dirTestClip(cueRamp(100))))
	assert.Equal(t, before, dirTestNames(t, dir), "nothing written")
	_, ok, err := ro.Get(context.Background(), other)
	require.NoError(t, err)
	assert.False(t, ok, "a read-only miss is a miss")
	got, ok, err := ro.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, pcm, got.PCM)
}

// A read-only cache does not create its directory; a missing one is an error
// rather than a cache that always misses. A writable cache makes it.
func TestDirCache_ReadOnlyNeedsTheDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := NewDirCache(missing, ReadOnly())
	require.Error(t, err)
	_, statErr := os.Stat(missing)
	assert.True(t, os.IsNotExist(statErr))

	_, err = NewDirCache(missing)
	require.NoError(t, err)
	info, statErr := os.Stat(missing)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

// A clip with no audio, or no format, cannot be kept: Put says so and writes nothing.
func TestDirCache_PutRejectsWhatItCannotStore(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	assert.Error(t, c.Put(context.Background(), dirTestKey(), Clip{}))
	noRate := dirTestClip(cueRamp(10))
	noRate.Format.SampleRate = 0
	assert.Error(t, c.Put(context.Background(), dirTestKey(), noRate))
	noChannels := dirTestClip(cueRamp(10))
	noChannels.Format.Channels = 0
	assert.Error(t, c.Put(context.Background(), dirTestKey(), noChannels))
	assert.Empty(t, dirTestNames(t, dir))
}

// Many writers of one key, each its own cache over the directory, leave one
// complete pair and no stray temp files; readers never see half a clip.
func TestDirCache_ConcurrentPutsLeaveOnePair(t *testing.T) {
	dir := t.TempDir()
	pcm := cueRamp(24000)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c, err := NewDirCache(dir)
			assert.NoError(t, err)
			assert.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))
		}()
		go func() {
			defer wg.Done()
			c, err := NewDirCache(dir)
			assert.NoError(t, err)
			got, ok, err := c.Get(context.Background(), dirTestKey())
			assert.NoError(t, err)
			if ok {
				assert.Equal(t, pcm, got.PCM)
			}
		}()
	}
	wg.Wait()
	hex := keyHex(dirTestKey())
	assert.ElementsMatch(t, []string{hex + ".pcm", hex + ".json"}, dirTestNames(t, dir))
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	got, ok, err := c.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, pcm, got.PCM)
}

// Put refuses a clip whose Meta does not make the key it is put under: the
// sidecar would never match, so the clip would miss forever.
func TestDirCache_PutRejectsMetaThatIsNotTheKey(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	noMeta := Clip{PCM: cueRamp(2400), Format: pcm24k}
	wrongVoice := dirTestClip(cueRamp(2400))
	wrongVoice.Meta.Voice = "verse"
	wrongRate := dirTestClip(cueRamp(1600))
	wrongRate.Format.SampleRate = 16000
	for name, clip := range map[string]Clip{"no meta": noMeta, "voice": wrongVoice, "rate": wrongRate} {
		assert.Error(t, c.Put(context.Background(), dirTestKey(), clip), name)
	}
	assert.Empty(t, dirTestNames(t, dir))
}

// Put refuses audio Get would always reject: no samples, or a part of a frame.
func TestDirCache_PutRejectsUnreadableAudio(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	for name, pcm := range map[string][]byte{"empty": nil, "half a sample": cueRamp(10)[:19]} {
		assert.Error(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)), name)
	}
	assert.Empty(t, dirTestNames(t, dir))
}

// A .pcm of the right length but other bytes than the sidecar vouches for (two
// writers that raced, a crash mid-write) is a miss, and a later Put heals it.
func TestDirCache_PcmOfOtherBytesIsAMissAndHeals(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(2400)
	require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))
	garbage := append([]byte(nil), pcm...)
	garbage[100] ^= 0xff
	require.NoError(t, os.WriteFile(filepath.Join(dir, keyHex(dirTestKey())+".pcm"), garbage, 0o600))
	_, ok, err := c.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))
	got, ok, err := c.Get(context.Background(), dirTestKey())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, pcm, got.PCM)
}

// A rename that fails because another writer holds the destination (Windows
// refuses to replace a file a reader has open) is success when the directory
// then holds the very clip being put, and an error when it does not.
func TestDirCache_RenameFailureWhereTheClipIsAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(2400)
	require.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)))
	c.(*dirCache).rename = func(string, string) error { return errors.New("the file is in use") }

	assert.NoError(t, c.Put(context.Background(), dirTestKey(), dirTestClip(pcm)), "the same clip is there")
	assert.Error(t, c.Put(context.Background(), dirTestKey(), dirTestClip(cueRamp(2401))), "another clip is there")

	empty := t.TempDir()
	c2, err := NewDirCache(empty)
	require.NoError(t, err)
	c2.(*dirCache).rename = func(string, string) error { return errors.New("the file is in use") }
	assert.Error(t, c2.Put(context.Background(), dirTestKey(), dirTestClip(pcm)), "nothing is there")
	assert.Empty(t, dirTestNames(t, empty), "no temp file is left")
}

// A writable cache sweeps temp files a crashed writer left, once they are old
// enough that no live writer can own them; a read-only one touches nothing.
func TestDirCache_SweepsStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".tmp-stale")
	fresh := filepath.Join(dir, ".tmp-fresh")
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{stale, fresh, other} {
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))
	require.NoError(t, os.Chtimes(other, old, old))

	_, err := NewDirCache(dir, ReadOnly())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".tmp-stale", ".tmp-fresh", "notes.txt"}, dirTestNames(t, dir))

	_, err = NewDirCache(dir)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".tmp-fresh", "notes.txt"}, dirTestNames(t, dir))
}

// A read-only cache over a directory it cannot read warns once, not per Get,
// and every Get is a miss.
func TestDirCache_ReadOnlyUnreadableDirWarnsOnce(t *testing.T) {
	dir := t.TempDir()
	c, err := NewDirCache(dir, ReadOnly())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, readErr := os.ReadFile(filepath.Join(dir, "x")); !errors.Is(readErr, os.ErrPermission) {
		t.Skip("this user can read a directory with no permissions")
	}
	logs := captureLogs(t)
	for i := 0; i < 3; i++ {
		_, ok, err := c.Get(context.Background(), dirTestKey())
		require.NoError(t, err)
		assert.False(t, ok)
	}
	assert.Equal(t, 1, strings.Count(logs.String(), "cannot be read"))
}
