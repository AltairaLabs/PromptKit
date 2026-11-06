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

// An index entry whose .meta has lost its policy describes media that never
// expires. The index pass drops the entry, leaves the files alone and does not
// count it as a failure, rather than failing on it on every tick.
func TestEnforceFromIndex_DropsEntryWhoseMetaHasNoPolicy(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	data, err := json.Marshal(storage.MediaMetadata{SessionID: "s", Timestamp: restoreNow.Add(-time.Hour)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(media+".meta", data, 0o600)) // rewritten with no policy

	enforced, failed := h.enforceEntry(h.snapshotEntry(t, media+".meta"), restoreNow)
	assert.False(t, enforced)
	assert.False(t, failed, "media with no policy is not a failure")
	assert.Equal(t, 0, h.ExpiryIndexLen(), "the entry leaves the index")
	assert.FileExists(t, media, "media with no policy is never deleted")
	assert.FileExists(t, media+".meta")
}

// The directory walk treats a .meta with no policy as media that never
// expires: nothing to do, and not a failure.
func TestEnforceWalkedFile_NoPolicyIsNotAFailure(t *testing.T) {
	h := NewTimeBasedPolicyHandler(0, WithClock(func() time.Time { return restoreNow }))
	media := filepath.Join(t.TempDir(), "kept.jpg")
	data, err := json.Marshal(storage.MediaMetadata{SessionID: "s", Timestamp: restoreNow.Add(-time.Hour)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(media, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(media+".meta", data, 0o600))

	_, failed := h.enforceWalkedFile(media+".meta", restoreNow)
	assert.False(t, failed)
	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))
	assert.FileExists(t, media)
}

// snapshotEntry returns the index entry for metaPath, as enforceFromIndex's
// snapshot holds it.
func (h *TimeBasedPolicyHandler) snapshotEntry(t *testing.T, metaPath string) expiryEntry {
	t.Helper()
	h.expiryMu.RLock()
	defer h.expiryMu.RUnlock()
	e, ok := h.expiryIndex[metaPath]
	require.True(t, ok)
	return e
}
