package turnexecutors

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newLocalServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("unable to start test server listener: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = l
	server.Start()
	return server
}

// MockStorageService for testing storage reference scenarios
type MockStorageService struct {
	mock.Mock
}

func (m *MockStorageService) StoreMedia(ctx context.Context, media *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
	args := m.Called(ctx, media, metadata)
	return args.Get(0).(storage.Reference), args.Error(1)
}

func (m *MockStorageService) RetrieveMedia(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
	args := m.Called(ctx, ref)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.MediaContent), args.Error(1)
}

func (m *MockStorageService) DeleteMedia(ctx context.Context, ref storage.Reference) error {
	args := m.Called(ctx, ref)
	return args.Error(0)
}

func (m *MockStorageService) GetURL(ctx context.Context, ref storage.Reference, expiry time.Duration) (string, error) {
	args := m.Called(ctx, ref, expiry)
	return args.String(0), args.Error(1)
}

// Test loadFromStorageReference - currently 0% coverage
func TestLoadFromStorageReference_Success(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "test-ref-123"
	data := "dGVzdCBkYXRh" // "test data" in base64

	expectedMedia := &types.MediaContent{
		Data:     &data,
		MIMEType: "image/jpeg",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(expectedMedia, nil)

	media, err := loadFromStorageReference(ctx, mockStorage, ref, "image", 0)

	assert.NoError(t, err)
	assert.NotNil(t, media)
	assert.Equal(t, expectedMedia, media)
	mockStorage.AssertExpectations(t)
}

func TestLoadFromStorageReference_NoService(t *testing.T) {
	ctx := context.Background()

	media, err := loadFromStorageReference(ctx, nil, "test-ref", "image", 0)

	assert.Error(t, err)
	assert.Nil(t, media)
	assert.Contains(t, err.Error(), errStorageServiceMissing)
}

func TestLoadFromStorageReference_RetrievalError(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "test-ref-123"

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).
		Return((*types.MediaContent)(nil), errors.New("storage failure"))

	media, err := loadFromStorageReference(ctx, mockStorage, ref, "audio", 1)

	assert.Error(t, err)
	assert.Nil(t, media)
	assert.Contains(t, err.Error(), errStorageRetrieveFailed)
	mockStorage.AssertExpectations(t)
}

func TestLoadFromStorageReference_NoData(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "test-ref-123"

	// Return media with nil Data
	emptyMedia := &types.MediaContent{
		MIMEType: "video/mp4",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(emptyMedia, nil)

	media, err := loadFromStorageReference(ctx, mockStorage, ref, "video", 2)

	assert.Error(t, err)
	assert.Nil(t, media)
	assert.Contains(t, err.Error(), errStorageReturnedNoData)
	mockStorage.AssertExpectations(t)
}

// Test convertMediaPart edge cases - currently 55.6% coverage
func TestConvertMediaPart_AudioFromStorage(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "audio-ref-456"
	data := "YXVkaW8gZGF0YQ==" // "audio data" in base64

	expectedMedia := &types.MediaContent{
		Data:     &data,
		MIMEType: "audio/mp3",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(expectedMedia, nil)

	turnPart := config.TurnContentPart{
		Type: "audio",
		Media: &config.TurnMediaContent{
			StorageReference: ref,
		},
	}

	cfg := mediaConversionConfig{
		turnPart:       turnPart,
		baseDir:        "",
		httpLoader:     nil,
		storageService: mockStorage,
		index:          0,
		contentType:    "audio",
	}

	part, err := convertMediaPart(ctx, cfg,
		func(data, mimeType string) types.ContentPart {
			return types.NewAudioPartFromData(data, mimeType)
		},
		func(filePath, baseDir string, idx int) (types.ContentPart, error) {
			return loadAudioFromFile(filePath, baseDir, idx)
		},
	)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeAudio, part.Type)
	mockStorage.AssertExpectations(t)
}

func TestConvertMediaPart_VideoFromStorage(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "video-ref-789"
	data := "dmlkZW8gZGF0YQ==" // "video data" in base64

	expectedMedia := &types.MediaContent{
		Data:     &data,
		MIMEType: "video/mp4",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(expectedMedia, nil)

	turnPart := config.TurnContentPart{
		Type: "video",
		Media: &config.TurnMediaContent{
			StorageReference: ref,
		},
	}

	cfg := mediaConversionConfig{
		turnPart:       turnPart,
		baseDir:        "",
		httpLoader:     nil,
		storageService: mockStorage,
		index:          0,
		contentType:    "video",
	}

	part, err := convertMediaPart(ctx, cfg,
		func(data, mimeType string) types.ContentPart {
			return types.NewVideoPartFromData(data, mimeType)
		},
		func(filePath, baseDir string, idx int) (types.ContentPart, error) {
			return loadVideoFromFile(filePath, baseDir, idx)
		},
	)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeVideo, part.Type)
	mockStorage.AssertExpectations(t)
}

// Test convertImagePart storage reference path - part of 70.8% coverage gap
func TestConvertImagePart_StorageReference(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "image-ref-123"
	data := "aW1hZ2UgZGF0YQ==" // "image data" in base64

	expectedMedia := &types.MediaContent{
		Data:     &data,
		MIMEType: "image/png",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(expectedMedia, nil)

	turnPart := config.TurnContentPart{
		Type: "image",
		Media: &config.TurnMediaContent{
			StorageReference: ref,
			Detail:           "high",
		},
	}

	part, err := convertImagePart(ctx, turnPart, "", nil, mockStorage, 0)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeImage, part.Type)
	assert.NotNil(t, part.Media)
	assert.NotNil(t, part.Media.Detail)
	assert.Equal(t, "high", *part.Media.Detail)
	mockStorage.AssertExpectations(t)
}

func TestConvertImagePart_URLWithHTTPLoader(t *testing.T) {
	// Create a test server that returns fake image data
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake image data"))
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("unable to start test server listener: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = l
	server.Start()
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	turnPart := config.TurnContentPart{
		Type: "image",
		Media: &config.TurnMediaContent{
			URL:    server.URL + "/image.jpg",
			Detail: "low",
		},
	}

	part, err := convertImagePart(ctx, turnPart, "", httpLoader, nil, 0)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeImage, part.Type)
	assert.NotNil(t, part.Media)
	assert.NotNil(t, part.Media.Data)
	assert.NotNil(t, part.Media.Detail)
	assert.Equal(t, "low", *part.Media.Detail)
}

func TestConvertImagePart_URLWithoutHTTPLoader(t *testing.T) {
	// Test the path where httpLoader is nil - provider will fetch
	turnPart := config.TurnContentPart{
		Type: "image",
		Media: &config.TurnMediaContent{
			URL:    "https://example.com/image.png",
			Detail: "auto",
		},
	}

	part, err := convertImagePart(context.Background(), turnPart, "", nil, nil, 0)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeImage, part.Type)
	assert.NotNil(t, part.Media)
	assert.NotNil(t, part.Media.URL)
	assert.Equal(t, "https://example.com/image.png", *part.Media.URL)
	assert.NotNil(t, part.Media.Detail)
	assert.Equal(t, "auto", *part.Media.Detail)
}

// Test HTTP loader edge cases - currently 82.8% coverage
func TestHTTPMediaLoader_UnsupportedScheme(t *testing.T) {
	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	_, _, err := httpLoader.loadMediaFromURL(ctx, "ftp://example.com/file.jpg", "image", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported URL scheme")
}

func TestHTTPMediaLoader_ContextCancelled(t *testing.T) {
	// Create a server that delays response
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("unable to start test server listener: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = l
	server.Start()
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err = httpLoader.loadMediaFromURL(ctx, server.URL, "image", 0)

	assert.Error(t, err)
}

func TestHTTPMediaLoader_NonOKStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("unable to start test server listener: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = l
	server.Start()
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	_, _, err = httpLoader.loadMediaFromURL(ctx, server.URL, "image", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestHTTPMediaLoader_ContentLengthExceedsLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20000000") // 20MB
		w.WriteHeader(http.StatusOK)
	})
	server := newLocalServer(t, handler)
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024) // 10MB limit
	ctx := context.Background()

	_, _, err := httpLoader.loadMediaFromURL(ctx, server.URL, "image", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestHTTPMediaLoader_BodyExceedsLimit(t *testing.T) {
	server := newLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write more than the limit
		largeData := make([]byte, 11*1024*1024) // 11MB
		w.Write(largeData)
	}))
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024) // 10MB limit
	ctx := context.Background()

	_, _, err := httpLoader.loadMediaFromURL(ctx, server.URL, "video", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestHTTPMediaLoader_ContentTypeFromHeader(t *testing.T) {
	server := newLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set explicit Content-Type that overrides URL extension
		w.Header().Set("Content-Type", "audio/wav")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test data"))
	}))
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	// URL says .mp3, but header says wav - header should win
	_, mimeType, err := httpLoader.loadMediaFromURL(ctx, server.URL+"/file.mp3", "audio", 0)

	assert.NoError(t, err)
	// Should use Content-Type from header, not URL extension
	assert.Equal(t, "audio/wav", mimeType)
}

func TestHTTPMediaLoader_MaxRedirects(t *testing.T) {
	redirectCount := 0
	server := newLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount <= 11 { // More than 10 redirects
			http.Redirect(w, r, "/redirect", http.StatusFound)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	_, _, err := httpLoader.loadMediaFromURL(ctx, server.URL, "image", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redirects")
}

// Test loadMediaFile edge cases - currently 87.5% coverage
func TestLoadMediaFile_FileNotFound(t *testing.T) {
	_, _, err := loadMediaFile("/nonexistent/path/file.jpg", "image", 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file does not exist")
}

func TestLoadMediaFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.png")
	testData := []byte("test image data")

	err := os.WriteFile(testFile, testData, 0644)
	assert.NoError(t, err)

	base64Data, mimeType, err := loadMediaFile(testFile, "image", 0)

	assert.NoError(t, err)
	assert.NotEmpty(t, base64Data)
	assert.Equal(t, types.MIMETypeImagePNG, mimeType)
}

// Test audio/video file loading - currently 80% coverage
func TestLoadAudioFromFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "audio.wav")
	audioData := []byte("fake wav data")

	err := os.WriteFile(audioFile, audioData, 0644)
	assert.NoError(t, err)

	part, err := loadAudioFromFile("audio.wav", tmpDir, 0)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeAudio, part.Type)
	assert.NotNil(t, part.Media)
	assert.Equal(t, types.MIMETypeAudioWAV, part.Media.MIMEType)
}

func TestLoadAudioFromFile_FileNotFound(t *testing.T) {
	part, err := loadAudioFromFile("missing.mp3", "/tmp", 0)

	assert.Error(t, err)
	assert.Empty(t, part.Type)
}

func TestLoadVideoFromFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	videoFile := filepath.Join(tmpDir, "video.webm")
	videoData := []byte("fake webm data")

	err := os.WriteFile(videoFile, videoData, 0644)
	assert.NoError(t, err)

	part, err := loadVideoFromFile("video.webm", tmpDir, 1)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeVideo, part.Type)
	assert.NotNil(t, part.Media)
	assert.Equal(t, types.MIMETypeVideoWebM, part.Media.MIMEType)
}

func TestLoadVideoFromFile_FileNotFound(t *testing.T) {
	part, err := loadVideoFromFile("missing.mp4", "/tmp", 2)

	assert.Error(t, err)
	assert.Empty(t, part.Type)
}

// Integration tests with storage service
func TestConvertTurnPartsToMessageParts_WithStorageService(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "stored-image"
	data := "aW1hZ2UgZGF0YQ==" // "image data" in base64

	expectedMedia := &types.MediaContent{
		Data:     &data,
		MIMEType: "image/jpeg",
	}

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).Return(expectedMedia, nil)

	turnParts := []config.TurnContentPart{
		{Type: "text", Text: "Check this image:"},
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				StorageReference: ref,
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(ctx, turnParts, "", nil, mockStorage)

	assert.NoError(t, err)
	assert.Len(t, parts, 2)
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, parts[1].Type)
	mockStorage.AssertExpectations(t)
}

func TestConvertTurnPartsToMessageParts_StorageError(t *testing.T) {
	mockStorage := new(MockStorageService)
	ctx := context.Background()
	ref := "bad-ref"

	mockStorage.On("RetrieveMedia", ctx, storage.Reference(ref)).
		Return((*types.MediaContent)(nil), errors.New("storage error"))

	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				StorageReference: ref,
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(ctx, turnParts, "", nil, mockStorage)

	assert.Error(t, err)
	assert.Nil(t, parts)
	mockStorage.AssertExpectations(t)
}

// Test audio/video URL loading
func TestConvertAudioPart_FromURL(t *testing.T) {
	server := newLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake audio data"))
	}))
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	turnPart := config.TurnContentPart{
		Type: "audio",
		Media: &config.TurnMediaContent{
			URL: server.URL + "/audio.mp3",
		},
	}

	part, err := convertAudioPart(ctx, turnPart, "", httpLoader, nil, 0)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeAudio, part.Type)
	assert.NotNil(t, part.Media)
	assert.NotNil(t, part.Media.Data)
}

func TestConvertVideoPart_FromURL(t *testing.T) {
	server := newLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake video data"))
	}))
	defer server.Close()

	httpLoader := NewHTTPMediaLoader(5*time.Second, 10*1024*1024)
	ctx := context.Background()

	turnPart := config.TurnContentPart{
		Type: "video",
		Media: &config.TurnMediaContent{
			URL: server.URL + "/video.mp4",
		},
	}

	part, err := convertVideoPart(ctx, turnPart, "", httpLoader, nil, 1)

	assert.NoError(t, err)
	assert.Equal(t, types.ContentTypeVideo, part.Type)
	assert.NotNil(t, part.Media)
	assert.NotNil(t, part.Media.Data)
}
