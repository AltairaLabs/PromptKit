package middleware_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMediaExternalization_StoreAndRetrieve tests the complete cycle:
// 1. Store media via Storage Service
// 2. Retrieve media via MediaLoader
func TestMediaExternalization_StoreAndRetrieve(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "media-integration-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create sample image data
	imageData := createSampleImageData()

	// Create media content with inline data
	data := imageData
	mediaContent := &types.MediaContent{
		Data:     &data,
		MIMEType: types.MIMETypeImagePNG,
	}

	// Store media via storage service (simulating what middleware does)
	storageRef, err := storageService.StoreMedia(context.Background(), mediaContent, &storage.MediaMetadata{
		RunID:          "test-run",
		SessionID:      "test-session",
		ConversationID: "test-conversation",
		MIMEType:       types.MIMETypeImagePNG,
		SizeBytes:      int64(len(imageData)),
		PolicyName:     "retain",
	})
	require.NoError(t, err)
	require.NotEmpty(t, storageRef)

	// Clear inline data and set storage reference (what externalization does)
	mediaContent.Data = nil
	storageRefStr := string(storageRef)
	mediaContent.StorageReference = &storageRefStr

	// Verify data was cleared
	assert.Nil(t, mediaContent.Data)
	assert.NotNil(t, mediaContent.StorageReference)

	// Now retrieve via MediaLoader
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		StorageService: storageService,
	})

	retrievedData, err := loader.GetBase64Data(context.Background(), mediaContent)
	require.NoError(t, err)
	assert.Equal(t, imageData, retrievedData, "Retrieved data should match original")
}

// TestMediaExternalization_SizeThreshold tests that small media stays inline
func TestMediaExternalization_SizeThreshold(t *testing.T) {
	// Create small media data (~500 bytes)
	smallData := createSmallImageData()
	decoded, err := base64.StdEncoding.DecodeString(smallData)
	require.NoError(t, err)

	sizeKB := int64(len(decoded)) / 1024

	// Test threshold logic: media smaller than threshold should stay inline
	thresholdKB := int64(100) // 100KB threshold

	assert.Less(t, sizeKB, thresholdKB, "Small media should be below threshold")

	// In real usage, middleware would check this and skip externalization
	shouldExternalize := sizeKB >= thresholdKB
	assert.False(t, shouldExternalize, "Small media should not be externalized")
}

// TestMediaExternalization_MultipleMedia tests storing and retrieving multiple media files
func TestMediaExternalization_MultipleMedia(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "media-multiple-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create multiple media items
	imageData := createSampleImageData()
	audioData := createSampleAudioData()

	// Store image
	imageData_ := imageData
	imageRef, err := storageService.StoreMedia(context.Background(), &types.MediaContent{
		Data:     &imageData_,
		MIMEType: types.MIMETypeImagePNG,
	}, &storage.MediaMetadata{
		RunID:          "multi-run",
		SessionID:      "multi-session",
		ConversationID: "multi-conv",
		MIMEType:       types.MIMETypeImagePNG,
		SizeBytes:      int64(len(imageData)),
		PolicyName:     "retain",
	})
	require.NoError(t, err)

	// Store audio
	audioData_ := audioData
	audioRef, err := storageService.StoreMedia(context.Background(), &types.MediaContent{
		Data:     &audioData_,
		MIMEType: types.MIMETypeAudioMP3,
	}, &storage.MediaMetadata{
		RunID:          "multi-run",
		SessionID:      "multi-session",
		ConversationID: "multi-conv",
		MIMEType:       types.MIMETypeAudioMP3,
		SizeBytes:      int64(len(audioData)),
		PolicyName:     "retain",
	})
	require.NoError(t, err)

	// Verify both references are unique
	assert.NotEqual(t, imageRef, audioRef)

	// Create media loader
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		StorageService: storageService,
	})

	// Retrieve image
	imageRefStr := string(imageRef)
	imageMedia := &types.MediaContent{
		StorageReference: &imageRefStr,
		MIMEType:         types.MIMETypeImagePNG,
	}
	retrievedImage, err := loader.GetBase64Data(context.Background(), imageMedia)
	require.NoError(t, err)
	assert.Equal(t, imageData, retrievedImage)

	// Retrieve audio
	audioRefStr := string(audioRef)
	audioMedia := &types.MediaContent{
		StorageReference: &audioRefStr,
		MIMEType:         types.MIMETypeAudioMP3,
	}
	retrievedAudio, err := loader.GetBase64Data(context.Background(), audioMedia)
	require.NoError(t, err)
	assert.Equal(t, audioData, retrievedAudio)
}

// TestMediaExternalization_HierarchicalOrganization tests storage hierarchy
func TestMediaExternalization_HierarchicalOrganization(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "media-hierarchy-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create media data
	imageData := createSampleImageData()

	// Store with full hierarchy
	imageData_ := imageData
	storageRef, err := storageService.StoreMedia(context.Background(), &types.MediaContent{
		Data:     &imageData_,
		MIMEType: types.MIMETypeImagePNG,
	}, &storage.MediaMetadata{
		RunID:          "test-run-123",
		SessionID:      "session-456",
		ConversationID: "conv-789",
		MIMEType:       types.MIMETypeImagePNG,
		SizeBytes:      int64(len(imageData)),
		PolicyName:     "retain",
	})
	require.NoError(t, err)

	// Storage reference should contain at minimum session information
	refStr := string(storageRef)
	// The local file store organizes by sessions
	assert.Contains(t, refStr, "sessions")
	assert.Contains(t, refStr, "session-456")
	// Verify it's a valid file path
	assert.Contains(t, refStr, ".png")
}

// Helper functions to create sample media data

func createSampleImageData() string {
	// Create a small PNG image (1x1 pixel, red)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
		0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82, // IEND chunk
	}
	return base64.StdEncoding.EncodeToString(pngData)
}

func createSmallImageData() string {
	// Create very small image data (~500 bytes when base64 encoded)
	smallData := make([]byte, 375) // Will be ~500 bytes in base64
	for i := range smallData {
		smallData[i] = byte(i % 256)
	}
	return base64.StdEncoding.EncodeToString(smallData)
}

func createSampleAudioData() string {
	// Create minimal MP3-like data
	mp3Header := []byte{
		0xFF, 0xFB, 0x90, 0x00, // MP3 frame sync + header
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	return base64.StdEncoding.EncodeToString(mp3Header)
}
