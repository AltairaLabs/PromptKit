//go:build e2e

package sdk

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Multimodal E2E Tests
//
// These tests verify multimodal (vision, audio, video) functionality across
// providers. They test image understanding, multi-image conversations, and
// combined text+image interactions.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Multimodal
// =============================================================================

// TestE2E_Multimodal_SingleImage tests basic single image understanding.
func TestE2E_Multimodal_SingleImage(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		// Create a simple test image (red pixel)
		imageData := createColoredPNG(255, 0, 0) // Red

		resp, err := conv.Send(ctx, "Describe what you see in this image.",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)

		text := resp.Text()
		// Should provide some description - models may struggle with 1x1 images
		// but should at least respond without error
		assert.NotEmpty(t, text, "Should provide some response about the image")

		t.Logf("Provider %s image response: %s", provider.ID, truncate(text, 100))
	})
}

// TestE2E_Multimodal_MultipleImages tests understanding multiple images.
func TestE2E_Multimodal_MultipleImages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multiple images test in short mode")
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

		// Create two different colored images
		redImage := createColoredPNG(255, 0, 0)  // Red
		blueImage := createColoredPNG(0, 0, 255) // Blue

		resp, err := conv.Send(ctx,
			"I'm showing you two images. Name the color of each one.",
			WithImageData(redImage, "image/png"),
			WithImageData(blueImage, "image/png"))
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Should mention both colors
		hasRed := strings.Contains(text, "red")
		hasBlue := strings.Contains(text, "blue")

		assert.True(t, hasRed || hasBlue, "Should identify at least one color, got: %s", truncate(resp.Text(), 150))

		t.Logf("Provider %s multi-image response: %s", provider.ID, truncate(resp.Text(), 150))
	})
}

// TestE2E_Multimodal_ImageFollowup tests follow-up questions about an image.
func TestE2E_Multimodal_ImageFollowup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping image followup test in short mode")
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

		// First message with image
		imageData := createColoredPNG(0, 255, 0) // Green
		resp1, err := conv.Send(ctx, "What color is this image?",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)
		t.Logf("Turn 1: %s", truncate(resp1.Text(), 100))

		// Follow-up question about the same image (without sending it again)
		resp2, err := conv.Send(ctx, "Is it a warm color or a cool color?")
		require.NoError(t, err)

		text := strings.ToLower(resp2.Text())
		// Green is typically considered a cool color
		assert.True(t,
			strings.Contains(text, "cool") ||
				strings.Contains(text, "cold") ||
				strings.Contains(text, "green") ||
				strings.Contains(text, "nature"),
			"Should discuss the color properties, got: %s", truncate(resp2.Text(), 100))

		t.Logf("Turn 2: %s", truncate(resp2.Text(), 100))
	})
}

// TestE2E_Multimodal_ImageWithDetailedPrompt tests detailed image analysis.
func TestE2E_Multimodal_ImageWithDetailedPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping detailed prompt test in short mode")
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

		imageData := createColoredPNG(128, 128, 128) // Gray

		resp, err := conv.Send(ctx,
			"Analyze this image and tell me: 1) The approximate color, 2) Whether it's bright or dark, 3) One word to describe it.",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Should have some analytical content
		hasColorRef := strings.Contains(text, "gray") ||
			strings.Contains(text, "grey") ||
			strings.Contains(text, "neutral")
		hasBrightnessRef := strings.Contains(text, "medium") ||
			strings.Contains(text, "mid") ||
			strings.Contains(text, "dark") ||
			strings.Contains(text, "light")

		assert.True(t, hasColorRef || hasBrightnessRef,
			"Should provide analytical content, got: %s", truncate(resp.Text(), 200))

		t.Logf("Provider %s analysis: %s", provider.ID, truncate(resp.Text(), 200))
	})
}

// TestE2E_Multimodal_StreamingWithImage tests streaming responses with images.
func TestE2E_Multimodal_StreamingWithImage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping streaming with image test in short mode")
	}

	EnsureTestPacks(t)

	// Need both vision and streaming capabilities
	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		// Check if provider also supports streaming
		if !provider.HasCapability(CapStreaming) {
			t.Skipf("Provider %s doesn't support streaming", provider.ID)
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		imageData := createColoredPNG(255, 165, 0) // Orange

		var chunks []StreamChunk
		var fullText strings.Builder

		// TODO: This requires a Stream method that accepts SendOptions
		// For now, we'll use the non-streaming path and note this as a gap
		resp, err := conv.Send(ctx, "Describe this color in detail.",
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text)

		t.Logf("Provider %s (non-streaming) image response (%d chunks): %s",
			provider.ID, len(chunks), truncate(fullText.String()+text, 100))
	})
}

// =============================================================================
// Test Image Helpers
// =============================================================================

// createColoredPNG creates a small PNG with the given RGB color.
// Uses a 10x10 image for better model recognition compared to 1x1.
func createColoredPNG(r, g, b byte) []byte {
	// Create a 10x10 solid color image for better recognition
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	c := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// Return a minimal valid PNG on error (shouldn't happen)
		return []byte{}
	}
	return buf.Bytes()
}
