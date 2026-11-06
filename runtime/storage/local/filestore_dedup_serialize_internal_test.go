package local

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockProbeHandler reports expiry times and records, each time the store asks,
// whether the store's dedup lock was free at that moment.
type lockProbeHandler struct {
	fs        *FileStore
	lockFree  []bool
	expiresAt time.Time
}

func (h *lockProbeHandler) ApplyPolicy(context.Context, *storage.MediaMetadata) error { return nil }
func (h *lockProbeHandler) EnforcePolicy(context.Context, string) error               { return nil }

func (h *lockProbeHandler) MediaExpiresAt(*storage.MediaMetadata) (time.Time, bool, error) {
	if h.fs.dedupMu.TryLock() {
		h.fs.dedupMu.Unlock()
		h.lockFree = append(h.lockFree, true)
	} else {
		h.lockFree = append(h.lockFree, false)
	}
	return h.expiresAt, true, nil
}

// A dedup hit compares and rewrites the shared file's metadata while it holds
// dedupMu, so two hits on one file cannot interleave their load, compare and
// write and leave the shorter retention, or a torn file, behind.
func TestDedupHit_ExtendsRetentionUnderTheDedupLock(t *testing.T) {
	for name, content := range map[string]func() *types.MediaContent{
		"inline": func() *types.MediaContent {
			data := base64.StdEncoding.EncodeToString([]byte("serialized"))
			return &types.MediaContent{Data: &data, MIMEType: "image/jpeg"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			handler := &lockProbeHandler{expiresAt: time.Now()}
			fs, err := NewFileStore(FileStoreConfig{
				BaseDir: t.TempDir(), DefaultPolicy: "p", EnableDeduplication: true,
			}, WithPolicyHandler(handler))
			require.NoError(t, err)
			handler.fs = fs
			meta := func() *storage.MediaMetadata {
				return &storage.MediaMetadata{SessionID: "s", MIMEType: "image/jpeg", Timestamp: time.Now()}
			}

			_, err = fs.StoreMedia(context.Background(), content(), meta())
			require.NoError(t, err)
			_, err = fs.StoreMedia(context.Background(), content(), meta())
			require.NoError(t, err)

			require.NotEmpty(t, handler.lockFree, "the hit asks the handler for expiry times")
			for _, free := range handler.lockFree {
				assert.False(t, free, "the retention update runs under dedupMu")
			}
		})
	}
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
