package providers_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		_, _ = w.Write(testContent)
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

// --- New tests for aggregate media size limits, per-item validation, and item count ---

func TestMediaLoader_MaxMediaItems_Exceeded(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	// Load MaxMediaItems items successfully (using inline data, which doesn't count
	// toward aggregate size but does count toward item limit)
	for i := 0; i < providers.MaxMediaItems; i++ {
		data := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("item-%d", i)))
		media := &types.MediaContent{
			Data:     &data,
			MIMEType: "text/plain",
		}
		_, err := loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err, "item %d should succeed", i)
	}

	// The next item should fail
	data := base64.StdEncoding.EncodeToString([]byte("one-too-many"))
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: "text/plain",
	}
	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media item limit exceeded")
	assert.Contains(t, err.Error(), fmt.Sprintf("maximum %d", providers.MaxMediaItems))
}

func TestMediaLoader_AggregateSize_ExceededViaFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Use a small max URL size and create files that together exceed the aggregate limit.
	// We'll set MaxURLSizeBytes high enough for individual files but use enough files
	// to exceed MaxAggregateMediaSize.
	// To keep the test fast, we'll use a loader and write moderate-sized files.
	// MaxAggregateMediaSize is 100MB — we'll create files of ~60MB each (only 2 needed).
	// But that's too slow for a test. Instead, let's verify the tracking logic
	// by checking that two files whose sum exceeds MaxAggregateMediaSize cause an error.

	// We'll create two files: one 60MB and one 50MB. But that's too large for tests.
	// Instead, let's verify the mechanism works at a smaller scale by checking the
	// aggregate tracking directly. We need to test with real files but small enough
	// to be fast.

	// Actually, the simplest approach: create files that are medium-sized and use
	// multiple calls to exceed the aggregate. We can't easily change MaxAggregateMediaSize
	// (it's a const), so let's verify the error message format and tracking logic.

	// Create a file slightly under MaxAggregateMediaSize
	// For a real test, we create two files that together exceed the limit.
	// Since MaxAggregateMediaSize = 100MB, we'll skip creating actual 100MB files
	// and instead test the error path indirectly by loading many URL items
	// with known sizes via a test server.

	// Let's use a test HTTP server to serve content of known sizes
	contentSize := 60 * 1024 * 1024 // 60MB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Write contentSize bytes
		buf := make([]byte, 32*1024)
		written := 0
		for written < contentSize {
			toWrite := len(buf)
			if written+toWrite > contentSize {
				toWrite = contentSize - written
			}
			n, err := w.Write(buf[:toWrite])
			if err != nil {
				return
			}
			written += n
		}
	}))
	defer server.Close()

	_ = tmpDir // not needed for this approach

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 70 * 1024 * 1024, // 70MB per file limit
		HTTPTimeout:     30 * time.Second,
	})

	url := server.URL

	// First 60MB file should succeed
	media1 := &types.MediaContent{
		URL:      &url,
		MIMEType: "application/octet-stream",
	}
	_, err := loader.GetBase64Data(context.Background(), media1)
	require.NoError(t, err)

	// Second 60MB file should exceed the 100MB aggregate limit
	media2 := &types.MediaContent{
		URL:      &url,
		MIMEType: "application/octet-stream",
	}
	_, err = loader.GetBase64Data(context.Background(), media2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "aggregate media size limit exceeded")
}

func TestMediaLoader_FileSizeLimit_Exceeded(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that exceeds the per-file limit
	tmpFile := filepath.Join(tmpDir, "large.bin")
	// Write 2KB of data
	data := make([]byte, 2048)
	err := os.WriteFile(tmpFile, data, 0644)
	require.NoError(t, err)

	// Set max URL size to 1KB (used as per-file limit too)
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 1024,
	})

	media := &types.MediaContent{
		FilePath: &tmpFile,
		MIMEType: "application/octet-stream",
	}

	_, err = loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file size")
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestMediaLoader_URLSizeLimit_ViaActualContent(t *testing.T) {
	// Test that URL content exceeding maxURLSize (via actual body, not Content-Length header)
	// is rejected
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't set Content-Length so the check is via LimitReader
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 1024,
	})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "application/octet-stream",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestMediaLoader_MaxMediaItems_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	// Create MaxMediaItems files and load them all
	for i := 0; i < providers.MaxMediaItems; i++ {
		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("file-%d.txt", i))
		err := os.WriteFile(tmpFile, []byte(fmt.Sprintf("content-%d", i)), 0644)
		require.NoError(t, err)

		media := &types.MediaContent{
			FilePath: &tmpFile,
			MIMEType: "text/plain",
		}
		_, err = loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err, "file %d should succeed", i)
	}

	// Next one should fail
	extraFile := filepath.Join(tmpDir, "extra.txt")
	err := os.WriteFile(extraFile, []byte("extra"), 0644)
	require.NoError(t, err)

	media := &types.MediaContent{
		FilePath: &extraFile,
		MIMEType: "text/plain",
	}
	_, err = loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media item limit exceeded")
}

func TestMediaLoader_MaxMediaItems_WithURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("url-content"))
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		HTTPTimeout: 5 * time.Second,
	})

	url := server.URL

	for i := 0; i < providers.MaxMediaItems; i++ {
		media := &types.MediaContent{
			URL:      &url,
			MIMEType: "text/plain",
		}
		_, err := loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err, "URL item %d should succeed", i)
	}

	// Next one should fail
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}
	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media item limit exceeded")
}

func TestMediaLoader_AggregateSize_TrackedAcrossFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two files: each 512 bytes. Set max URL size to 1024 per file.
	// Set aggregate limit to 100MB (can't change const), so this just verifies
	// the tracking doesn't error for small files.
	f1 := filepath.Join(tmpDir, "f1.bin")
	f2 := filepath.Join(tmpDir, "f2.bin")
	err := os.WriteFile(f1, make([]byte, 512), 0644)
	require.NoError(t, err)
	err = os.WriteFile(f2, make([]byte, 512), 0644)
	require.NoError(t, err)

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	media1 := &types.MediaContent{FilePath: &f1, MIMEType: "application/octet-stream"}
	_, err = loader.GetBase64Data(context.Background(), media1)
	assert.NoError(t, err)

	media2 := &types.MediaContent{FilePath: &f2, MIMEType: "application/octet-stream"}
	_, err = loader.GetBase64Data(context.Background(), media2)
	assert.NoError(t, err)
}

func TestMediaLoader_LargeFileWarning(t *testing.T) {
	// This test verifies that loading a file > 5MB doesn't error
	// (the warning is logged but doesn't affect the return value).
	// We use a 6MB file to trigger the warning path.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "large.bin")

	size := 6 * 1024 * 1024 // 6MB
	data := make([]byte, size)
	err := os.WriteFile(tmpFile, data, 0644)
	require.NoError(t, err)

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	media := &types.MediaContent{
		FilePath: &tmpFile,
		MIMEType: "application/octet-stream",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)

	// Verify the result is valid base64
	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Len(t, decoded, size)
}

func TestMediaLoader_LargeURLWarning(t *testing.T) {
	// Serve 6MB content from a URL to trigger the large-file warning path
	size := 6 * 1024 * 1024
	content := make([]byte, size)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		HTTPTimeout: 10 * time.Second,
	})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "application/octet-stream",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestMediaLoader_Constants(t *testing.T) {
	// Verify the constants have expected values
	assert.Equal(t, int64(100*1024*1024), providers.MaxAggregateMediaSize)
	assert.Equal(t, 20, providers.MaxMediaItems)
}

func TestMediaLoader_MixedSourceTypes_ItemCounting(t *testing.T) {
	tmpDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("url-data"))
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		HTTPTimeout: 5 * time.Second,
	})

	// Load a mix of inline, file, and URL items
	// 7 inline + 7 file + 6 URL = 20 items (at limit)
	for i := 0; i < 7; i++ {
		data := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("inline-%d", i)))
		media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
		_, err := loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err)
	}

	for i := 0; i < 7; i++ {
		f := filepath.Join(tmpDir, fmt.Sprintf("mixed-%d.txt", i))
		err := os.WriteFile(f, []byte(fmt.Sprintf("file-%d", i)), 0644)
		require.NoError(t, err)
		media := &types.MediaContent{FilePath: &f, MIMEType: "text/plain"}
		_, err = loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err)
	}

	url := server.URL
	for i := 0; i < 6; i++ {
		media := &types.MediaContent{URL: &url, MIMEType: "text/plain"}
		_, err := loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err)
	}

	// 21st item should fail regardless of type
	data := base64.StdEncoding.EncodeToString([]byte("overflow"))
	media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media item limit exceeded")
}

func TestMediaLoader_NewLoaderResetsCounters(t *testing.T) {
	// Verify that each new MediaLoader instance starts fresh
	loader1 := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	loader2 := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	// Load items on loader1
	for i := 0; i < 5; i++ {
		data := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("data-%d", i)))
		media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
		_, err := loader1.GetBase64Data(context.Background(), media)
		require.NoError(t, err)
	}

	// loader2 should be unaffected
	data := base64.StdEncoding.EncodeToString([]byte("fresh"))
	media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
	_, err := loader2.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)
}

func TestMediaLoader_FileStatError(t *testing.T) {
	// Test error when file exists but stat fails (e.g., permission issues)
	// We simulate this by using a non-existent file path
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	path := "/nonexistent/path/to/file.bin"
	media := &types.MediaContent{
		FilePath: &path,
		MIMEType: "application/octet-stream",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestMediaLoader_EmptyDataField(t *testing.T) {
	// When Data is a pointer to an empty string, it should fall through
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	empty := ""
	media := &types.MediaContent{
		Data:     &empty,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

func TestMediaLoader_EmptyFilePathField(t *testing.T) {
	// When FilePath is a pointer to an empty string, it should fall through
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	empty := ""
	media := &types.MediaContent{
		FilePath: &empty,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

func TestMediaLoader_EmptyURLField(t *testing.T) {
	// When URL is a pointer to an empty string, it should fall through
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	empty := ""
	media := &types.MediaContent{
		URL:      &empty,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

func TestMediaLoader_EmptyStorageRefField(t *testing.T) {
	// When StorageReference is a pointer to an empty string, it should fall through
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	empty := ""
	media := &types.MediaContent{
		StorageReference: &empty,
		MIMEType:         "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

func TestMediaLoader_URLContentLengthExceedsLimit(t *testing.T) {
	// Server reports Content-Length exceeding the limit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 1024,
	})

	url := server.URL
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: "text/plain",
	}

	_, err := loader.GetBase64Data(context.Background(), media)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL content size")
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestMediaLoader_DefaultConfig(t *testing.T) {
	// Verify defaults are applied when config is zero-valued
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	// Should be able to load small content without issues
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "small.txt")
	err := os.WriteFile(tmpFile, []byte("small"), 0644)
	require.NoError(t, err)

	media := &types.MediaContent{
		FilePath: &tmpFile,
		MIMEType: "text/plain",
	}

	result, err := loader.GetBase64Data(context.Background(), media)
	assert.NoError(t, err)

	decoded, err := base64.StdEncoding.DecodeString(result)
	require.NoError(t, err)
	assert.Equal(t, []byte("small"), decoded)
}

func TestMediaLoader_AggregateSize_ErrorMessage(t *testing.T) {
	// Test the exact error message format for aggregate limit
	// We use URLs with a server that returns exactly-sized content
	chunkSize := 60 * 1024 * 1024 // 60MB per request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Stream chunkSize bytes
		remaining := chunkSize
		buf := make([]byte, 32*1024)
		for remaining > 0 {
			toWrite := len(buf)
			if toWrite > remaining {
				toWrite = remaining
			}
			n, err := w.Write(buf[:toWrite])
			if err != nil {
				return
			}
			remaining -= n
		}
	}))
	defer server.Close()

	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
		MaxURLSizeBytes: 70 * 1024 * 1024,
		HTTPTimeout:     30 * time.Second,
	})

	url := server.URL

	// First request succeeds
	media := &types.MediaContent{URL: &url, MIMEType: "application/octet-stream"}
	_, err := loader.GetBase64Data(context.Background(), media)
	require.NoError(t, err)

	// Second request exceeds aggregate
	media2 := &types.MediaContent{URL: &url, MIMEType: "application/octet-stream"}
	_, err = loader.GetBase64Data(context.Background(), media2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aggregate media size limit exceeded")
	assert.Contains(t, err.Error(), fmt.Sprintf("maximum %d bytes", providers.MaxAggregateMediaSize))
}

func TestMediaLoader_ItemCountError_IncludesNumbers(t *testing.T) {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	// Fill up all slots
	for i := 0; i < providers.MaxMediaItems; i++ {
		data := strings.Repeat("A", 4) // minimal valid base64-ish data
		media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
		_, err := loader.GetBase64Data(context.Background(), media)
		require.NoError(t, err)
	}

	// Verify error message includes counts
	data := "overflow"
	media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}
	_, err := loader.GetBase64Data(context.Background(), media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("already loaded %d items", providers.MaxMediaItems))
	assert.Contains(t, err.Error(), fmt.Sprintf("maximum %d", providers.MaxMediaItems))
}
