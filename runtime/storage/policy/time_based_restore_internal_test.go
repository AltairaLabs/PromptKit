package policy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var restoreNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// writeRestoreMedia writes media and its .meta at mediaPath with a
// delete-after-5min policy stamped at ts.
func writeRestoreMedia(t *testing.T, mediaPath string, ts time.Time) {
	t.Helper()
	data, err := json.Marshal(storage.MediaMetadata{PolicyName: "delete-after-5min", Timestamp: ts})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mediaPath, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(mediaPath+".meta", data, 0o600))
}

// indexedHandlerWithExpiredMedia returns a handler with a built index holding
// one expired item, and that item's media path.
func indexedHandlerWithExpiredMedia(t *testing.T) (*TimeBasedPolicyHandler, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewTimeBasedPolicyHandler(0, WithClock(func() time.Time { return restoreNow }))
	media := filepath.Join(dir, "reused.jpg")
	writeRestoreMedia(t, media, restoreNow.Add(-6*time.Minute))
	require.NoError(t, h.BuildExpiryIndex(dir))
	require.Equal(t, 1, h.ExpiryIndexLen())
	return h, media
}

func (h *TimeBasedPolicyHandler) indexedExpiry(metaPath string) (time.Time, bool) {
	h.expiryMu.RLock()
	defer h.expiryMu.RUnlock()
	e, ok := h.expiryIndex[metaPath]
	return e.expiresAt, ok
}

// A re-store of the same path that lands after the index pass has taken its
// snapshot, but before it handles the entry, must keep its fresh entry.
func TestEnforceFromIndex_RestoreDuringPassKeepsFreshEntry(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	fresh := restoreNow.Add(5 * time.Minute)
	h.beforeEnforceEntry = func(metaPath string) {
		writeRestoreMedia(t, media, restoreNow) // the store rewrites the files...
		h.TrackFile(metaPath, fresh)            // ...then tracks them
	}

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	got, ok := h.indexedExpiry(media + ".meta")
	assert.True(t, ok, "the re-stored entry must stay in the index")
	assert.Equal(t, fresh, got)
	assert.FileExists(t, media)
}

// When the entry changed between the snapshot and its enforcement, the pass
// must not delete the files on the strength of the stale snapshot.
func TestEnforceFromIndex_ChangedEntrySkipsDeletingFiles(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	extended := restoreNow.Add(time.Hour)
	h.beforeEnforceEntry = func(metaPath string) {
		h.TrackFile(metaPath, extended) // retention extended; disk not yet rewritten
	}

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	assert.FileExists(t, media, "files whose entry changed must not be deleted")
	assert.FileExists(t, media+".meta")
	got, ok := h.indexedExpiry(media + ".meta")
	assert.True(t, ok)
	assert.Equal(t, extended, got)
}

// BuildExpiryIndex must not overwrite an entry tracked while it scanned with
// the stale expiry it read from disk before the re-store.
func TestBuildExpiryIndex_PrefersTrackedEntry(t *testing.T) {
	dir := t.TempDir()
	h := NewTimeBasedPolicyHandler(0, WithClock(func() time.Time { return restoreNow }))
	media := filepath.Join(dir, "reused.jpg")
	writeRestoreMedia(t, media, restoreNow.Add(-6*time.Minute))
	fresh := restoreNow.Add(5 * time.Minute)
	h.TrackFile(media+".meta", fresh)

	require.NoError(t, h.BuildExpiryIndex(dir))

	got, ok := h.indexedExpiry(media + ".meta")
	require.True(t, ok)
	assert.Equal(t, fresh, got)
}

// A re-track that lands while the old files are already gone must not be
// pruned as a stale entry.
func TestEnforceFromIndex_RetrackAfterFilesGoneIsNotPruned(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	fresh := restoreNow.Add(5 * time.Minute)
	h.beforeEnforceEntry = func(metaPath string) {
		require.NoError(t, os.Remove(media))
		require.NoError(t, os.Remove(metaPath))
		h.TrackFile(metaPath, fresh) // tracked before its files land
	}

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))

	got, ok := h.indexedExpiry(media + ".meta")
	assert.True(t, ok, "a fresh entry must not be pruned as stale")
	assert.Equal(t, fresh, got)
}
