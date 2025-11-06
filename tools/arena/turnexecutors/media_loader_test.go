package turnexecutors

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestConvertTurnPartsToMessageParts_EmptyParts(t *testing.T) {
	parts, err := ConvertTurnPartsToMessageParts(nil, "")
	if err != nil {
		t.Errorf("Expected no error for nil parts, got: %v", err)
	}
	if parts != nil {
		t.Errorf("Expected nil parts, got: %v", parts)
	}

	parts, err = ConvertTurnPartsToMessageParts([]config.TurnContentPart{}, "")
	if err != nil {
		t.Errorf("Expected no error for empty parts, got: %v", err)
	}
	if parts != nil {
		t.Errorf("Expected nil parts, got: %v", parts)
	}
}

func TestConvertTurnPartsToMessageParts_TextOnly(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{Type: "text", Text: "Hello, world!"},
		{Type: "text", Text: "How are you?"},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeText || *parts[0].Text != "Hello, world!" {
		t.Errorf("First part incorrect: type=%s, text=%v", parts[0].Type, parts[0].Text)
	}

	if parts[1].Type != types.ContentTypeText || *parts[1].Text != "How are you?" {
		t.Errorf("Second part incorrect: type=%s, text=%v", parts[1].Type, parts[1].Text)
	}
}

func TestConvertTurnPartsToMessageParts_EmptyText(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{Type: "text", Text: ""},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err == nil {
		t.Error("Expected error for empty text part")
	}
}

func TestConvertTurnPartsToMessageParts_ImageFromURL(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				URL:      "https://example.com/image.jpg",
				MIMEType: "image/jpeg",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeImage {
		t.Errorf("Expected image type, got %s", parts[0].Type)
	}

	if parts[0].Media == nil || *parts[0].Media.URL != "https://example.com/image.jpg" {
		t.Error("Image URL not preserved correctly")
	}
}

func TestConvertTurnPartsToMessageParts_ImageFromData(t *testing.T) {
	testData := "dGVzdCBpbWFnZSBkYXRh" // base64 encoded "test image data"

	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				Data:     testData,
				MIMEType: "image/png",
				Detail:   "high",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeImage {
		t.Errorf("Expected image type, got %s", parts[0].Type)
	}

	if parts[0].Media == nil || *parts[0].Media.Data != testData {
		t.Error("Image data not preserved correctly")
	}

	if parts[0].Media.MIMEType != "image/png" {
		t.Errorf("Expected image/png MIME type, got %s", parts[0].Media.MIMEType)
	}

	if parts[0].Media.Detail == nil || *parts[0].Media.Detail != "high" {
		t.Error("Image detail not preserved correctly")
	}
}

func TestConvertTurnPartsToMessageParts_ImageFromFile(t *testing.T) {
	// Create temporary directory and test image file
	tmpDir := t.TempDir()
	imageFile := filepath.Join(tmpDir, "test.jpg")
	imageData := []byte("fake jpeg data")

	err := os.WriteFile(imageFile, imageData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				FilePath: "test.jpg",
				MIMEType: "image/jpeg",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, tmpDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeImage {
		t.Errorf("Expected image type, got %s", parts[0].Type)
	}

	// Verify base64 data was loaded
	if parts[0].Media == nil || parts[0].Media.Data == nil {
		t.Fatal("Image data not loaded")
	}

	// Decode and verify
	decoded, err := base64.StdEncoding.DecodeString(*parts[0].Media.Data)
	if err != nil {
		t.Fatalf("Failed to decode base64: %v", err)
	}

	if string(decoded) != string(imageData) {
		t.Errorf("Image data mismatch: got %s, want %s", decoded, imageData)
	}

	if parts[0].Media.MIMEType != types.MIMETypeImageJPEG {
		t.Errorf("Expected %s MIME type, got %s", types.MIMETypeImageJPEG, parts[0].Media.MIMEType)
	}
}

func TestConvertTurnPartsToMessageParts_ImageMissingMedia(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{Type: "image", Media: nil},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err == nil {
		t.Error("Expected error for image part with no media")
	}
}

func TestConvertTurnPartsToMessageParts_ImageNoSource(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type:  "image",
			Media: &config.TurnMediaContent{MIMEType: "image/jpeg"},
		},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err == nil {
		t.Error("Expected error for image with no URL, data, or file_path")
	}
}

func TestConvertTurnPartsToMessageParts_AudioFromData(t *testing.T) {
	testData := "YXVkaW8gZGF0YQ==" // base64 encoded "audio data"

	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				Data:     testData,
				MIMEType: "audio/mpeg",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeAudio {
		t.Errorf("Expected audio type, got %s", parts[0].Type)
	}

	if parts[0].Media == nil || *parts[0].Media.Data != testData {
		t.Error("Audio data not preserved correctly")
	}
}

func TestConvertTurnPartsToMessageParts_AudioFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.mp3")
	audioData := []byte("fake mp3 data")

	err := os.WriteFile(audioFile, audioData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				FilePath: "test.mp3",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, tmpDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeAudio {
		t.Errorf("Expected audio type, got %s", parts[0].Type)
	}

	if parts[0].Media.MIMEType != types.MIMETypeAudioMP3 {
		t.Errorf("Expected %s MIME type, got %s", types.MIMETypeAudioMP3, parts[0].Media.MIMEType)
	}
}

func TestConvertTurnPartsToMessageParts_AudioMissingMIMEType(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				Data: "YXVkaW8=",
			},
		},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err == nil {
		t.Error("Expected error for audio with inline data but no MIME type")
	}
}

func TestConvertTurnPartsToMessageParts_VideoFromData(t *testing.T) {
	testData := "dmlkZW8gZGF0YQ==" // base64 encoded "video data"

	turnParts := []config.TurnContentPart{
		{
			Type: "video",
			Media: &config.TurnMediaContent{
				Data:     testData,
				MIMEType: "video/mp4",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeVideo {
		t.Errorf("Expected video type, got %s", parts[0].Type)
	}

	if parts[0].Media == nil || *parts[0].Media.Data != testData {
		t.Error("Video data not preserved correctly")
	}
}

func TestConvertTurnPartsToMessageParts_VideoFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	videoFile := filepath.Join(tmpDir, "test.mp4")
	videoData := []byte("fake mp4 data")

	err := os.WriteFile(videoFile, videoData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	turnParts := []config.TurnContentPart{
		{
			Type: "video",
			Media: &config.TurnMediaContent{
				FilePath: "test.mp4",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, tmpDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(parts))
	}

	if parts[0].Type != types.ContentTypeVideo {
		t.Errorf("Expected video type, got %s", parts[0].Type)
	}

	if parts[0].Media.MIMEType != types.MIMETypeVideoMP4 {
		t.Errorf("Expected %s MIME type, got %s", types.MIMETypeVideoMP4, parts[0].Media.MIMEType)
	}
}

func TestConvertTurnPartsToMessageParts_FileNotFound(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				FilePath: "nonexistent.jpg",
			},
		},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "/tmp")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestConvertTurnPartsToMessageParts_UnsupportedType(t *testing.T) {
	turnParts := []config.TurnContentPart{
		{Type: "document", Text: "unsupported"},
	}

	_, err := ConvertTurnPartsToMessageParts(turnParts, "")
	if err == nil {
		t.Error("Expected error for unsupported content type")
	}
}

func TestConvertTurnPartsToMessageParts_MixedContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	imageFile := filepath.Join(tmpDir, "image.png")
	audioFile := filepath.Join(tmpDir, "audio.mp3")

	err := os.WriteFile(imageFile, []byte("fake image"), 0644)
	if err != nil {
		t.Fatalf("Failed to create image file: %v", err)
	}

	err = os.WriteFile(audioFile, []byte("fake audio"), 0644)
	if err != nil {
		t.Fatalf("Failed to create audio file: %v", err)
	}

	turnParts := []config.TurnContentPart{
		{Type: "text", Text: "Analyze this:"},
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				FilePath: "image.png",
			},
		},
		{Type: "text", Text: "And listen to this:"},
		{
			Type: "audio",
			Media: &config.TurnMediaContent{
				FilePath: "audio.mp3",
			},
		},
	}

	parts, err := ConvertTurnPartsToMessageParts(turnParts, tmpDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parts) != 4 {
		t.Fatalf("Expected 4 parts, got %d", len(parts))
	}

	// Verify types
	expectedTypes := []string{
		types.ContentTypeText,
		types.ContentTypeImage,
		types.ContentTypeText,
		types.ContentTypeAudio,
	}

	for i, expected := range expectedTypes {
		if parts[i].Type != expected {
			t.Errorf("Part %d: expected type %s, got %s", i, expected, parts[i].Type)
		}
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"image.jpg", types.MIMETypeImageJPEG},
		{"image.jpeg", types.MIMETypeImageJPEG},
		{"image.png", types.MIMETypeImagePNG},
		{"image.gif", types.MIMETypeImageGIF},
		{"image.webp", types.MIMETypeImageWebP},
		{"audio.mp3", types.MIMETypeAudioMP3},
		{"audio.wav", types.MIMETypeAudioWAV},
		{"audio.ogg", types.MIMETypeAudioOgg},
		{"audio.m4a", "audio/mp4"},
		{"video.mp4", types.MIMETypeVideoMP4},
		{"video.webm", types.MIMETypeVideoWebM},
		{"video.mov", "video/quicktime"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := detectMIMEType(tt.filename)
			if got != tt.want {
				t.Errorf("detectMIMEType(%s) = %s, want %s", tt.filename, got, tt.want)
			}
		})
	}
}

func TestDetectMIMEType_CaseInsensitive(t *testing.T) {
	tests := []string{
		"IMAGE.JPG",
		"Image.PNG",
		"AUDIO.MP3",
		"Video.MP4",
	}

	for _, filename := range tests {
		got := detectMIMEType(filename)
		if got == "application/octet-stream" {
			t.Errorf("detectMIMEType(%s) returned fallback, should detect type", filename)
		}
	}
}

func TestResolveFilePath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		baseDir  string
		wantAbs  bool
	}{
		{
			name:     "relative path",
			filePath: "test.jpg",
			baseDir:  "/base",
			wantAbs:  true,
		},
		{
			name:     "absolute path unchanged",
			filePath: "/abs/path/test.jpg",
			baseDir:  "/base",
			wantAbs:  true,
		},
		{
			name:     "nested relative path",
			filePath: "images/test.jpg",
			baseDir:  "/base",
			wantAbs:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveFilePath(tt.filePath, tt.baseDir)
			if tt.wantAbs && !filepath.IsAbs(got) {
				t.Errorf("resolveFilePath() = %s, expected absolute path", got)
			}
			if filepath.IsAbs(tt.filePath) && got != tt.filePath {
				t.Errorf("resolveFilePath() = %s, want %s (absolute path should be unchanged)", got, tt.filePath)
			}
		})
	}
}

func TestParseDetailLevel(t *testing.T) {
	tests := []struct {
		input string
		want  *string
	}{
		{"", nil},
		{"low", strPtr("low")},
		{"high", strPtr("high")},
		{"auto", strPtr("auto")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDetailLevel(tt.input)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("parseDetailLevel(%q) nil mismatch", tt.input)
				return
			}
			if got != nil && tt.want != nil && *got != *tt.want {
				t.Errorf("parseDetailLevel(%q) = %q, want %q", tt.input, *got, *tt.want)
			}
		})
	}
}
