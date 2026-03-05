package policy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTimeBasedPolicyHandler(t *testing.T) {
	handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)
	assert.NotNil(t, handler)
}

func TestTimeBasedPolicyHandler_RegisterPolicy(t *testing.T) {
	handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

	t.Run("registers valid policy", func(t *testing.T) {
		p := policy.Config{
			Name:        "delete-after-5min",
			Description: "Delete after 5 minutes",
		}

		err := handler.RegisterPolicy(p)
		assert.NoError(t, err)
	})

	t.Run("rejects invalid policy", func(t *testing.T) {
		p := policy.Config{
			Name: "invalid-format",
		}

		err := handler.RegisterPolicy(p)
		assert.Error(t, err)
	})
}

func TestTimeBasedPolicyHandler_ApplyPolicy(t *testing.T) {
	ctx := context.Background()
	handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

	t.Run("applies valid policy", func(t *testing.T) {
		now := time.Now()
		metadata := &storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  now,
		}

		err := handler.ApplyPolicy(ctx, metadata)
		assert.NoError(t, err)
		assert.Equal(t, "delete-after-5min", metadata.PolicyName)
	})

	t.Run("handles empty policy name", func(t *testing.T) {
		metadata := &storage.MediaMetadata{
			PolicyName: "",
			Timestamp:  time.Now(),
		}

		err := handler.ApplyPolicy(ctx, metadata)
		assert.NoError(t, err) // Should not error on empty policy
	})

	t.Run("fails on invalid policy name", func(t *testing.T) {
		metadata := &storage.MediaMetadata{
			PolicyName: "invalid-format",
			Timestamp:  time.Now(),
		}

		err := handler.ApplyPolicy(ctx, metadata)
		assert.Error(t, err)
	})
}

func TestTimeBasedPolicyHandler_EnforcePolicy(t *testing.T) {
	ctx := context.Background()
	handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

	t.Run("deletes expired media", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a media file and .meta file with expired policy
		mediaPath := filepath.Join(tempDir, "expired.jpg")
		err := os.WriteFile(mediaPath, []byte("test data"), 0600)
		require.NoError(t, err)

		// Create metadata that expired 1 minute ago
		metadata := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute), // Expired
			MIMEType:   "image/jpeg",
		}

		metaPath := mediaPath + ".meta"
		metaData, err := json.Marshal(metadata)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Run enforcement
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		// Verify files are deleted
		assert.NoFileExists(t, mediaPath)
		assert.NoFileExists(t, metaPath)
	})

	t.Run("keeps non-expired media", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a media file and .meta file with non-expired policy
		mediaPath := filepath.Join(tempDir, "active.jpg")
		err := os.WriteFile(mediaPath, []byte("test data"), 0600)
		require.NoError(t, err)

		// Create metadata that expires in 4 minutes
		metadata := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-1 * time.Minute), // Not yet expired
			MIMEType:   "image/jpeg",
		}

		metaPath := mediaPath + ".meta"
		metaData, err := json.Marshal(metadata)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Run enforcement
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		// Verify files still exist
		assert.FileExists(t, mediaPath)
		assert.FileExists(t, metaPath)
	})

	t.Run("handles missing media file gracefully", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create only .meta file (media file missing)
		metadata := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}

		metaPath := filepath.Join(tempDir, "missing.jpg.meta")
		metaData, err := json.Marshal(metadata)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Run enforcement - should not error
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		// .meta file should be deleted
		assert.NoFileExists(t, metaPath)
	})

	t.Run("handles corrupt .meta file", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create corrupt .meta file
		metaPath := filepath.Join(tempDir, "corrupt.jpg.meta")
		err := os.WriteFile(metaPath, []byte("not json"), 0600)
		require.NoError(t, err)

		// Run enforcement - should not error
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		// Corrupt file should still exist (we don't delete files we can't read)
		assert.FileExists(t, metaPath)
	})
}

func TestTimeBasedPolicyHandler_StartEnforcement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tempDir := t.TempDir()
	handler := policy.NewTimeBasedPolicyHandler(100 * time.Millisecond)

	// Create an expired media file
	mediaPath := filepath.Join(tempDir, "expire-soon.jpg")
	err := os.WriteFile(mediaPath, []byte("test data"), 0600)
	require.NoError(t, err)

	metadata := storage.MediaMetadata{
		PolicyName: "delete-after-5min",
		Timestamp:  time.Now().Add(-6 * time.Minute), // Already expired
		MIMEType:   "image/jpeg",
	}

	metaPath := mediaPath + ".meta"
	metaData, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = os.WriteFile(metaPath, metaData, 0600)
	require.NoError(t, err)

	// Start background enforcement
	handler.StartEnforcement(ctx, tempDir)

	// Wait for enforcement to run
	time.Sleep(250 * time.Millisecond)

	// Verify files are deleted by background process
	assert.NoFileExists(t, mediaPath)
	assert.NoFileExists(t, metaPath)

	// Stop enforcement
	handler.Stop()
}

func TestTimeBasedPolicyHandler_AutoStart(t *testing.T) {
	t.Run("auto-starts enforcement when configured", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create an expired media file before constructing the handler
		mediaPath := filepath.Join(tempDir, "auto-expire.jpg")
		err := os.WriteFile(mediaPath, []byte("test data"), 0600)
		require.NoError(t, err)

		metadata := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}

		metaPath := mediaPath + ".meta"
		metaData, err := json.Marshal(metadata)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Create handler with auto-start — enforcement should begin immediately
		handler := policy.NewTimeBasedPolicyHandler(
			100*time.Millisecond,
			policy.WithAutoStart(true),
			policy.WithBaseDir(tempDir),
		)

		// Wait for enforcement to run
		time.Sleep(250 * time.Millisecond)

		// Verify files are deleted by auto-started background process
		assert.NoFileExists(t, mediaPath)
		assert.NoFileExists(t, metaPath)

		handler.Stop()
	})

	t.Run("does not auto-start without WithAutoStart", func(t *testing.T) {
		tempDir := t.TempDir()

		mediaPath := filepath.Join(tempDir, "no-auto.jpg")
		err := os.WriteFile(mediaPath, []byte("test data"), 0600)
		require.NoError(t, err)

		metadata := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}

		metaPath := mediaPath + ".meta"
		metaData, err := json.Marshal(metadata)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Create handler without auto-start
		_ = policy.NewTimeBasedPolicyHandler(
			100*time.Millisecond,
			policy.WithBaseDir(tempDir),
		)

		// Wait to confirm enforcement does NOT run
		time.Sleep(250 * time.Millisecond)

		// Files should still exist
		assert.FileExists(t, mediaPath)
		assert.FileExists(t, metaPath)
	})

	t.Run("does not auto-start without base dir", func(t *testing.T) {
		// Should not panic or start enforcement when base dir is missing
		handler := policy.NewTimeBasedPolicyHandler(
			100*time.Millisecond,
			policy.WithAutoStart(true),
		)
		assert.NotNil(t, handler)
	})
}

func TestTimeBasedPolicyHandler_StopIdempotent(t *testing.T) {
	t.Run("stop is safe when enforcement never started", func(t *testing.T) {
		handler := policy.NewTimeBasedPolicyHandler(100 * time.Millisecond)
		// Should not panic
		handler.Stop()
	})

	t.Run("stop is safe to call multiple times", func(t *testing.T) {
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(
			100*time.Millisecond,
			policy.WithAutoStart(true),
			policy.WithBaseDir(tempDir),
		)

		handler.Stop()
		// Second call should not panic
		handler.Stop()
	})
}

func TestTimeBasedPolicyHandler_StartEnforcementIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tempDir := t.TempDir()
	handler := policy.NewTimeBasedPolicyHandler(100 * time.Millisecond)

	// Calling StartEnforcement twice should not panic or start two goroutines
	handler.StartEnforcement(ctx, tempDir)
	handler.StartEnforcement(ctx, tempDir)

	handler.Stop()
}

func TestTimeBasedPolicyHandler_BuildExpiryIndex(t *testing.T) {
	t.Run("builds index from existing meta files", func(t *testing.T) {
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		// Create 3 meta files: 2 expired, 1 active
		for i, offset := range []time.Duration{-6, -10, -1} {
			mediaPath := filepath.Join(tempDir, fmt.Sprintf("file%d.jpg", i))
			err := os.WriteFile(mediaPath, []byte("data"), 0600)
			require.NoError(t, err)

			meta := storage.MediaMetadata{
				PolicyName: "delete-after-5min",
				Timestamp:  time.Now().Add(offset * time.Minute),
				MIMEType:   "image/jpeg",
			}
			metaData, err := json.Marshal(meta)
			require.NoError(t, err)
			err = os.WriteFile(mediaPath+".meta", metaData, 0600)
			require.NoError(t, err)
		}

		err := handler.BuildExpiryIndex(tempDir)
		require.NoError(t, err)

		// All 3 files should be in the index (they all have policies)
		assert.Equal(t, 3, handler.ExpiryIndexLen())
	})

	t.Run("skips files without policy", func(t *testing.T) {
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		// Create a meta file without a policy name
		metaPath := filepath.Join(tempDir, "nopolicy.jpg.meta")
		meta := storage.MediaMetadata{
			MIMEType:  "image/jpeg",
			Timestamp: time.Now(),
		}
		metaData, err := json.Marshal(meta)
		require.NoError(t, err)
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		err = handler.BuildExpiryIndex(tempDir)
		require.NoError(t, err)
		assert.Equal(t, 0, handler.ExpiryIndexLen())
	})

	t.Run("handles corrupt meta files gracefully", func(t *testing.T) {
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		metaPath := filepath.Join(tempDir, "corrupt.jpg.meta")
		err := os.WriteFile(metaPath, []byte("not json"), 0600)
		require.NoError(t, err)

		err = handler.BuildExpiryIndex(tempDir)
		require.NoError(t, err)
		assert.Equal(t, 0, handler.ExpiryIndexLen())
	})
}

func TestTimeBasedPolicyHandler_TrackFile(t *testing.T) {
	t.Run("adds file to expiry index", func(t *testing.T) {
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		expiresAt := time.Now().Add(5 * time.Minute)
		handler.TrackFile("/tmp/test.jpg.meta", expiresAt)

		assert.Equal(t, 1, handler.ExpiryIndexLen())
	})

	t.Run("updates existing entry", func(t *testing.T) {
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		handler.TrackFile("/tmp/test.jpg.meta", time.Now().Add(5*time.Minute))
		handler.TrackFile("/tmp/test.jpg.meta", time.Now().Add(10*time.Minute))

		assert.Equal(t, 1, handler.ExpiryIndexLen())
	})
}

func TestTimeBasedPolicyHandler_UntrackFile(t *testing.T) {
	t.Run("removes file from expiry index", func(t *testing.T) {
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		handler.TrackFile("/tmp/test.jpg.meta", time.Now().Add(5*time.Minute))
		assert.Equal(t, 1, handler.ExpiryIndexLen())

		handler.UntrackFile("/tmp/test.jpg.meta")
		assert.Equal(t, 0, handler.ExpiryIndexLen())
	})

	t.Run("no-op for unknown file", func(t *testing.T) {
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)
		handler.UntrackFile("/tmp/nonexistent.meta") // should not panic
		assert.Equal(t, 0, handler.ExpiryIndexLen())
	})
}

func TestTimeBasedPolicyHandler_IndexedEnforcement(t *testing.T) {
	t.Run("deletes only expired files using index", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		// Create expired file
		expiredMedia := filepath.Join(tempDir, "expired.jpg")
		err := os.WriteFile(expiredMedia, []byte("expired"), 0600)
		require.NoError(t, err)

		expiredMeta := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}
		expiredMetaData, err := json.Marshal(expiredMeta)
		require.NoError(t, err)
		expiredMetaPath := expiredMedia + ".meta"
		err = os.WriteFile(expiredMetaPath, expiredMetaData, 0600)
		require.NoError(t, err)

		// Create active file
		activeMedia := filepath.Join(tempDir, "active.jpg")
		err = os.WriteFile(activeMedia, []byte("active"), 0600)
		require.NoError(t, err)

		activeMeta := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-1 * time.Minute),
			MIMEType:   "image/jpeg",
		}
		activeMetaData, err := json.Marshal(activeMeta)
		require.NoError(t, err)
		activeMetaPath := activeMedia + ".meta"
		err = os.WriteFile(activeMetaPath, activeMetaData, 0600)
		require.NoError(t, err)

		// Build index
		err = handler.BuildExpiryIndex(tempDir)
		require.NoError(t, err)
		assert.Equal(t, 2, handler.ExpiryIndexLen())

		// Enforce using index
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		// Expired file should be deleted, active file should remain
		assert.NoFileExists(t, expiredMedia)
		assert.NoFileExists(t, expiredMetaPath)
		assert.FileExists(t, activeMedia)
		assert.FileExists(t, activeMetaPath)

		// Index should have removed the expired entry
		assert.Equal(t, 1, handler.ExpiryIndexLen())
	})

	t.Run("enforcement with tracked files deletes expired", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(1 * time.Minute)

		// Create a media file
		mediaPath := filepath.Join(tempDir, "tracked.jpg")
		err := os.WriteFile(mediaPath, []byte("data"), 0600)
		require.NoError(t, err)

		meta := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}
		metaData, err := json.Marshal(meta)
		require.NoError(t, err)
		metaPath := mediaPath + ".meta"
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Track the file manually (as would happen during store)
		handler.TrackFile(metaPath, time.Now().Add(-1*time.Minute)) // already expired

		// Enforce
		err = handler.EnforcePolicy(ctx, tempDir)
		assert.NoError(t, err)

		assert.NoFileExists(t, mediaPath)
		assert.NoFileExists(t, metaPath)
		assert.Equal(t, 0, handler.ExpiryIndexLen())
	})

	t.Run("StartEnforcement builds index and uses it", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tempDir := t.TempDir()
		handler := policy.NewTimeBasedPolicyHandler(100 * time.Millisecond)

		// Create an expired file
		mediaPath := filepath.Join(tempDir, "indexed-expire.jpg")
		err := os.WriteFile(mediaPath, []byte("data"), 0600)
		require.NoError(t, err)

		meta := storage.MediaMetadata{
			PolicyName: "delete-after-5min",
			Timestamp:  time.Now().Add(-6 * time.Minute),
			MIMEType:   "image/jpeg",
		}
		metaData, err := json.Marshal(meta)
		require.NoError(t, err)
		metaPath := mediaPath + ".meta"
		err = os.WriteFile(metaPath, metaData, 0600)
		require.NoError(t, err)

		// Start enforcement - should build index on startup
		handler.StartEnforcement(ctx, tempDir)

		// Wait for enforcement to run
		time.Sleep(250 * time.Millisecond)

		// File should be deleted
		assert.NoFileExists(t, mediaPath)
		assert.NoFileExists(t, metaPath)

		handler.Stop()
	})
}
