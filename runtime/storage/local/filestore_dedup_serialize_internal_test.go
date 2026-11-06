package local

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockProbeHandler reports expiry times and tracks media, recording at each
// call whether the store's dedup lock was held at that moment.
type lockProbeHandler struct {
	fs    *FileStore
	calls []string
}

func (h *lockProbeHandler) ApplyPolicy(context.Context, *storage.MediaMetadata) error { return nil }
func (h *lockProbeHandler) EnforcePolicy(context.Context, string) error               { return nil }

func (h *lockProbeHandler) probe(call string) {
	state := "held"
	if h.fs.dedupMu.TryLock() {
		h.fs.dedupMu.Unlock()
		state = "free"
	}
	h.calls = append(h.calls, call+":"+state)
}

func (h *lockProbeHandler) MediaExpiresAt(metadata *storage.MediaMetadata) (time.Time, bool, error) {
	h.probe("expiry")
	return metadata.Timestamp.Add(time.Hour), true, nil
}

func (h *lockProbeHandler) TrackMedia(string, *storage.MediaMetadata) error {
	h.probe("track")
	return nil
}

// Every write of a deduplicated file's metadata, by the store that writes the
// file and by a later hit that extends its retention, happens under dedupMu.
// So no two stores of the same content can interleave their metadata writes
// and leave the shorter retention, or a torn file, behind.
func TestDedup_MetadataWritesRunUnderTheDedupLock(t *testing.T) {
	for name, content := range map[string]func(t *testing.T) *types.MediaContent{
		"inline": func(*testing.T) *types.MediaContent {
			data := base64.StdEncoding.EncodeToString([]byte("serialized"))
			return &types.MediaContent{Data: &data, MIMEType: "image/jpeg"}
		},
		"streaming": func(t *testing.T) *types.MediaContent {
			path := filepath.Join(t.TempDir(), "src.bin")
			require.NoError(t, os.WriteFile(path, []byte("serialized"), 0o600))
			return &types.MediaContent{FilePath: &path, MIMEType: "image/jpeg"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			handler := &lockProbeHandler{}
			fs, err := NewFileStore(FileStoreConfig{
				BaseDir: t.TempDir(), DefaultPolicy: "p", EnableDeduplication: true,
			}, WithPolicyHandler(handler))
			require.NoError(t, err)
			handler.fs = fs
			base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			meta := func(ts time.Time) *storage.MediaMetadata {
				return &storage.MediaMetadata{SessionID: "s", MIMEType: "image/jpeg", Timestamp: ts}
			}
			src := content(t)

			ref, err := fs.StoreMedia(context.Background(), src, meta(base))
			require.NoError(t, err)
			metaBefore, err := os.Stat(string(ref) + ".meta")
			require.NoError(t, err)
			assert.Equal(t, []string{"track:held"}, handler.calls, "the miss tracks its metadata under dedupMu")

			handler.calls = nil // a later hit outlasts the metadata on disk
			_, err = fs.StoreMedia(context.Background(), src, meta(base.Add(time.Minute)))
			require.NoError(t, err)
			assert.Equal(t, []string{"expiry:held", "expiry:held", "track:held"}, handler.calls,
				"the hit compares and rewrites the metadata under dedupMu")
			metaAfter, err := os.Stat(string(ref) + ".meta")
			require.NoError(t, err)
			assert.False(t, os.SameFile(metaBefore, metaAfter), "the .meta is replaced by rename, not rewritten in place")
		})
	}
}

// writeFileAtomic writes through a temporary file of its own, so it does not
// depend on, or collide over, a fixed temporary name next to the target.
func TestWriteFileAtomic_UsesItsOwnTemporaryFile(t *testing.T) {
	fs, err := NewFileStore(FileStoreConfig{BaseDir: t.TempDir()})
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "x.meta")
	require.NoError(t, os.Mkdir(target+".tmp", 0o700)) // blocks a fixed "<path>.tmp"

	require.NoError(t, fs.writeFileAtomic(target, []byte("data")))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))
	entries, err := os.ReadDir(filepath.Dir(target))
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no temporary file is left behind")
}

// The dedup index is saved by writing a new file and renaming it into place,
// never by rewriting the existing file in place, so concurrent saves never
// tear it.
func TestSaveDedupIndex_WritesAtomically(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true})
	require.NoError(t, err)
	indexPath := filepath.Join(baseDir, ".dedup_index.json")
	require.NoError(t, os.WriteFile(indexPath, []byte("{}"), 0o600))
	before, err := os.Stat(indexPath)
	require.NoError(t, err)

	fs.dedupMu.Lock()
	fs.dedupIndex["h"] = "p"
	fs.dedupDirty = true
	fs.dedupMu.Unlock()
	require.NoError(t, fs.Flush())

	after, err := os.Stat(indexPath)
	require.NoError(t, err)
	assert.False(t, os.SameFile(before, after), "the index is replaced by rename, not rewritten in place")
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	assert.JSONEq(t, `{"h":"p"}`, string(data))
}

// A save that fails to write leaves the index marked dirty, so the next save
// writes it.
func TestSaveDedupIndex_FailedWriteStaysDirty(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true})
	require.NoError(t, err)
	fs.dedupMu.Lock()
	fs.dedupIndex["h"] = "p"
	fs.dedupDirty = true
	fs.dedupMu.Unlock()

	require.NoError(t, os.Chmod(baseDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(baseDir, 0o700) })
	require.Error(t, fs.Flush())
	fs.dedupMu.RLock()
	assert.True(t, fs.dedupDirty, "a failed save keeps the changes pending")
	fs.dedupMu.RUnlock()

	require.NoError(t, os.Chmod(baseDir, 0o700))
	require.NoError(t, fs.Flush())
	assert.FileExists(t, filepath.Join(baseDir, ".dedup_index.json"))
}

// Dropping a dedup entry whose file is gone also drops its reference count,
// so counts for deleted files do not accumulate.
func TestDedupHit_StaleEntryDropsItsReferenceCount(t *testing.T) {
	fs, err := NewFileStore(FileStoreConfig{BaseDir: t.TempDir(), EnableDeduplication: true})
	require.NoError(t, err)
	data := base64.StdEncoding.EncodeToString([]byte("stale"))
	content := &types.MediaContent{Data: &data, MIMEType: "image/jpeg"}
	meta := &storage.MediaMetadata{SessionID: "s", MIMEType: "image/jpeg", Timestamp: time.Now()}

	ref, err := fs.StoreMedia(context.Background(), content, meta)
	require.NoError(t, err)
	require.NoError(t, os.Remove(string(ref))) // deleted outside DeleteMedia, as a policy does

	again, err := fs.StoreMedia(context.Background(), content, meta)
	require.NoError(t, err)
	fs.refMu.Lock()
	defer fs.refMu.Unlock()
	assert.NotContains(t, fs.refCounts, string(ref))
	assert.Equal(t, 1, fs.refCounts[string(again)])
}
