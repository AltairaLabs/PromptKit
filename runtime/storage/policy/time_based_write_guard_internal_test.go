package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ storage.MediaWriteGuard = (*TimeBasedPolicyHandler)(nil)

// While a store is writing an item, the index pass must leave its files and
// its index entry alone, then enforce it once the write ends.
func TestBeginMediaWrite_BlocksIndexEnforcementUntilEnd(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	ctx := context.Background()

	end := h.BeginMediaWrite(media + ".meta")
	require.NoError(t, h.EnforcePolicy(ctx, filepath.Dir(media)))
	assert.FileExists(t, media, "media being written must not be deleted")
	_, ok := h.indexedExpiry(media + ".meta")
	assert.True(t, ok, "its entry stays for the next pass")

	end()
	end() // safe to call twice
	require.NoError(t, h.EnforcePolicy(ctx, filepath.Dir(media)))
	assert.NoFileExists(t, media, "once the write ends the item is enforced")
}

// The directory walk honors the guard the same way.
func TestBeginMediaWrite_BlocksWalkEnforcementUntilEnd(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	h := NewTimeBasedPolicyHandler(0, WithClock(func() time.Time { return restoreNow }))
	media := filepath.Join(dir, "walked.jpg")
	writeRestoreMedia(t, media, restoreNow.Add(-6*time.Minute))

	end := h.BeginMediaWrite(media + ".meta")
	require.NoError(t, h.EnforcePolicy(ctx, dir))
	assert.FileExists(t, media, "media being written must not be deleted by the walk")

	end()
	require.NoError(t, h.EnforcePolicy(ctx, dir))
	assert.NoFileExists(t, media)
}

// Guarding one item does not hold back enforcement of another.
func TestBeginMediaWrite_GuardsOnlyItsOwnPath(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	end := h.BeginMediaWrite(filepath.Join(filepath.Dir(media), "other.jpg.meta"))
	defer end()

	require.NoError(t, h.EnforcePolicy(context.Background(), filepath.Dir(media)))
	assert.NoFileExists(t, media)
}

// Overlapping writes to one path keep it guarded until the last one ends.
func TestBeginMediaWrite_OverlappingWritesNest(t *testing.T) {
	h, media := indexedHandlerWithExpiredMedia(t)
	ctx := context.Background()

	endFirst := h.BeginMediaWrite(media + ".meta")
	endSecond := h.BeginMediaWrite(media + ".meta")
	endFirst()
	endFirst() // a repeated end must not release the other write
	require.NoError(t, h.EnforcePolicy(ctx, filepath.Dir(media)))
	assert.FileExists(t, media, "the second write is still in progress")

	endSecond()
	require.NoError(t, h.EnforcePolicy(ctx, filepath.Dir(media)))
	assert.NoFileExists(t, media)
}

func TestMediaExpiresAt(t *testing.T) {
	h := NewTimeBasedPolicyHandler(0)

	at, expires, err := h.MediaExpiresAt(&storage.MediaMetadata{PolicyName: "retain-2hours", Timestamp: restoreNow})
	require.NoError(t, err)
	assert.True(t, expires)
	assert.Equal(t, restoreNow.Add(2*time.Hour), at)

	for _, meta := range []*storage.MediaMetadata{nil, {Timestamp: restoreNow}} {
		_, expires, err = h.MediaExpiresAt(meta)
		require.NoError(t, err)
		assert.False(t, expires, "media with no policy never expires")
	}

	_, _, err = h.MediaExpiresAt(&storage.MediaMetadata{PolicyName: "keep-forever"})
	assert.Error(t, err)
}
