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

// dedupPolicyStore returns a deduplicating FileStore with a time-based policy
// handler on a clock the test moves, and the handler.
func dedupPolicyStore(t *testing.T, clock *time.Time) (*local.FileStore, *policy.TimeBasedPolicyHandler, string) {
	t.Helper()
	baseDir := t.TempDir()
	handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return *clock }))
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             baseDir,
		DefaultPolicy:       "delete-after-10min",
		EnableDeduplication: true,
	}, local.WithPolicyHandler(handler))
	require.NoError(t, err)
	return fs, handler, baseDir
}

// TestFileStore_DedupHitOnExpiredMediaExtendsItsRetention checks that a dedup
// hit on media that has expired but not been deleted yet keeps the file for the
// new store's retention, instead of handing out a reference the next
// enforcement pass deletes.
func TestFileStore_DedupHitOnExpiredMediaExtendsItsRetention(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			fs, handler, baseDir := dedupPolicyStore(t, &now)
			src := content(t)

			old := policyMetadata()
			old.Timestamp = now.Add(-time.Hour) // expired, not yet enforced
			_, err := fs.StoreMedia(ctx, src, old)
			require.NoError(t, err)

			fresh := policyMetadata()
			fresh.Timestamp = now
			ref, err := fs.StoreMedia(ctx, src, fresh)
			require.NoError(t, err)

			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			_, err = fs.RetrieveMedia(ctx, ref)
			require.NoError(t, err, "the hit must keep the file for the new store's retention")

			now = now.Add(11 * time.Minute)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.NoFileExists(t, string(ref), "the file still expires at the new store's expiry")
		})
	}
}

// TestFileStore_DedupHitNeverShortensRetention checks that a later store with a
// shorter policy does not cut short the retention an earlier store asked for.
func TestFileStore_DedupHitNeverShortensRetention(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fs, handler, baseDir := dedupPolicyStore(t, &now)
	src := inlineContent(t)

	long := policyMetadata()
	long.Timestamp = now
	long.PolicyName = "retain-1hours"
	ref, err := fs.StoreMedia(ctx, src, long)
	require.NoError(t, err)

	short := policyMetadata()
	short.Timestamp = now
	short.PolicyName = "delete-after-5min"
	_, err = fs.StoreMedia(ctx, src, short)
	require.NoError(t, err)

	now = now.Add(20 * time.Minute)
	require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
	assert.FileExists(t, string(ref), "the earlier store's longer retention must hold")
}

// TestFileStore_OldReferenceDeleteKeepsReStoredMedia checks that after media
// was deleted by policy and stored again, deleting through a reference handed
// out before the deletion does not remove the re-stored copy another caller
// holds, and deleting through the new reference does remove it.
func TestFileStore_OldReferenceDeleteKeepsReStoredMedia(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			fs, handler, baseDir := dedupPolicyStore(t, &now)
			src := content(t)

			old := policyMetadata()
			old.Timestamp = now.Add(-time.Hour)
			oldRef, err := fs.StoreMedia(ctx, src, old)
			require.NoError(t, err)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))

			fresh := policyMetadata()
			fresh.Timestamp = now
			ref, err := fs.StoreMedia(ctx, src, fresh)
			require.NoError(t, err)
			require.NotEqual(t, oldRef, ref, "re-stored content gets a fresh path")

			require.NoError(t, fs.DeleteMedia(ctx, oldRef))
			_, err = fs.RetrieveMedia(ctx, ref)
			require.NoError(t, err, "the re-stored copy is still referenced")

			again, err := fs.StoreMedia(ctx, src, fresh)
			require.NoError(t, err)
			assert.Equal(t, ref, again, "the fresh path is the new dedup target")
		})
	}
}

// TestFileStore_DeleteOfReStoredMediaRemovesIt checks that once media deleted
// by policy has been stored again, deleting through its only live reference
// removes it. A count kept for references to the deleted copy would keep it on
// disk, and with no policy nothing else would ever remove it.
func TestFileStore_DeleteOfReStoredMediaRemovesIt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return now }))
	baseDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true},
		local.WithPolicyHandler(handler))
	require.NoError(t, err)
	src := inlineContent(t)

	old := policyMetadata()
	old.Timestamp = now.Add(-time.Hour)
	old.PolicyName = "delete-after-5min"
	_, err = fs.StoreMedia(ctx, src, old)
	require.NoError(t, err)
	require.NoError(t, handler.EnforcePolicy(ctx, baseDir))

	kept := policyMetadata()
	kept.Timestamp = now // no policy: never expires
	ref, err := fs.StoreMedia(ctx, src, kept)
	require.NoError(t, err)

	require.NoError(t, fs.DeleteMedia(ctx, ref))
	assert.NoFileExists(t, string(ref), "the only live reference's delete removes the file")
	assert.NoFileExists(t, string(ref)+".meta")
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

// TestFileStore_DedupHitChecksTheFileInsideTheWriteGuard checks that a dedup
// hit opens the write guard for the shared file before relying on it, so
// enforcement cannot delete the file between the check and the hit returning.
func TestFileStore_DedupHitChecksTheFileInsideTheWriteGuard(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			handler := &guardRecordingHandler{}
			fs, err := local.NewFileStore(local.FileStoreConfig{
				BaseDir:             t.TempDir(),
				DefaultPolicy:       "delete-after-10min",
				EnableDeduplication: true,
			}, local.WithPolicyHandler(handler))
			require.NoError(t, err)
			src := content(t)

			_, err = fs.StoreMedia(context.Background(), src, policyMetadata())
			require.NoError(t, err)
			handler.events = nil
			_, err = fs.StoreMedia(context.Background(), src, policyMetadata())
			require.NoError(t, err)
			assert.Equal(t, []string{"begin media=yes", "end media=yes meta=yes"}, handler.events)
		})
	}
}

// TestFileStore_DedupHitRetentionWithNoExpiry checks that a file an earlier
// store asked to keep forever is not given a later store's expiry. A later
// store with no policy does not extend a file that has one; see
// TestFileStore_DedupHitWithNoPolicyKeepsTheFilesPolicy.
func TestFileStore_DedupHitRetentionWithNoExpiry(t *testing.T) {
	for name, policies := range map[string][2]string{
		"an earlier store that never expires is kept": {"", "delete-after-5min"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return now }))
			baseDir := t.TempDir()
			fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true},
				local.WithPolicyHandler(handler))
			require.NoError(t, err)
			src := inlineContent(t)

			var ref storage.Reference
			for _, name := range policies {
				meta := policyMetadata()
				meta.Timestamp = now
				meta.PolicyName = name
				ref, err = fs.StoreMedia(ctx, src, meta)
				require.NoError(t, err)
			}

			now = now.Add(10 * time.Minute)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.FileExists(t, string(ref), "a store that never expires keeps the shared file")
		})
	}
}
