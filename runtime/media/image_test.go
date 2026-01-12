package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// createTestImage creates a test image with the specified dimensions.
func createTestImage(width, height int, format string) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a solid color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}

	var buf bytes.Buffer
	switch format {
	case "png":
		_ = png.Encode(&buf, img)
	default: // jpeg
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: DefaultQuality})
	}
	return buf.Bytes()
}

// testResizeConfig returns a standard test configuration with optional overrides.
func testResizeConfig(opts ...func(*ImageResizeConfig)) ImageResizeConfig {
	cfg := ImageResizeConfig{
		MaxWidth:            DefaultMaxWidth,
		MaxHeight:           DefaultMaxHeight,
		Quality:             DefaultQuality,
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func TestResizeImage_BasicResize(t *testing.T) {
	testData := createTestImage(2048, 2048, "jpeg")
	config := testResizeConfig()

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if !result.WasResized {
		t.Error("Expected image to be resized")
	}

	if result.Width > 1024 || result.Height > 1024 {
		t.Errorf("Expected dimensions <= 1024, got %dx%d", result.Width, result.Height)
	}

	if result.Format != "jpeg" {
		t.Errorf("Expected format 'jpeg', got '%s'", result.Format)
	}
}

func TestResizeImage_NoResizeNeeded(t *testing.T) {
	testData := createTestImage(512, 512, "jpeg")
	config := testResizeConfig()

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.WasResized {
		t.Error("Expected image to NOT be resized")
	}

	if result.Width != 512 || result.Height != 512 {
		t.Errorf("Expected dimensions 512x512, got %dx%d", result.Width, result.Height)
	}

	// Data should be unchanged
	if !bytes.Equal(result.Data, testData) {
		t.Error("Expected original data to be returned unchanged")
	}
}

func TestResizeImage_AspectRatioPreserved(t *testing.T) {
	testData := createTestImage(2000, 1000, "jpeg")
	config := testResizeConfig()

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	// Width should be limited to 1024, height should scale proportionally
	if result.Width != 1024 {
		t.Errorf("Expected width 1024, got %d", result.Width)
	}

	// Height should be approximately 512 (1024/2)
	expectedHeight := 512
	tolerance := 2 // Allow small rounding differences
	if result.Height < expectedHeight-tolerance || result.Height > expectedHeight+tolerance {
		t.Errorf("Expected height ~%d, got %d", expectedHeight, result.Height)
	}
}

func TestResizeImage_FormatConversion(t *testing.T) {
	testData := createTestImage(512, 512, "png")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.Format = "jpeg"
		c.SkipIfSmaller = false
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.Format != "jpeg" {
		t.Errorf("Expected format 'jpeg', got '%s'", result.Format)
	}

	if result.MIMEType != "image/jpeg" {
		t.Errorf("Expected MIME type 'image/jpeg', got '%s'", result.MIMEType)
	}
}

func TestResizeImage_EmptyData(t *testing.T) {
	config := DefaultImageResizeConfig()
	_, err := ResizeImage(nil, config)
	if err == nil {
		t.Error("Expected error for empty data")
	}
}

func TestResizeImage_InvalidData(t *testing.T) {
	config := DefaultImageResizeConfig()
	_, err := ResizeImage([]byte("not an image"), config)
	if err == nil {
		t.Error("Expected error for invalid image data")
	}
}

func TestDefaultImageResizeConfig(t *testing.T) {
	config := DefaultImageResizeConfig()

	if config.MaxWidth != 1024 {
		t.Errorf("Expected MaxWidth 1024, got %d", config.MaxWidth)
	}
	if config.MaxHeight != 1024 {
		t.Errorf("Expected MaxHeight 1024, got %d", config.MaxHeight)
	}
	if config.Quality != 85 {
		t.Errorf("Expected Quality 85, got %d", config.Quality)
	}
	if !config.PreserveAspectRatio {
		t.Error("Expected PreserveAspectRatio to be true")
	}
	if !config.SkipIfSmaller {
		t.Error("Expected SkipIfSmaller to be true")
	}
}

func TestCalculateTargetDimensions(t *testing.T) {
	tests := []struct {
		name           string
		origW, origH   int
		maxW, maxH     int
		preserveAspect bool
		wantW, wantH   int
	}{
		{
			name:           "no resize needed",
			origW:          512,
			origH:          512,
			maxW:           1024,
			maxH:           1024,
			preserveAspect: true,
			wantW:          512,
			wantH:          512,
		},
		{
			name:           "width exceeds limit",
			origW:          2000,
			origH:          1000,
			maxW:           1000,
			maxH:           1000,
			preserveAspect: true,
			wantW:          1000,
			wantH:          500,
		},
		{
			name:           "height exceeds limit",
			origW:          1000,
			origH:          2000,
			maxW:           1000,
			maxH:           1000,
			preserveAspect: true,
			wantW:          500,
			wantH:          1000,
		},
		{
			name:           "both exceed, width dominant",
			origW:          3000,
			origH:          1500,
			maxW:           1000,
			maxH:           1000,
			preserveAspect: true,
			wantW:          1000,
			wantH:          500,
		},
		{
			name:           "no aspect ratio preservation",
			origW:          2000,
			origH:          1000,
			maxW:           800,
			maxH:           600,
			preserveAspect: false,
			wantW:          800,
			wantH:          600,
		},
		{
			name:           "no limits",
			origW:          5000,
			origH:          3000,
			maxW:           0,
			maxH:           0,
			preserveAspect: true,
			wantW:          5000,
			wantH:          3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := calculateTargetDimensions(tt.origW, tt.origH, tt.maxW, tt.maxH, tt.preserveAspect)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("calculateTargetDimensions() = %dx%d, want %dx%d", gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestFormatToMIMEType(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"jpeg", "image/jpeg"},
		{"jpg", "image/jpeg"},
		{"png", "image/png"},
		{"gif", "image/gif"},
		{"webp", "image/webp"},
		{"unknown", "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := formatToMIMEType(tt.format)
			if got != tt.want {
				t.Errorf("formatToMIMEType(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestMIMETypeToFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		want     string
	}{
		{"image/jpeg", "jpeg"},
		{"image/png", "png"},
		{"image/gif", "gif"},
		{"image/webp", "webp"},
		{"image/unknown", "jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := MIMETypeToFormat(tt.mimeType)
			if got != tt.want {
				t.Errorf("MIMETypeToFormat(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestResizeImage_PNGOutput(t *testing.T) {
	testData := createTestImage(2048, 2048, "jpeg")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.Format = "png"
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.Format != "png" {
		t.Errorf("Expected format 'png', got '%s'", result.Format)
	}
	if result.MIMEType != MIMETypePNG {
		t.Errorf("Expected MIME type '%s', got '%s'", MIMETypePNG, result.MIMEType)
	}
}

func TestResizeImage_MaxSizeBytes(t *testing.T) {
	testData := createTestImage(1024, 1024, "jpeg")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.MaxSizeBytes = 1000
		c.Quality = 95
		c.SkipIfSmaller = false
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.NewSize > result.OriginalSize {
		t.Errorf("Expected compressed output, got larger: %d > %d", result.NewSize, result.OriginalSize)
	}
}

func TestResizeImage_ZeroQuality(t *testing.T) {
	testData := createTestImage(2048, 2048, "jpeg")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.Quality = 0
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if !result.WasResized {
		t.Error("Expected image to be resized")
	}
}

func TestResizeImage_UnknownFormat(t *testing.T) {
	testData := createTestImage(2048, 2048, "jpeg")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.Format = "unknown_format"
		c.SkipIfSmaller = false
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.MIMEType != MIMETypeJPEG {
		t.Errorf("Expected MIME type '%s' for unknown format, got '%s'", MIMETypeJPEG, result.MIMEType)
	}
}

func TestResizeImage_TinyDimensions(t *testing.T) {
	testData := createTestImage(100, 10, "jpeg")
	config := testResizeConfig(func(c *ImageResizeConfig) {
		c.MaxWidth = 1
		c.MaxHeight = 1
		c.SkipIfSmaller = false
	})

	result, err := ResizeImage(testData, config)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if result.Width < 1 || result.Height < 1 {
		t.Errorf("Expected minimum dimensions of 1x1, got %dx%d", result.Width, result.Height)
	}
}
