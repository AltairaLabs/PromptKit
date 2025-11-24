package policy_test

import (
"context"
"encoding/json"
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
p := policy.PolicyConfig{
Name:        "delete-after-5min",
Description: "Delete after 5 minutes",
}

err := handler.RegisterPolicy(p)
assert.NoError(t, err)
})

	t.Run("rejects invalid policy", func(t *testing.T) {
p := policy.PolicyConfig{
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
