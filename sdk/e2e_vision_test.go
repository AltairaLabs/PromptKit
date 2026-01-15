//go:build e2e

package sdk

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Vision/Multimodal E2E Tests
//
// These tests verify image understanding capabilities across providers
// that support vision (OpenAI, Anthropic, Gemini).
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Vision
// =============================================================================

// TestE2E_Vision_ImageDescription tests basic image understanding.
func TestE2E_Vision_ImageDescription(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		// Get test image
		imageData := getTestImage(t)

		// Use WithImageData to send image with text
		resp, err := conv.Send(ctx, "What do you see in this image? Be brief.",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text, "Should return a description")

		t.Logf("Provider %s vision response: %s", provider.ID, truncate(text, 150))
	})
}

// TestE2E_Vision_ImageAnalysis tests more detailed image analysis.
func TestE2E_Vision_ImageAnalysis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping detailed vision test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		imageData := getTestImage(t)

		resp, err := conv.Send(ctx,
			"Analyze this image. List: 1) Main colors present, 2) Any shapes you can identify, 3) Overall description in one sentence.",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Should contain some analytical content
		assert.True(t,
			strings.Contains(text, "color") ||
				strings.Contains(text, "shape") ||
				strings.Contains(text, "image"),
			"Response should contain analytical content")

		t.Logf("Provider %s analysis: %s", provider.ID, truncate(resp.Text(), 200))
	})
}

// TestE2E_Vision_URLImage tests image from URL.
func TestE2E_Vision_URLImage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping URL image test in short mode")
	}

	EnsureTestPacks(t)

	// Use a stable public test image
	testImageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/300px-PNG_transparency_demonstration_1.png"

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx,
			"What do you see in this image?",
			WithImageURL(testImageURL))

		// Some providers may not support URL images
		if err != nil && strings.Contains(err.Error(), "url") {
			t.Skipf("Provider %s may not support URL images: %v", provider.ID, err)
		}

		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())

		t.Logf("Provider %s URL image: %s", provider.ID, truncate(resp.Text(), 150))
	})
}

// =============================================================================
// Video Tests (Gemini-specific) - TODO
// =============================================================================

// TestE2E_Vision_Video tests video understanding (Gemini only).
func TestE2E_Vision_Video(t *testing.T) {
	t.Skip("Video test requires video file - implement when test assets available")
}

// =============================================================================
// Test Image Helpers
// =============================================================================

// getTestImage returns test image data as bytes.
func getTestImage(t *testing.T) []byte {
	t.Helper()
	// Generate a minimal valid PNG (1x1 red pixel)
	return createMinimalPNG(255, 0, 0) // Red
}

// createMinimalPNG creates a minimal valid 1x1 PNG with the given RGB color.
func createMinimalPNG(r, g, b byte) []byte {
	// Base64-encoded minimal 1x1 PNG template
	if r == 255 && g == 0 && b == 0 {
		// Red 1x1 PNG
		data, _ := base64.StdEncoding.DecodeString(
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==")
		return data
	}
	// Blue 1x1 PNG (default)
	data, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPj/HwADBwIAMCbHYQAAAABJRU5ErkJggg==")
	return data
}
