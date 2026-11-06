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

// TestFileStore_DedupHitWithNoPolicyKeepsTheFileForever checks that, with no
// DefaultPolicy, a dedup hit that asks for no policy, and so asks to keep the
// media forever, clears the shared file's retention: an enforcement pass after
// the earlier store's finite policy has run out does not delete the file, its
// stale index entry leaves the index, and the earlier store's identity stays.
func TestFileStore_DedupHitWithNoPolicyKeepsTheFileForever(t *testing.T) {
	for name, content := range storeContents {
		for _, indexed := range []bool{false, true} {
			t.Run(name+map[bool]string{false: " walk", true: " index"}[indexed], func(t *testing.T) {
				ctx := context.Background()
				now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(func() time.Time { return now }))
				baseDir := t.TempDir()
				fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true},
					local.WithPolicyHandler(handler))
				require.NoError(t, err)
				if indexed {
					require.NoError(t, handler.BuildExpiryIndex(baseDir))
				}
				src := content(t)

				owner := policyMetadata()
				owner.Timestamp = now
				owner.SessionID = "session-owner"
				owner.PolicyName = "delete-after-5min"
				ref, err := fs.StoreMedia(ctx, src, owner)
				require.NoError(t, err)
				before := readMeta(t, ref)

				forever := policyMetadata()
				forever.Timestamp = now.Add(time.Minute)
				forever.SessionID = "session-hit"
				again, err := fs.StoreMedia(ctx, src, forever)
				require.NoError(t, err)
				require.Equal(t, ref, again)

				after := readMeta(t, ref)
				assert.NotContains(t, after, "policy_name", "the file's retention is cleared to never expire")
				assert.NotContains(t, after, "expires_at")
				assert.Equal(t, before["session_id"], after["session_id"], "the owner's identity stays")
				assert.Equal(t, before["timestamp"], after["timestamp"])

				now = now.Add(6 * time.Minute)
				require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
				assert.FileExists(t, string(ref), "the earlier store's policy no longer deletes the file")
				assert.Equal(t, 0, handler.ExpiryIndexLen(), "the stale entry leaves the index")

				now = now.Add(24 * time.Hour)
				require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
				assert.FileExists(t, string(ref))
			})
		}
	}
}

// TestFileStore_ExpiresAtWithoutAPolicyIsHonored checks that media stored with
// an explicit ExpiresAt and no policy, in a store with no DefaultPolicy, is
// kept until then and deleted after, by both the index pass and the walk;
// and that a dedup hit asking for that extends a file with a shorter policy.
func TestFileStore_ExpiresAtWithoutAPolicyIsHonored(t *testing.T) {
	for name, content := range storeContents {
		for _, indexed := range []bool{false, true} {
			for _, hit := range []bool{false, true} {
				label := name + map[bool]string{false: " walk", true: " index"}[indexed] +
					map[bool]string{false: "", true: " dedup hit"}[hit]
				t.Run(label, func(t *testing.T) {
					ctx := context.Background()
					now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
					handler := policy.NewTimeBasedPolicyHandler(time.Hour,
						policy.WithClock(func() time.Time { return now }))
					baseDir := t.TempDir()
					fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, EnableDeduplication: true},
						local.WithPolicyHandler(handler))
					require.NoError(t, err)
					if indexed {
						require.NoError(t, handler.BuildExpiryIndex(baseDir))
					}
					src := content(t)

					if hit {
						owner := policyMetadata()
						owner.Timestamp = now
						owner.PolicyName = "delete-after-5min"
						_, err = fs.StoreMedia(ctx, src, owner)
						require.NoError(t, err)
					}
					expiresAt := now.Add(time.Hour)
					meta := policyMetadata()
					meta.Timestamp = now
					meta.ExpiresAt = &expiresAt
					ref, err := fs.StoreMedia(ctx, src, meta)
					require.NoError(t, err)

					now = now.Add(30 * time.Minute)
					require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
					require.FileExists(t, string(ref), "kept until ExpiresAt")

					now = now.Add(31 * time.Minute)
					require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
					assert.NoFileExists(t, string(ref), "deleted once ExpiresAt passes, with no policy named")
				})
			}
		}
	}
}
