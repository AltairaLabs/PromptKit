package providers_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaLoader_GetBase64Data_InlineData(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	testData := "SGVsbG8gV29ybGQ=" // "Hello World" in base64
	media := &types.MediaContent{
		Data:     &testData,
		MIMEType: "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)
	assert.Equal(t, testData, result)
}

func TestMediaLoader_GetBase64Data_FilePath(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("Test file content")
	err := os.WriteFile(tmpFile, testContent, 0644)
	require.NoError(t, err)

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	media := &types.MediaContent{
		FilePath: &tmpFile,
		MIMEType: "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)

	// Verify decoded content
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Equal(t, testContent, decoded)
}

func TestMediaLoader_GetBase64Data_URL(t *testing.T) {
	// Create test server
	testContent := []byte("Remote content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(testContent)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		HTTPTimeout: 5 * time.Second,
	})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)

	// Verify decoded content
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Equal(t, testContent, decoded)
}

func TestMediaLoader_GetBase64Data_StorageReference(t *testing.T) {
	// Setup storage
	tmpDir := t.TempDir()
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tmpDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	// Store test media
	testData := []byte("Stored content")
	base64Data := base64.StdEncoding.EncodeToString(testData)
	storeMedia := &types.MediaContent{
		Data:     &base64Data,
		MIMEType: "text/plain",
	}
	metadata := &storage.MediaMetadata{
		RunID:    "test-run",
		MIMEType: "text/plain",
	}

	ref, err := storageService.StoreMedia(context.Background(), storeMedia, metadata)
	require.NoError(t, err)

	// Load via MediaLoader
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		StorageService: storageService,
	})

	refStr := string(ref)
	media := &types.MediaContent{
		StorageReference: &refStr,
		MIMEType:         "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)

	// Verify decoded content matches original
	// Note: RetrieveMedia returns FilePath, so MediaLoader loads it from disk
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Equal(t, testData, decoded)
}

func TestMediaLoader_GetBase64Data_StorageReferenceWithoutService(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	ref := "some-storage-ref"
	media := &types.MediaContent{
		StorageReference: &ref,
		MIMEType:         "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage service not configured")
}

func TestMediaLoader_GetBase64Data_NilMedia(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	_, err := loader.GetBase64Data(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media content is nil")
}

func TestMediaLoader_GetBase64Data_NoSource(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	media := &types.MediaContent{
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

func TestMediaLoader_GetBase64Data_FileNotFound(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	nonExistentFile := "/nonexistent/file.txt"
	media := &types.MediaContent{
		FilePath: &nonExistentFile,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestMediaLoader_GetBase64Data_URL_404(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestMediaLoader_GetBase64Data_URL_TooLarge(t *testing.T) {
	// Create test server with large content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000") // 1MB
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 1024, // Only allow 1KB
	})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestMediaLoader_GetBase64Data_PriorityOrder(t *testing.T) {
	// Test that Data has highest priority
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("file content"), 0644)
	require.NoError(t, err)

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	inlineData := base64.StdEncoding.EncodeToString([]byte("inline data"))
	media := &types.MediaContent{
		Data:     &inlineData,
		FilePath: &tmpFile,
		MIMEType: "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)
	assert.Equal(t, inlineData, result) // Should return inline data, not file
}

func TestMediaLoader_GetBase64Data_ContextCancellation(t *testing.T) {
	// Create test server with slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		HTTPTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(ctx, media)
	assert.Error(t, err)
}

func TestLoadFileAsBase64_BackwardCompatibility(t *testing.T) {
	// Test that the deprecated function still works
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("Backward compat test")
	err := os.WriteFile(tmpFile, testContent, 0644)
	require.NoError(t, err)

	result, err := providers.LoadFileAsBase64(tmpFile)
	assert.NoError(t, err)

	// Verify decoded content
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Equal(t, testContent, decoded)
}
