package local_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/policy"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var storeContents = map[string]func(*testing.T) *types.MediaContent{
	"inline":    inlineContent,
	"streaming": fileContent,
}

// TestFileStore_DedupAfterPolicyDeletionStoresAgain checks that once the
// retention policy has deleted deduplicated media, storing the same content
// again writes it back instead of returning a reference to the deleted file.
func TestFileStore_DedupAfterPolicyDeletionStoresAgain(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			baseDir := t.TempDir()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return now }))
			fs, err := local.NewFileStore(local.FileStoreConfig{
				BaseDir:             baseDir,
				DefaultPolicy:       "delete-after-10min",
				EnableDeduplication: true,
			}, local.WithPolicyHandler(handler))
			require.NoError(t, err)

			old := policyMetadata()
			old.Timestamp = now.Add(-time.Hour)
			src := content(t)
			ref, err := fs.StoreMedia(ctx, src, old)
			require.NoError(t, err)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			require.NoFileExists(t, string(ref))

			fresh := policyMetadata()
			fresh.Timestamp = now
			again, err := fs.StoreMedia(ctx, src, fresh)
			require.NoError(t, err)
			got, err := fs.RetrieveMedia(ctx, again)
			require.NoError(t, err, "a dedup hit on deleted media must store the content again")
			require.NotNil(t, got)
			assert.FileExists(t, string(again)+".meta")

			// The re-stored item is enforced at its own expiry.
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.FileExists(t, string(again))
		})
	}
}

// guardRecordingHandler records the order in which a store calls the write
// guard and the tracker, and what was on disk at each step.
type guardRecordingHandler struct {
	events []string
}

func (h *guardRecordingHandler) ApplyPolicy(context.Context, *storage.MediaMetadata) error {
	return nil
}
func (h *guardRecordingHandler) EnforcePolicy(context.Context, string) error { return nil }

func (h *guardRecordingHandler) TrackMedia(string, *storage.MediaMetadata) error {
	h.events = append(h.events, "track")
	return nil
}

func (h *guardRecordingHandler) BeginMediaWrite(metaPath string) func() {
	h.events = append(h.events, "begin media="+exists(strings.TrimSuffix(metaPath, ".meta")))
	return func() {
		h.events = append(h.events,
			"end media="+exists(strings.TrimSuffix(metaPath, ".meta"))+" meta="+exists(metaPath))
	}
}

func exists(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "yes"
	}
	return "no"
}

// TestFileStore_WritesAndTracksInsideTheWriteGuard checks that a store opens
// the handler's write guard before it writes a media item's files and closes
// it only after it has tracked them, so enforcement cannot delete files that
// are being replaced.
func TestFileStore_WritesAndTracksInsideTheWriteGuard(t *testing.T) {
	for name, content := range storeContents {
		for _, dedup := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "", true: " dedup"}[dedup], func(t *testing.T) {
				handler := &guardRecordingHandler{}
				fs, err := local.NewFileStore(local.FileStoreConfig{
					BaseDir:             t.TempDir(),
					DefaultPolicy:       "delete-after-10min",
					EnableDeduplication: dedup,
				}, local.WithPolicyHandler(handler))
				require.NoError(t, err)

				_, err = fs.StoreMedia(context.Background(), content(t), policyMetadata())
				require.NoError(t, err)
				assert.Equal(t, []string{"begin media=no", "track", "end media=yes meta=yes"}, handler.events)
			})
		}
	}
}
