package local_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
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

	t.Run("fails for non-existent media", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		url, err := fs.GetURL(ctx, storage.Reference("/nonexistent.jpg"), 1*time.Hour)
		assert.Error(t, err)
		assert.Empty(t, url)
		assert.Contains(t, err.Error(), "media not found")
	})
}

func TestFileStore_OrganizationModes(t *testing.T) {
	ctx := context.Background()
	testData := []byte("test data")
	base64Data := base64.StdEncoding.EncodeToString(testData)

	t.Run("organizes by session", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationBySession,
		})
		require.NoError(t, err)

		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/png",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-1",
			SessionID:  "session-abc",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/png",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)
		assert.Contains(t, string(ref), "sessions")
		assert.Contains(t, string(ref), "session-abc")
	})

	t.Run("organizes by conversation", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByConversation,
		})
		require.NoError(t, err)

		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "audio/mp3",
		}

		metadata := storage.MediaMetadata{
			RunID:          "run-1",
			ConversationID: "conv-xyz",
			MessageIdx:     0,
			PartIdx:        0,
			MIMEType:       "audio/mp3",
			SizeBytes:      int64(len(testData)),
			Timestamp:      time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)
		assert.Contains(t, string(ref), "conversations")
		assert.Contains(t, string(ref), "conv-xyz")
	})

	t.Run("fails without required session ID", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationBySession,
		})
		require.NoError(t, err)

		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-1",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		assert.Error(t, err)
		assert.Empty(t, ref)
		assert.Contains(t, err.Error(), "session ID required")
	})

	t.Run("fails without required conversation ID", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByConversation,
		})
		require.NoError(t, err)

		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-1",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		assert.Error(t, err)
		assert.Empty(t, ref)
		assert.Contains(t, err.Error(), "conversation ID required")
	})
}

func TestFileStore_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("fails with invalid content", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		content := &types.MediaContent{
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-1",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  0,
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		assert.Error(t, err)
		assert.Empty(t, ref)
		assert.Contains(t, err.Error(), "invalid media content")
	})

	t.Run("retrieve fails for non-existent file", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		retrieved, err := fs.RetrieveMedia(ctx, storage.Reference("/nonexistent/file.jpg"))
		assert.Error(t, err)
		assert.Nil(t, retrieved)
		assert.Contains(t, err.Error(), "media not found")
	})

	t.Run("delete succeeds for non-existent file", func(t *testing.T) {
		tempDir := t.TempDir()
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		err = fs.DeleteMedia(ctx, storage.Reference("/nonexistent/file.jpg"))
		assert.NoError(t, err) // Should not error
	})
}

func TestFileStore_MIMETypes(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	testCases := []struct {
		name     string
		mimeType string
		wantExt  string
	}{
		{"JPEG", "image/jpeg", ".jpg"},
		{"PNG", "image/png", ".png"},
		{"GIF", "image/gif", ".gif"},
		{"WebP", "image/webp", ".webp"},
		{"MP3", "audio/mpeg", ".mp3"},
		{"WAV", "audio/wav", ".wav"},
		{"OGG", "audio/ogg", ".ogg"},
		{"WebM Audio", "audio/webm", ".weba"},
		{"MP4", "video/mp4", ".mp4"},
		{"WebM Video", "video/webm", ".webm"},
		{"OGV", "video/ogg", ".ogv"},
		{"Unknown", "application/octet-stream", ".bin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testData := []byte("test " + tc.name)
			base64Data := base64.StdEncoding.EncodeToString(testData)
			content := &types.MediaContent{
				Data:     &base64Data,
				MIMEType: tc.mimeType,
			}

			metadata := storage.MediaMetadata{
				RunID:      "run-mime-test",
				MessageIdx: 0,
				PartIdx:    0,
				MIMEType:   tc.mimeType,
				SizeBytes:  int64(len(testData)),
				Timestamp:  time.Now(),
			}

			ref, err := fs.StoreMedia(ctx, content, &metadata)
			require.NoError(t, err)
			assert.Contains(t, string(ref), tc.wantExt)
		})
	}
}

func TestFileStore_DedupReferenceCount(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             tempDir,
		Organization:        storage.OrganizationByRun,
		EnableDeduplication: true,
	})
	require.NoError(t, err)

	testData := []byte("shared content")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	content := &types.MediaContent{
		Data:     &base64Data,
		MIMEType: "image/jpeg",
	}

	// Store same content 3 times
	refs := make([]storage.Reference, 3)
	for i := 0; i < 3; i++ {
		metadata := storage.MediaMetadata{
			RunID:      "run-" + string(rune('1'+i)),
			MessageIdx: i,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)
		refs[i] = ref
	}

	// All should point to same file
	assert.Equal(t, refs[0], refs[1])
	assert.Equal(t, refs[1], refs[2])
	assert.FileExists(t, string(refs[0]))

	// Delete first reference - file should still exist
	err = fs.DeleteMedia(ctx, refs[0])
	assert.NoError(t, err)
	assert.FileExists(t, string(refs[0]))

	// Delete second reference - file should still exist
	err = fs.DeleteMedia(ctx, refs[1])
	assert.NoError(t, err)
	assert.FileExists(t, string(refs[0]))

	// Delete third reference - file should be deleted
	err = fs.DeleteMedia(ctx, refs[2])
	assert.NoError(t, err)
	assert.NoFileExists(t, string(refs[0]))
}

func TestFileStore_DefaultPolicy(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:       tempDir,
		Organization:  storage.OrganizationByRun,
		DefaultPolicy: "retain-30days",
	})
	require.NoError(t, err)

	testData := []byte("test data")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	content := &types.MediaContent{
		Data:     &base64Data,
		MIMEType: "image/jpeg",
	}

	metadata := storage.MediaMetadata{
		RunID:      "run-policy",
		MessageIdx: 0,
		PartIdx:    0,
		MIMEType:   "image/jpeg",
		SizeBytes:  int64(len(testData)),
		Timestamp:  time.Now(),
	}

	ref, err := fs.StoreMedia(ctx, content, &metadata)
	require.NoError(t, err)

	// Retrieve and verify policy was applied
	retrieved, err := fs.RetrieveMedia(ctx, ref)
	require.NoError(t, err)
	assert.NotNil(t, retrieved.PolicyName)
	assert.Equal(t, "retain-30days", *retrieved.PolicyName)
}

func TestFileStore_URLAndFilePathContent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	t.Run("rejects URL-based media", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		url := "https://example.com/image.jpg"
		content := &types.MediaContent{
			URL:      &url,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-url",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  0,
			Timestamp:  time.Now(),
		}

		// URL-based media not yet supported
		_, err = fs.StoreMedia(ctx, content, &metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "URL-based media not yet supported")
	})

	t.Run("stores media from file path", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		// Create actual test file
		testFile := filepath.Join(tempDir, "source.png")
		testData := []byte("png file content")
		err = os.WriteFile(testFile, testData, 0600)
		require.NoError(t, err)

		content := &types.MediaContent{
			FilePath: &testFile,
			MIMEType: "image/png",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-file",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/png",
			SizeBytes:  int64(len(testData)),
			Timestamp:  time.Now(),
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)

		// Should have copied file content to storage
		retrieved, err := fs.RetrieveMedia(ctx, ref)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.FilePath)
		assert.Equal(t, string(ref), *retrieved.FilePath)
	})
}

func TestFileStore_MetadataPersistence(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	t.Run("persists and loads metadata", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		testData := []byte("metadata test")
		base64Data := base64.StdEncoding.EncodeToString(testData)
		content := &types.MediaContent{
			Data:     &base64Data,
			MIMEType: "audio/mp3",
		}

		originalTime := time.Now().UTC().Truncate(time.Second)
		metadata := storage.MediaMetadata{
			RunID:          "run-meta",
			SessionID:      "session-123",
			ConversationID: "conv-456",
			MessageIdx:     5,
			PartIdx:        3,
			MIMEType:       "audio/mp3",
			SizeBytes:      int64(len(testData)),
			ProviderID:     "test-provider",
			Timestamp:      originalTime,
			PolicyName:     "keep-forever",
		}

		ref, err := fs.StoreMedia(ctx, content, &metadata)
		require.NoError(t, err)

		// Retrieve and verify all metadata fields
		retrieved, err := fs.RetrieveMedia(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, metadata.MIMEType, retrieved.MIMEType)
		assert.NotNil(t, retrieved.FilePath)
		assert.Equal(t, string(ref), *retrieved.FilePath)
		// PolicyName should be loaded from metadata
		assert.NotNil(t, retrieved.PolicyName)
		assert.Equal(t, "keep-forever", *retrieved.PolicyName)

		// Verify .meta file exists
		metaFile := string(ref) + ".meta"
		assert.FileExists(t, metaFile)
	})
}

func TestFileStore_LoadsDedupIndexOnStartup(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Create first filestore and store media
	fs1, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             tempDir,
		Organization:        storage.OrganizationByRun,
		EnableDeduplication: true,
	})
	require.NoError(t, err)

	testData := []byte("persistent dedup test")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	content := &types.MediaContent{
		Data:     &base64Data,
		MIMEType: "image/jpeg",
	}

	metadata := storage.MediaMetadata{
		RunID:      "run-dedup-persist",
		MessageIdx: 0,
		PartIdx:    0,
		MIMEType:   "image/jpeg",
		SizeBytes:  int64(len(testData)),
		Timestamp:  time.Now(),
	}

	ref1, err := fs1.StoreMedia(ctx, content, &metadata)
	require.NoError(t, err)

	// Create new filestore instance (simulates restart)
	fs2, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             tempDir,
		Organization:        storage.OrganizationByRun,
		EnableDeduplication: true,
	})
	require.NoError(t, err)

	// Store same content with new metadata
	metadata2 := metadata
	metadata2.RunID = "run-dedup-persist-2"
	metadata2.MessageIdx = 1

	ref2, err := fs2.StoreMedia(ctx, content, &metadata2)
	require.NoError(t, err)

	// Should deduplicate using loaded index
	assert.Equal(t, ref1, ref2)
}

func TestFileStore_SpecialCases(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	t.Run("rejects directory as file reference", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		// Create a directory
		dirPath := filepath.Join(tempDir, "testdir")
		err = os.MkdirAll(dirPath, 0750)
		require.NoError(t, err)

		// Try to retrieve directory as media
		_, err = fs.RetrieveMedia(ctx, storage.Reference(dirPath))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("handles invalid base64 data", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		invalidBase64 := "not!!valid!!base64"
		content := &types.MediaContent{
			Data:     &invalidBase64,
			MIMEType: "image/jpeg",
		}

		metadata := storage.MediaMetadata{
			RunID:      "run-invalid",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "image/jpeg",
			SizeBytes:  10,
			Timestamp:  time.Now(),
		}

		_, err = fs.StoreMedia(ctx, content, &metadata)
		assert.Error(t, err)
	})

	t.Run("retrieves media without metadata file", func(t *testing.T) {
		fs, err := local.NewFileStore(local.FileStoreConfig{
			BaseDir:      tempDir,
			Organization: storage.OrganizationByRun,
		})
		require.NoError(t, err)

		// Create a file directly without storing through filestore
		testFile := filepath.Join(tempDir, "runs", "test-orphan", "orphan.jpg")
		err = os.MkdirAll(filepath.Dir(testFile), 0750)
		require.NoError(t, err)
		err = os.WriteFile(testFile, []byte("orphan data"), 0600)
		require.NoError(t, err)

		// Try to retrieve - should infer MIME type from extension
		retrieved, err := fs.RetrieveMedia(ctx, storage.Reference(testFile))
		require.NoError(t, err)
		assert.Equal(t, "image/jpeg", retrieved.MIMEType)
		assert.Nil(t, retrieved.PolicyName)
	})
}
