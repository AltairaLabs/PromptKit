package local_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/policy"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingPolicyHandler records the policy names it is asked to apply.
type recordingPolicyHandler struct {
	applied []string
	err     error
}

func (h *recordingPolicyHandler) ApplyPolicy(_ context.Context, metadata *storage.MediaMetadata) error {
	h.applied = append(h.applied, metadata.PolicyName)
	return h.err
}

func (h *recordingPolicyHandler) EnforcePolicy(context.Context, string) error { return nil }

func inlineContent(t *testing.T) *types.MediaContent {
	t.Helper()
	data := base64.StdEncoding.EncodeToString([]byte("policy test data"))
	return &types.MediaContent{Data: &data, MIMEType: "image/jpeg"}
}

func fileContent(t *testing.T) *types.MediaContent {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.bin")
	require.NoError(t, os.WriteFile(path, []byte("streamed policy data"), 0o600))
	return &types.MediaContent{FilePath: &path, MIMEType: "image/jpeg"}
}

func policyMetadata() *storage.MediaMetadata {
	return &storage.MediaMetadata{RunID: "run", SessionID: "session", MIMEType: "image/jpeg", Timestamp: time.Now()}
}

func TestFileStore_AppliesPolicyHandler(t *testing.T) {
	ctx := context.Background()

	for name, content := range map[string]func(*testing.T) *types.MediaContent{
		"inline":    inlineContent,
		"streaming": fileContent,
	} {
		t.Run(name+" applies the default policy", func(t *testing.T) {
			handler := &recordingPolicyHandler{}
			fs, err := local.NewFileStore(local.FileStoreConfig{
				BaseDir:       t.TempDir(),
				DefaultPolicy: "delete-after-10min",
			}, local.WithPolicyHandler(handler))
			require.NoError(t, err)

			_, err = fs.StoreMedia(ctx, content(t), policyMetadata())
			require.NoError(t, err)
			assert.Equal(t, []string{"delete-after-10min"}, handler.applied)
		})

		t.Run(name+" fails when the policy is rejected", func(t *testing.T) {
			baseDir := t.TempDir()
			rejected := errors.New("unknown policy")
			fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir},
				local.WithPolicyHandler(&recordingPolicyHandler{err: rejected}))
			require.NoError(t, err)

			meta := policyMetadata()
			meta.PolicyName = "keep-forever"
			ref, err := fs.StoreMedia(ctx, content(t), meta)
			require.ErrorIs(t, err, rejected)
			assert.Empty(t, ref)
		})
	}
}

func TestFileStore_TimeBasedPolicyHandlerRejectsUnknownPolicy(t *testing.T) {
	ctx := context.Background()
	handler := policy.NewTimeBasedPolicyHandler(0)

	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:       t.TempDir(),
		DefaultPolicy: "keep-forever",
	}, local.WithPolicyHandler(handler))
	require.NoError(t, err)
	_, err = fs.StoreMedia(ctx, inlineContent(t), policyMetadata())
	require.Error(t, err, "a policy the handler cannot enforce must not be written")

	// Without a handler the store keeps today's behavior and writes the name.
	plain, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:       t.TempDir(),
		DefaultPolicy: "keep-forever",
	})
	require.NoError(t, err)
	_, err = plain.StoreMedia(ctx, inlineContent(t), policyMetadata())
	require.NoError(t, err)
}

// TestFileStore_StoringMediaKeepsEnforcingMediaFromEarlierRuns checks that
// storing one item through the store does not narrow EnforcePolicy to the
// items tracked since the handler was created: expired media an earlier run
// left on disk is still found and deleted when the host calls EnforcePolicy
// itself, without StartEnforcement.
func TestFileStore_StoringMediaKeepsEnforcingMediaFromEarlierRuns(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	// An earlier run stored this media; its policy expired an hour ago.
	earlier, err := local.NewFileStore(local.FileStoreConfig{BaseDir: baseDir, DefaultPolicy: "delete-after-10min"})
	require.NoError(t, err)
	old := policyMetadata()
	old.SessionID = "earlier-run"
	old.Timestamp = now.Add(-70 * time.Minute)
	oldRef, err := earlier.StoreMedia(ctx, inlineContent(t), old)
	require.NoError(t, err)
	require.FileExists(t, string(oldRef))

	handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(clock))
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:       baseDir,
		DefaultPolicy: "delete-after-10min",
	}, local.WithPolicyHandler(handler))
	require.NoError(t, err)
	fresh := policyMetadata()
	fresh.SessionID = "this-run"
	fresh.Timestamp = now
	freshRef, err := fs.StoreMedia(ctx, inlineContent(t), fresh)
	require.NoError(t, err)

	require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
	assert.NoFileExists(t, string(oldRef), "expired media from an earlier run must still be enforced")
	assert.NoFileExists(t, string(oldRef)+".meta")
	assert.FileExists(t, string(freshRef), "unexpired media must survive")
}

// TestFileStore_MediaStoredAfterEnforcementStartsExpires checks that media
// stored after StartEnforcement has built its expiry index is tracked, so
// index-based enforcement deletes it once its policy expires.
func TestFileStore_MediaStoredAfterEnforcementStartsExpires(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var nowNanos atomic.Int64
	nowNanos.Store(start.UnixNano())
	clock := func() time.Time { return time.Unix(0, nowNanos.Load()).UTC() }

	// A long interval keeps the background ticker from firing; the test
	// calls EnforcePolicy itself.
	handler := policy.NewTimeBasedPolicyHandler(time.Hour, policy.WithClock(clock))
	handler.StartEnforcement(ctx, baseDir)
	t.Cleanup(handler.Stop)

	for name, content := range map[string]func(*testing.T) *types.MediaContent{
		"inline":    inlineContent,
		"streaming": fileContent,
	} {
		t.Run(name, func(t *testing.T) {
			fs, err := local.NewFileStore(local.FileStoreConfig{
				BaseDir:       baseDir,
				DefaultPolicy: "delete-after-10min",
			}, local.WithPolicyHandler(handler))
			require.NoError(t, err)

			meta := policyMetadata()
			meta.SessionID = "session-" + name
			meta.Timestamp = clock()
			ref, err := fs.StoreMedia(ctx, content(t), meta)
			require.NoError(t, err)
			require.FileExists(t, string(ref))

			nowNanos.Add(int64(5 * time.Minute))
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.FileExists(t, string(ref), "media must survive before its policy expires")

			nowNanos.Add(int64(6 * time.Minute))
			require.NoError(t, handler.EnforcePolicy(ctx, baseDir))
			assert.NoFileExists(t, string(ref), "media stored after enforcement started must expire")
			assert.NoFileExists(t, string(ref)+".meta")
		})
	}
}
