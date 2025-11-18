package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTextPart(t *testing.T) {
	text := "Hello, world!"
	part := NewTextPart(text)

	if part.Type != ContentTypeText {
		t.Errorf("expected type %s, got %s", ContentTypeText, part.Type)
	}
	if part.Text == nil || *part.Text != text {
		t.Errorf("expected text %q, got %v", text, part.Text)
	}
	if part.Media != nil {
		t.Errorf("expected nil media, got %v", part.Media)
	}
}

func TestNewImagePartFromURL(t *testing.T) {
	url := "https://example.com/image.jpg"
	detail := "high"
	part := NewImagePartFromURL(url, &detail)

	if part.Type != ContentTypeImage {
		t.Errorf("expected type %s, got %s", ContentTypeImage, part.Type)
	}
	if part.Media == nil {
		t.Fatal("expected media, got nil")
	}
	if part.Media.URL == nil || *part.Media.URL != url {
		t.Errorf("expected URL %q, got %v", url, part.Media.URL)
	}
	if part.Media.MIMEType != MIMETypeImageJPEG {
		t.Errorf("expected MIME type %s, got %s", MIMETypeImageJPEG, part.Media.MIMEType)
	}
	if part.Media.Detail == nil || *part.Media.Detail != detail {
		t.Errorf("expected detail %q, got %v", detail, part.Media.Detail)
	}
}

func TestNewImagePartFromData(t *testing.T) {
	data := "base64encodeddata"
	mimeType := MIMETypeImagePNG
	detail := "low"
	part := NewImagePartFromData(data, mimeType, &detail)

	if part.Type != ContentTypeImage {
		t.Errorf("expected type %s, got %s", ContentTypeImage, part.Type)
	}
	if part.Media == nil {
		t.Fatal("expected media, got nil")
	}
	if part.Media.Data == nil || *part.Media.Data != data {
		t.Errorf("expected data %q, got %v", data, part.Media.Data)
	}
	if part.Media.MIMEType != mimeType {
		t.Errorf("expected MIME type %s, got %s", mimeType, part.Media.MIMEType)
	}
}

func TestNewAudioPartFromData(t *testing.T) {
	data := "base64encodedaudio"
	mimeType := MIMETypeAudioMP3
	part := NewAudioPartFromData(data, mimeType)

	if part.Type != ContentTypeAudio {
		t.Errorf("expected type %s, got %s", ContentTypeAudio, part.Type)
	}
	if part.Media == nil {
		t.Fatal("expected media, got nil")
	}
	if part.Media.Data == nil || *part.Media.Data != data {
		t.Errorf("expected data %q, got %v", data, part.Media.Data)
	}
	if part.Media.MIMEType != mimeType {
		t.Errorf("expected MIME type %s, got %s", mimeType, part.Media.MIMEType)
	}
}

func TestNewVideoPartFromData(t *testing.T) {
	data := "base64encodedvideo"
	mimeType := MIMETypeVideoMP4
	part := NewVideoPartFromData(data, mimeType)

	if part.Type != ContentTypeVideo {
		t.Errorf("expected type %s, got %s", ContentTypeVideo, part.Type)
	}
	if part.Media == nil {
		t.Fatal("expected media, got nil")
	}
	if part.Media.Data == nil || *part.Media.Data != data {
		t.Errorf("expected data %q, got %v", data, part.Media.Data)
	}
	if part.Media.MIMEType != mimeType {
		t.Errorf("expected MIME type %s, got %s", mimeType, part.Media.MIMEType)
	}
}

func TestContentPartValidate(t *testing.T) {
	tests := []struct {
		name    string
		part    ContentPart
		wantErr bool
	}{
		{
			name: "valid text part",
			part: ContentPart{
				Type: ContentTypeText,
				Text: stringPtr("Hello"),
			},
			wantErr: false,
		},
		{
			name: "invalid text part - empty text",
			part: ContentPart{
				Type: ContentTypeText,
				Text: stringPtr(""),
			},
			wantErr: true,
		},
		{
			name: "invalid text part - nil text",
			part: ContentPart{
				Type: ContentTypeText,
				Text: nil,
			},
			wantErr: true,
		},
		{
			name: "valid image part with data",
			part: ContentPart{
				Type: ContentTypeImage,
				Media: &MediaContent{
					Data:     stringPtr("base64data"),
					MIMEType: MIMETypeImageJPEG,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid image part - nil media",
			part: ContentPart{
				Type:  ContentTypeImage,
				Media: nil,
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			part: ContentPart{
				Type: "invalid",
				Text: stringPtr("test"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.part.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMediaContentValidate(t *testing.T) {
	tests := []struct {
		name    string
		media   MediaContent
		wantErr bool
	}{
		{
			name: "valid with data",
			media: MediaContent{
				Data:     stringPtr("base64data"),
				MIMEType: MIMETypeImageJPEG,
			},
			wantErr: false,
		},
		{
			name: "valid with file path",
			media: MediaContent{
				FilePath: stringPtr("/path/to/image.jpg"),
				MIMEType: MIMETypeImageJPEG,
			},
			wantErr: false,
		},
		{
			name: "valid with URL",
			media: MediaContent{
				URL:      stringPtr("https://example.com/image.jpg"),
				MIMEType: MIMETypeImageJPEG,
			},
			wantErr: false,
		},
		{
			name: "invalid - no data source",
			media: MediaContent{
				MIMEType: MIMETypeImageJPEG,
			},
			wantErr: true,
		},
		{
			name: "invalid - multiple data sources",
			media: MediaContent{
				Data:     stringPtr("base64data"),
				FilePath: stringPtr("/path/to/image.jpg"),
				MIMEType: MIMETypeImageJPEG,
			},
			wantErr: true,
		},
		{
			name: "invalid - no MIME type",
			media: MediaContent{
				Data: stringPtr("base64data"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.media.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMediaContentGetBase64Data(t *testing.T) {
	// Test with direct base64 data
	t.Run("with base64 data", func(t *testing.T) {
		data := "base64encodeddata"
		media := MediaContent{
			Data:     &data,
			MIMEType: MIMETypeImageJPEG,
		}

		result, err := media.GetBase64Data()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != data {
			t.Errorf("expected %q, got %q", data, result)
		}
	})

	// Test with file path
	t.Run("with file path", func(t *testing.T) {
		// Create a temporary file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.jpg")
		content := []byte("test image data")
		if err := os.WriteFile(tmpFile, content, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		media := MediaContent{
			FilePath: &tmpFile,
			MIMEType: MIMETypeImageJPEG,
		}

		result, err := media.GetBase64Data()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify the base64-encoded content is correct
		if !strings.Contains(result, "dGVzdCBpbWFnZSBkYXRh") { // base64 of "test image data"
			t.Errorf("base64 data doesn't match expected content")
		}
	})

	// Test with URL (should error)
	t.Run("with URL", func(t *testing.T) {
		url := "https://example.com/image.jpg"
		media := MediaContent{
			URL:      &url,
			MIMEType: MIMETypeImageJPEG,
		}

		_, err := media.GetBase64Data()
		if err == nil {
			t.Error("expected error for URL, got nil")
		}
	})
}

func TestMediaContentReadData(t *testing.T) {
	// Test with base64 data
	t.Run("with base64 data", func(t *testing.T) {
		// Base64 encode "test data"
		data := "dGVzdCBkYXRh"
		media := MediaContent{
			Data:     &data,
			MIMEType: MIMETypeImageJPEG,
		}

		reader, err := media.ReadData()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer reader.Close()

		// Read and verify content
		content := make([]byte, 100)
		n, _ := reader.Read(content)
		result := string(content[:n])
		if result != "test data" {
			t.Errorf("expected %q, got %q", "test data", result)
		}
	})

	// Test with file path
	t.Run("with file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.jpg")
		expectedContent := []byte("test image data")
		if err := os.WriteFile(tmpFile, expectedContent, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		media := MediaContent{
			FilePath: &tmpFile,
			MIMEType: MIMETypeImageJPEG,
		}

		reader, err := media.ReadData()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer reader.Close()

		content := make([]byte, 100)
		n, _ := reader.Read(content)
		result := content[:n]
		if string(result) != string(expectedContent) {
			t.Errorf("expected %q, got %q", expectedContent, result)
		}
	})

	// Test with URL (should error)
	t.Run("with URL", func(t *testing.T) {
		url := "https://example.com/image.jpg"
		media := MediaContent{
			URL:      &url,
			MIMEType: MIMETypeImageJPEG,
		}

		_, err := media.ReadData()
		if err == nil {
			t.Error("expected error for URL, got nil")
		}
	})
}

func TestInferMIMEType(t *testing.T) {
	tests := []struct {
		filePath string
		want     string
		wantErr  bool
	}{
		{"/path/to/image.jpg", MIMETypeImageJPEG, false},
		{"/path/to/image.jpeg", MIMETypeImageJPEG, false},
		{"/path/to/image.png", MIMETypeImagePNG, false},
		{"/path/to/image.gif", MIMETypeImageGIF, false},
		{"/path/to/image.webp", MIMETypeImageWebP, false},
		{"/path/to/audio.mp3", MIMETypeAudioMP3, false},
		{"/path/to/audio.wav", MIMETypeAudioWAV, false},
		{"/path/to/audio.ogg", MIMETypeAudioOgg, false},
		{"/path/to/audio.weba", MIMETypeAudioWebM, false},
		{"/path/to/video.mp4", MIMETypeVideoMP4, false},
		{"/path/to/video.webm", MIMETypeVideoWebM, false},
		{"/path/to/video.ogv", MIMETypeVideoOgg, false},
		{"/path/to/unknown.xyz", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got, err := inferMIMEType(tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("inferMIMEType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("inferMIMEType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

func TestNewImagePart_ErrorPath(t *testing.T) {
	// Test with unsupported extension
	tmpDir := t.TempDir()
	unsupportedFile := filepath.Join(tmpDir, "test.xyz")
	os.WriteFile(unsupportedFile, []byte("test"), 0644)
	_, err := NewImagePart(unsupportedFile, nil)
	if err == nil {
		t.Error("expected error for unsupported extension")
	} else if !strings.Contains(err.Error(), "unsupported file extension") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewAudioPart_ErrorPath(t *testing.T) {
	// Test with unsupported extension
	tmpDir := t.TempDir()
	unsupportedFile := filepath.Join(tmpDir, "test.unknown")
	os.WriteFile(unsupportedFile, []byte("test"), 0644)
	_, err := NewAudioPart(unsupportedFile)
	if err == nil {
		t.Error("expected error for unsupported extension")
	}
}

func TestNewVideoPart_ErrorPath(t *testing.T) {
	// Test with unsupported extension
	tmpDir := t.TempDir()
	unsupportedFile := filepath.Join(tmpDir, "test.xyz")
	os.WriteFile(unsupportedFile, []byte("test"), 0644)
	_, err := NewVideoPart(unsupportedFile)
	if err == nil {
		t.Error("expected error for unsupported extension")
	}
}

func TestGetBase64Data_StorageReference(t *testing.T) {
	ref := "storage-ref-123"
	media := &MediaContent{
		StorageReference: &ref,
		MIMEType:         "image/jpeg",
	}

	_, err := media.GetBase64Data()
	if err == nil {
		t.Error("expected error when getting base64 data from storage reference")
	} else if !strings.Contains(err.Error(), "storage reference") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetBase64Data_URL(t *testing.T) {
	url := "https://example.com/image.jpg"
	media := &MediaContent{
		URL:      &url,
		MIMEType: "image/jpeg",
	}

	_, err := media.GetBase64Data()
	if err == nil {
		t.Error("expected error when getting base64 data from URL")
	} else if !strings.Contains(err.Error(), "URL") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInferMIMETypeFromURL_Fallback(t *testing.T) {
	// URL with no extension should default to JPEG
	result := inferMIMETypeFromURL("https://example.com/image")
	if result != MIMETypeImageJPEG {
		t.Errorf("expected default MIME type %s, got %s", MIMETypeImageJPEG, result)
	}

	// URL with unknown extension should default to JPEG
	result = inferMIMETypeFromURL("https://example.com/file.unknown")
	if result != MIMETypeImageJPEG {
		t.Errorf("expected default MIME type %s, got %s", MIMETypeImageJPEG, result)
	}
}
