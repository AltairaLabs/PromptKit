package local_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileStore(t *testing.T) {
	t.Run("creates with valid config", func(t *testing.T) {
		tempDir := t.TempDir()
		config := local.FileStoreConfig{
			BaseDir:             tempDir,
			Organization:        storage.OrganizationByRun,
			EnableDeduplication: true,
		}

		fs, err := local.NewFileStore(config)
		require.NoError(t, err)
		require.NotNil(t, fs)

		// Verify directory was created
		assert.DirExists(t, tempDir)
	})

	t.Run("fails without base directory", func(t *testing.T) {
		config := local.FileStoreConfig{}

		fs, err := local.NewFileStore(config)
		assert.Error(t, err)
		assert.Nil(t, fs)
		assert.Contains(t, err.Error(), "base directory is required")
	})
}

func TestFileStore_StoreMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("stores media with base64 data", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		testData := []byte("test image data")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "test-run",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		assert.NoError(t, err)
		assert.NotEmpty(t, ref)

		// Verify file exists
		assert.FileExists(t, string(ref))
	})

	t.Run("deduplicates identical content", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:             tempDir,
			Organization:        storage.OrganizationByRun,
			EnableDeduplication: true,
		})
		require.NoError(t, err)

		testData := []byte("identical data")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata1 := storage.MediaMetadata{
			RunID:      "test-run-1",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref1, err := fs.StoreMedia(ctx, content, &metadata1)
		require.NoError(t, err)

		metadata2 := storage.MediaMetadata{
			RunID:      "test-run-2",
			MessageIdx: 1,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref2, err := fs.StoreMedia(ctx, content, &metadata2)
		require.NoError(t, err)

		// Should return same reference
		assert.Equal(t, ref1, ref2)
	})
}

func TestFileStore_RetrieveMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("retrieves stored media", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		testData := []byte("test image data")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "test-run",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)

		retrieved, err := fs.RetrieveMedia(ctx, ref)
		assert.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.NotNil(t, retrieved.FilePath)
		assert.Equal(t, "image/jpeg", retrieved.MIMEType)
	})
}

func TestFileStore_DeleteMedia(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes media", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		testData := []byte("test data")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "test-run",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)

		assert.FileExists(t, string(ref))

		err = fs.DeleteMedia(ctx, ref)
		assert.NoError(t, err)

		assert.NoFileExists(t, string(ref))
	})
}

func TestFileStore_GetURL(t *testing.T) {
	ctx := context.Background()

	t.Run("returns file URL", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		testData := []byte("test data")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "test-run",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)

		url, err := fs.GetURL(ctx, ref, 1*time.Hour)
		assert.NoError(t, err)
		assert.Contains(t, url, "file://")
	})
}
