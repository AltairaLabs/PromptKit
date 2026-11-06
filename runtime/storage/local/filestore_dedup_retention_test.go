package local_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readMeta decodes the metadata file stored next to the media at ref.
func readMeta(t *testing.T, ref storage.Reference) map[string]any {
	t.Helper()
	data, err := os.ReadFile(string(ref) + ".meta")
	require.NoError(t, err)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(data, &meta))
	return meta
}

// TestFileStore_DedupHitExtendsOnlyTheRetention checks that a dedup hit that
// extends a shared file's retention changes only its retention, the policy and
// the expiry, and leaves the identity of the store that wrote the file alone.
func TestFileStore_DedupHitExtendsOnlyTheRetention(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			fs, handler, baseDir := dedupPolicyStore(t, &now)
			src := content(t)

			owner := &storage.MediaMetadata{
				RunID: "run-owner", ConversationID: "conv-owner", SessionID: "session-owner",
				MessageIdx: 1, PartIdx: 2, MIMEType: "image/jpeg", ProviderID: "provider-owner",
				Timestamp: now.Add(-50 * time.Minute), PolicyName: "delete-after-5min",
			}
			ref, err := fs.StoreMedia(ctx, src, owner)
			require.NoError(t, err)
			before := readMeta(t, ref)

			hit := &storage.MediaMetadata{
				RunID: "run-hit", ConversationID: "conv-hit", SessionID: "session-hit",
				MessageIdx: 7, PartIdx: 8, MIMEType: "image/jpeg", ProviderID: "provider-hit",
				Timestamp: now, PolicyName: "retain-1hours",
			}
			again, err := fs.StoreMedia(ctx, src, hit)
			require.NoError(t, err)
			require.Equal(t, ref, again)

			after := readMeta(t, ref)
			assert.Equal(t, "retain-1hours", after["policy_name"], "the hit's longer policy is recorded")
			for _, field := range []string{
				"run_id", "conversation_id", "session_id", "message_idx", "part_idx",
				"mime_type", "size_bytes", "provider_id", "timestamp",
			} {
				assert.Equal(t, before[field], after[field], "%s must stay the owner's", field)
			}

			// The file is kept for the hit's retention, which is measured from
			// the hit's store, and then expires.
			now = now.Add(30 * time.Minute)
			require.NoError(t, handler.BuildExpiryIndex(baseDir))
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			require.FileExists(t, string(ref), "the hit's retention is measured from the hit, not the owner")

			now = now.Add(31 * time.Minute)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.NoFileExists(t, string(ref), "the file expires an hour after the hit")
		})
	}
}

// TestFileStore_DedupHitWithNoPolicyKeepsTheFilesPolicy checks that, with no
// DefaultPolicy, a dedup hit that asks for no policy does not count as
// outlasting a shared file that has one: the .meta keeps its policy, so the
// file is enforced and leaves the expiry index when it expires.
func TestFileStore_DedupHitWithNoPolicyKeepsTheFilesPolicy(t *testing.T) {
	for name, content := range storeContents {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return now }))
			baseDir := t.TempDir()
			fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true},
				local.WithPolicyHandler(handler))
			require.NoError(t, err)
			require.NoError(t, handler.BuildExpiryIndex(baseDir))
			src := content(t)

			owner := policyMetadata()
			owner.Timestamp = now
			owner.PolicyName = "delete-after-5min"
			ref, err := fs.StoreMedia(ctx, src, owner)
			require.NoError(t, err)

			noPolicy := policyMetadata()
			noPolicy.Timestamp = now
			_, err = fs.StoreMedia(ctx, src, noPolicy)
			require.NoError(t, err)
			assert.Equal(t, "delete-after-5min", readMeta(t, ref)["policy_name"],
				"a hit with no policy must not write a .meta without one")

			now = now.Add(6 * time.Minute)
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.NoFileExists(t, string(ref), "the file expires under its own policy")
			assert.Equal(t, 0, handler.ExpiryIndexLen(), "the expired entry leaves the index")
		})
	}
}
