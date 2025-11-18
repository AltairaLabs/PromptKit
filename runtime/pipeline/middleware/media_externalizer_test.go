package middleware_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaExternalizerMiddleware_Disabled(t *testing.T) {
	config := middleware.MediaExternalizerConfig{
		Enabled: false,
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	// Add response with media
	testData := []byte("test image data")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	execCtx.Response = &pipeline.Response{
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &base64Data,
					MIMEType: "image/jpeg",
				},
			},
		},
	}

	nextCalled := false
	err := mw.Process(execCtx, func() error {
		nextCalled = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, nextCalled)
	// Media should still have inline data (not externalized)
	assert.NotNil(t, execCtx.Response.Parts[0].Media.Data)
	assert.Nil(t, execCtx.Response.Parts[0].Media.StorageReference)
}

func TestMediaExternalizerMiddleware_ExternalizesMedia(t *testing.T) {
	tempDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	config := middleware.MediaExternalizerConfig{
		Enabled:        true,
		StorageService: storageService,
		RunID:          "test-run",
		DefaultPolicy:  "retain-30days",
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	// Add response with media
	testData := []byte("test image data for externalization")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	execCtx.Response = &pipeline.Response{
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &base64Data,
					MIMEType: "image/jpeg",
				},
			},
		},
	}

	nextCalled := false
	err = mw.Process(execCtx, func() error {
		nextCalled = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, nextCalled)

	// Verify media was externalized
	media := execCtx.Response.Parts[0].Media
	assert.Nil(t, media.Data, "inline data should be cleared")
	assert.NotNil(t, media.StorageReference, "storage reference should be set")
	assert.NotEmpty(t, *media.StorageReference)
}

func TestMediaExternalizerMiddleware_SizeThreshold(t *testing.T) {
	tempDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	config := middleware.MediaExternalizerConfig{
		Enabled:         true,
		StorageService:  storageService,
		SizeThresholdKB: 10, // 10 KB threshold
		RunID:           "test-run",
	}

	mw := middleware.MediaExternalizerMiddleware(&config)

	t.Run("small media stays inline", func(t *testing.T) {
		execCtx := createTestExecutionContext()

		// Create small media (< 10 KB)
		smallData := []byte("small")
		base64Data := base64.StdEncoding.EncodeToString(smallData)
		execCtx.Response = &pipeline.Response{
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						Data:     &base64Data,
						MIMEType: "image/png",
					},
				},
			},
		}

		err := mw.Process(execCtx, func() error { return nil })
		assert.NoError(t, err)

		// Should NOT be externalized
		media := execCtx.Response.Parts[0].Media
		assert.NotNil(t, media.Data, "small media should stay inline")
		assert.Nil(t, media.StorageReference)
	})

	t.Run("large media gets externalized", func(t *testing.T) {
		execCtx := createTestExecutionContext()

		// Create large media (> 10 KB)
		largeData := make([]byte, 15*1024) // 15 KB
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}
		base64Data := base64.StdEncoding.EncodeToString(largeData)
		execCtx.Response = &pipeline.Response{
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						Data:     &base64Data,
						MIMEType: "image/jpeg",
					},
				},
			},
		}

		err := mw.Process(execCtx, func() error { return nil })
		assert.NoError(t, err)

		// Should be externalized
		media := execCtx.Response.Parts[0].Media
		assert.Nil(t, media.Data, "large media should be externalized")
		assert.NotNil(t, media.StorageReference)
	})
}

func TestMediaExternalizerMiddleware_MultipleMedia(t *testing.T) {
	tempDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	config := middleware.MediaExternalizerConfig{
		Enabled:        true,
		StorageService: storageService,
		RunID:          "test-run",
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	// Add response with multiple media parts
	testData1 := []byte("image 1")
	testData2 := []byte("audio 1")
	base64Data1 := base64.StdEncoding.EncodeToString(testData1)
	base64Data2 := base64.StdEncoding.EncodeToString(testData2)

	execCtx.Response = &pipeline.Response{
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: stringPtr("Some text"),
			},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &base64Data1,
					MIMEType: "image/png",
				},
			},
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &base64Data2,
					MIMEType: "audio/mp3",
				},
			},
		},
	}

	err = mw.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Verify text part unchanged
	assert.NotNil(t, execCtx.Response.Parts[0].Text)

	// Verify both media parts externalized
	media1 := execCtx.Response.Parts[1].Media
	assert.Nil(t, media1.Data)
	assert.NotNil(t, media1.StorageReference)

	media2 := execCtx.Response.Parts[2].Media
	assert.Nil(t, media2.Data)
	assert.NotNil(t, media2.StorageReference)

	// Storage references should be different
	assert.NotEqual(t, *media1.StorageReference, *media2.StorageReference)
}

func TestMediaExternalizerMiddleware_SkipsAlreadyExternalized(t *testing.T) {
	tempDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	config := middleware.MediaExternalizerConfig{
		Enabled:        true,
		StorageService: storageService,
		RunID:          "test-run",
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	existingRef := "/path/to/existing/media.jpg"
	execCtx.Response = &pipeline.Response{
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					StorageReference: &existingRef,
					MIMEType:         "image/jpeg",
				},
			},
		},
	}

	err = mw.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Reference should be unchanged
	assert.Equal(t, existingRef, *execCtx.Response.Parts[0].Media.StorageReference)
}

func TestMediaExternalizerMiddleware_SkipsNoResponse(t *testing.T) {
	config := middleware.MediaExternalizerConfig{
		Enabled: true,
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()
	execCtx.Response = nil

	err := mw.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)
}

func TestMediaExternalizerMiddleware_SkipsOnError(t *testing.T) {
	config := middleware.MediaExternalizerConfig{
		Enabled: true,
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()
	execCtx.Error = assert.AnError

	err := mw.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)
}

func TestMediaExternalizerMiddleware_WithSessionAndConversationIDs(t *testing.T) {
	tempDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationBySession,
	})
	require.NoError(t, err)

	config := middleware.MediaExternalizerConfig{
		Enabled:        true,
		StorageService: storageService,
		RunID:          "test-run",
		SessionID:      "session-123",
		ConversationID: "conv-456",
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	testData := []byte("test data")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	execCtx.Response = &pipeline.Response{
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &base64Data,
					MIMEType: "image/jpeg",
				},
			},
		},
	}

	err = mw.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Verify externalized
	media := execCtx.Response.Parts[0].Media
	assert.NotNil(t, media.StorageReference)
	// Should contain session ID in path
	assert.Contains(t, *media.StorageReference, "session-123")
}

func TestMediaExternalizerMiddleware_StreamChunk(t *testing.T) {
	config := middleware.MediaExternalizerConfig{
		Enabled: true,
	}

	mw := middleware.MediaExternalizerMiddleware(&config)
	execCtx := createTestExecutionContext()

	// StreamChunk should be a no-op
	chunk := &providers.StreamChunk{
		Content: "test content",
	}

	err := mw.StreamChunk(execCtx, chunk)
	assert.NoError(t, err)
}

// Helper functions

func createTestExecutionContext() *pipeline.ExecutionContext {
	return &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}
}

func stringPtr(s string) *string {
	return &s
}
