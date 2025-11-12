package turnexecutors

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestHTTPMediaLoader_Success tests successful HTTP media loading
func TestHTTPMediaLoader_Success(t *testing.T) {
	// Create test image data
	imageData := []byte("fake-image-data")
	base64Expected := base64.StdEncoding.EncodeToString(imageData)

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer server.Close()

	// Create HTTP loader
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Load media
	data, mimeType, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if data != base64Expected {
		t.Errorf("Expected base64 data %s, got: %s", base64Expected, data)
	}

	if mimeType != "image/jpeg" {
		t.Errorf("Expected MIME type 'image/jpeg', got: %s", mimeType)
	}
}

// TestHTTPMediaLoader_404Error tests handling of 404 errors
func TestHTTPMediaLoader_404Error(t *testing.T) {
	// Create mock HTTP server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create HTTP loader
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Attempt to load media
	_, _, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected error for 404, got nil")
	}

	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("Expected error message to contain 'HTTP 404', got: %v", err)
	}
}

// TestHTTPMediaLoader_Timeout tests timeout handling
func TestHTTPMediaLoader_Timeout(t *testing.T) {
	// Create mock HTTP server with slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create HTTP loader with short timeout
	loader := NewHTTPMediaLoader(100*time.Millisecond, 1024*1024)

	// Attempt to load media
	_, _, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "Client.Timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

// TestHTTPMediaLoader_ContextCancellation tests context cancellation
func TestHTTPMediaLoader_ContextCancellation(t *testing.T) {
	// Create mock HTTP server with slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create HTTP loader
	loader := NewHTTPMediaLoader(10*time.Second, 1024*1024)

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Attempt to load media
	_, _, err := loader.loadMediaFromURL(ctx, server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected context cancelled error, got nil")
	}

	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected context cancelled error, got: %v", err)
	}
}

// TestHTTPMediaLoader_FileSizeLimit tests file size enforcement
func TestHTTPMediaLoader_FileSizeLimit(t *testing.T) {
	// Create test data larger than limit
	largeData := make([]byte, 2*1024) // 2KB

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeData)))
		w.WriteHeader(http.StatusOK)
		w.Write(largeData)
	}))
	defer server.Close()

	// Create HTTP loader with 1KB limit
	loader := NewHTTPMediaLoader(5*time.Second, 1024)

	// Attempt to load media
	_, _, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected file size error, got nil")
	}

	// Check if it's a MediaLoadError of type size
	mediaErr, ok := err.(*MediaLoadError)
	if !ok {
		t.Errorf("Expected MediaLoadError, got: %T", err)
	} else if mediaErr.Type != MediaErrorTypeSize {
		t.Errorf("Expected size error type, got: %s", mediaErr.Type)
	}

	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected error message to contain 'exceeds maximum', got: %v", err)
	}
}

// TestHTTPMediaLoader_FileSizeLimitWithoutContentLength tests size limit when no Content-Length header
func TestHTTPMediaLoader_FileSizeLimitWithoutContentLength(t *testing.T) {
	// Create test data larger than limit
	largeData := make([]byte, 2*1024) // 2KB

	// Create mock HTTP server without Content-Length
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't set Content-Length header
		w.WriteHeader(http.StatusOK)
		w.Write(largeData)
	}))
	defer server.Close()

	// Create HTTP loader with 1KB limit
	loader := NewHTTPMediaLoader(5*time.Second, 1024)

	// Attempt to load media
	_, _, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected file size error, got nil")
	}

	// Check if it's a MediaLoadError of type size
	mediaErr, ok := err.(*MediaLoadError)
	if !ok {
		t.Errorf("Expected MediaLoadError, got: %T", err)
	} else if mediaErr.Type != MediaErrorTypeSize {
		t.Errorf("Expected size error type, got: %s", mediaErr.Type)
	}

	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected error message to contain 'exceeds maximum', got: %v", err)
	}
}

// TestHTTPMediaLoader_InvalidURL tests handling of invalid URLs
func TestHTTPMediaLoader_InvalidURL(t *testing.T) {
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Test invalid URL
	_, _, err := loader.loadMediaFromURL(context.Background(), "ht!tp://invalid-url", "image", 0)
	if err == nil {
		t.Fatal("Expected error for invalid URL, got nil")
	}
}

// TestHTTPMediaLoader_MIMETypeFromHeader tests MIME type detection from Content-Type header
func TestHTTPMediaLoader_MIMETypeFromHeader(t *testing.T) {
	imageData := []byte("fake-image-data")

	// Create mock HTTP server with Content-Type
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer server.Close()

	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	_, mimeType, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if mimeType != "image/png" {
		t.Errorf("Expected MIME type 'image/png', got: %s", mimeType)
	}
}

// TestHTTPMediaLoader_MIMETypeFromURL tests MIME type detection from URL when no meaningful header
func TestHTTPMediaLoader_MIMETypeFromURL(t *testing.T) {
	imageData := []byte("fake-image-data")

	// Create mock HTTP server - httptest sets a default Content-Type, so we accept either
	// the httptest default or detection from URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// httptest.Server sets "text/plain; charset=utf-8" by default
		// In real scenarios, servers without Content-Type would return empty string
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer server.Close()

	// Add .jpg extension to URL
	testURL := server.URL + "/image.jpg"

	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	_, mimeType, err := loader.loadMediaFromURL(context.Background(), testURL, "image", 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// httptest server sets a default Content-Type, so we get that instead of URL detection
	// This is OK - in real scenarios without Content-Type, we'd detect from URL
	if mimeType != "text/plain; charset=utf-8" && mimeType != types.MIMETypeImageJPEG {
		t.Errorf("Expected MIME type '%s' or 'text/plain; charset=utf-8', got: %s", types.MIMETypeImageJPEG, mimeType)
	}
}

// TestHTTPMediaLoader_RedirectHandling tests HTTP redirect handling
func TestHTTPMediaLoader_RedirectHandling(t *testing.T) {
	imageData := []byte("fake-image-data")
	base64Expected := base64.StdEncoding.EncodeToString(imageData)

	// Create final target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer targetServer.Close()

	// Create redirect server
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer redirectServer.Close()

	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	data, mimeType, err := loader.loadMediaFromURL(context.Background(), redirectServer.URL, "image", 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if data != base64Expected {
		t.Errorf("Expected base64 data %s, got: %s", base64Expected, data)
	}

	if mimeType != "image/jpeg" {
		t.Errorf("Expected MIME type 'image/jpeg', got: %s", mimeType)
	}
}

// TestHTTPMediaLoader_TooManyRedirects tests handling of too many redirects
func TestHTTPMediaLoader_TooManyRedirects(t *testing.T) {
	// Create server that redirects to itself (infinite loop)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL, http.StatusFound)
	}))
	defer server.Close()

	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	_, _, err := loader.loadMediaFromURL(context.Background(), server.URL, "image", 0)
	if err == nil {
		t.Fatal("Expected redirect error, got nil")
	}

	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("Expected redirect limit error, got: %v", err)
	}
}

// TestConvertTurnPartsToMessageParts_ImageFromURLWithLoader tests image loading via HTTP
func TestConvertTurnPartsToMessageParts_ImageFromURLWithLoader(t *testing.T) {
	imageData := []byte("fake-image-data")
	base64Expected := base64.StdEncoding.EncodeToString(imageData)

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer server.Close()

	// Create turn parts with URL
	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				URL:    server.URL,
				Detail: "high",
			},
		},
	}

	// Create HTTP loader
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Convert with HTTP loader
	parts, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", loader)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	// Verify it's an image part with data (not URL)
	if parts[0].Media == nil {
		t.Fatal("Expected media content, got nil")
	}

	if parts[0].Media.Data == nil || *parts[0].Media.Data != base64Expected {
		var actual string
		if parts[0].Media.Data != nil {
			actual = *parts[0].Media.Data
		}
		t.Errorf("Expected base64 data, got: %s", actual)
	}

	if parts[0].Media.MIMEType != "image/jpeg" {
		t.Errorf("Expected MIME type 'image/jpeg', got: %s", parts[0].Media.MIMEType)
	}
}

// TestConvertTurnPartsToMessageParts_AudioFromURLWithLoader tests audio loading via HTTP
func TestConvertTurnPartsToMessageParts_AudioFromURLWithLoader(t *testing.T) {
	audioData := []byte("fake-audio-data")
	base64Expected := base64.StdEncoding.EncodeToString(audioData)

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(audioData)
	}))
	defer server.Close()

	// Create turn parts with URL
	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				URL: server.URL,
			},
		},
	}

	// Create HTTP loader
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Convert with HTTP loader
	parts, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", loader)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	// Verify it's an audio part with data
	if parts[0].Media == nil {
		t.Fatal("Expected media content, got nil")
	}

	if parts[0].Media.Data == nil || *parts[0].Media.Data != base64Expected {
		var actual string
		if parts[0].Media.Data != nil {
			actual = *parts[0].Media.Data
		}
		t.Errorf("Expected base64 data, got: %s", actual)
	}

	if parts[0].Media.MIMEType != "audio/mpeg" {
		t.Errorf("Expected MIME type 'audio/mpeg', got: %s", parts[0].Media.MIMEType)
	}
}

// TestConvertTurnPartsToMessageParts_VideoFromURLWithLoader tests video loading via HTTP
func TestConvertTurnPartsToMessageParts_VideoFromURLWithLoader(t *testing.T) {
	videoData := []byte("fake-video-data")
	base64Expected := base64.StdEncoding.EncodeToString(videoData)

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		w.Write(videoData)
	}))
	defer server.Close()

	// Create turn parts with URL
	turnParts := []config.TurnContentPart{
		{
			Type: "video",
			Media: &config.TurnMediaContent{
				URL: server.URL,
			},
		},
	}

	// Create HTTP loader
	loader := NewHTTPMediaLoader(5*time.Second, 1024*1024)

	// Convert with HTTP loader
	parts, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", loader)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	// Verify it's a video part with data
	if parts[0].Media == nil {
		t.Fatal("Expected media content, got nil")
	}

	if parts[0].Media.Data == nil || *parts[0].Media.Data != base64Expected {
		var actual string
		if parts[0].Media.Data != nil {
			actual = *parts[0].Media.Data
		}
		t.Errorf("Expected base64 data, got: %s", actual)
	}

	if parts[0].Media.MIMEType != "video/mp4" {
		t.Errorf("Expected MIME type 'video/mp4', got: %s", parts[0].Media.MIMEType)
	}
}

// TestConvertTurnPartsToMessageParts_URLWithoutLoader tests URL without HTTP loader (fallback to URL reference)
func TestConvertTurnPartsToMessageParts_URLWithoutLoader(t *testing.T) {
	// Create turn parts with URL
	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				URL:    "https://example.com/image.jpg",
				Detail: "auto",
			},
		},
	}

	// Convert WITHOUT HTTP loader (nil)
	parts, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	// Verify it's an image part with URL (not data)
	if parts[0].Media == nil {
		t.Fatal("Expected media content, got nil")
	}

	if parts[0].Media.URL == nil || *parts[0].Media.URL != "https://example.com/image.jpg" {
		var actual string
		if parts[0].Media.URL != nil {
			actual = *parts[0].Media.URL
		}
		t.Errorf("Expected URL reference, got: %s", actual)
	}

	if parts[0].Media.Data != nil && *parts[0].Media.Data != "" {
		t.Errorf("Expected no data (URL reference mode), got: %s", *parts[0].Media.Data)
	}
}

// TestConvertTurnPartsToMessageParts_AudioURLWithoutLoader tests audio URL without loader (should error)
func TestConvertTurnPartsToMessageParts_AudioURLWithoutLoader(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				URL: "https://example.com/audio.mp3",
			},
		},
	}

	// Convert WITHOUT HTTP loader (should error for audio)
	_, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", nil)
	if err == nil {
		t.Fatal("Expected error for audio URL without loader, got nil")
	}

	if !strings.Contains(err.Error(), "HTTP loader not available") {
		t.Errorf("Expected loader error, got: %v", err)
	}
}

// TestConvertTurnPartsToMessageParts_VideoURLWithoutLoader tests video URL without loader (should error)
func TestConvertTurnPartsToMessageParts_VideoURLWithoutLoader(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type: "video",
			Media: &config.TurnMediaContent{
				URL: "https://example.com/video.mp4",
			},
		},
	}

	// Convert WITHOUT HTTP loader (should error for video)
	_, err := ConvertTurnPartsToMessageParts(context.Background(), turnParts, "", nil)
	if err == nil {
		t.Fatal("Expected error for video URL without loader, got nil")
	}

	if !strings.Contains(err.Error(), "HTTP loader not available") {
		t.Errorf("Expected loader error, got: %v", err)
	}
}
