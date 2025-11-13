package turnexecutors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestValidateTurnMediaContent_Valid tests validation with valid media content
func TestValidateTurnMediaContent_Valid(t *testing.T) {
	tests := []struct {
		name        string
		media       *config.TurnMediaContent
		contentType string
	}{
		{
			name: "URL with MIME type",
			media: &config.TurnMediaContent{
				URL:      "https://example.com/image.jpg",
				MIMEType: "image/jpeg",
			},
			contentType: "image",
		},
		{
			name: "Data with MIME type",
			media: &config.TurnMediaContent{
				Data:     "base64data",
				MIMEType: "image/png",
			},
			contentType: "image",
		},
		{
			name: "URL without MIME type",
			media: &config.TurnMediaContent{
				URL: "https://example.com/audio.mp3",
			},
			contentType: "audio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTurnMediaContent(tt.media, tt.contentType)
			if err != nil {
				t.Errorf("ValidateTurnMediaContent() error = %v, want nil", err)
			}
		})
	}
}

// TestValidateTurnMediaContent_NilMedia tests validation with nil media
func TestValidateTurnMediaContent_NilMedia(t *testing.T) {
	err := ValidateTurnMediaContent(nil, "image")
	if err == nil {
		t.Error("Expected error for nil media, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("Expected error about nil, got: %v", err)
	}
}

// TestValidateTurnMediaContent_NoSource tests validation with no source
func TestValidateTurnMediaContent_NoSource(t *testing.T) {
	media := &config.TurnMediaContent{
		MIMEType: "image/jpeg",
	}

	err := ValidateTurnMediaContent(media, "image")
	if err == nil {
		t.Error("Expected error for media with no source, got nil")
	}
	if !strings.Contains(err.Error(), "no source") {
		t.Errorf("Expected error about no source, got: %v", err)
	}
}

// TestValidateTurnMediaContent_DataWithoutMIMEType tests validation of inline data without MIME type
func TestValidateTurnMediaContent_DataWithoutMIMEType(t *testing.T) {
	media := &config.TurnMediaContent{
		Data: "base64data",
	}

	err := ValidateTurnMediaContent(media, "image")
	if err == nil {
		t.Error("Expected error for data without MIME type, got nil")
	}
	if !strings.Contains(err.Error(), "mime_type") {
		t.Errorf("Expected error about mime_type, got: %v", err)
	}
}

// TestValidateTurnMediaContent_InvalidMIMEType tests validation with invalid MIME type
func TestValidateTurnMediaContent_InvalidMIMEType(t *testing.T) {
	media := &config.TurnMediaContent{
		Data:     "base64data",
		MIMEType: "video/mp4", // Wrong type for image content
	}

	err := ValidateTurnMediaContent(media, "image")
	if err == nil {
		t.Error("Expected error for mismatched MIME type, got nil")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("Expected error about MIME type mismatch, got: %v", err)
	}
}

// TestValidateFilePath_Valid tests validation with valid file paths
func TestValidateFilePath_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jpg")

	// Create test file
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name     string
		filePath string
		baseDir  string
	}{
		{
			name:     "Absolute path without base dir",
			filePath: testFile,
			baseDir:  "",
		},
		{
			name:     "Absolute path with base dir",
			filePath: testFile,
			baseDir:  tmpDir,
		},
		{
			name:     "Relative path with base dir",
			filePath: "test.jpg",
			baseDir:  tmpDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilePath(tt.filePath, tt.baseDir)
			if err != nil {
				t.Errorf("ValidateFilePath() error = %v, want nil", err)
			}
		})
	}
}

// TestValidateFilePath_EmptyPath tests validation with empty file path
func TestValidateFilePath_EmptyPath(t *testing.T) {
	err := ValidateFilePath("", "")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("Expected error about empty path, got: %v", err)
	}
}

// TestValidateFilePath_PathTooLong tests validation with overly long path
func TestValidateFilePath_PathTooLong(t *testing.T) {
	longPath := strings.Repeat("a", MaxPathLength+1)
	err := ValidateFilePath(longPath, "")
	if err == nil {
		t.Error("Expected error for path too long, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum length") {
		t.Errorf("Expected error about path length, got: %v", err)
	}
}

// TestValidateFilePath_PathTraversal tests validation with path traversal attempts
func TestValidateFilePath_PathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
	}{
		{
			name:     "Parent directory reference",
			filePath: "../etc/passwd",
		},
		{
			name:     "Multiple parent references",
			filePath: "../../secret.txt",
		},
		{
			name:     "Hidden parent reference",
			filePath: "images/../../../etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilePath(tt.filePath, "")
			if err == nil {
				t.Error("Expected error for path traversal, got nil")
			}
			if !strings.Contains(err.Error(), "path traversal") {
				t.Errorf("Expected error about path traversal, got: %v", err)
			}
		})
	}
}

// TestValidateFilePath_FileNotFound tests validation with non-existent file
func TestValidateFilePath_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.jpg")

	err := ValidateFilePath(nonExistentFile, tmpDir)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error about file not existing, got: %v", err)
	}
}

// TestValidateFilePath_Directory tests validation with directory instead of file
func TestValidateFilePath_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	err := ValidateFilePath(tmpDir, "")
	if err == nil {
		t.Error("Expected error for directory, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Expected error about not being a regular file, got: %v", err)
	}
}

// TestValidateFilePath_Symlink tests validation with symlink
func TestValidateFilePath_Symlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Resolve tmpDir to handle /private prefix on macOS
	resolvedTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("Failed to resolve tmpDir: %v", err)
	}

	targetFile := filepath.Join(resolvedTmpDir, "target.jpg")
	symlinkFile := filepath.Join(resolvedTmpDir, "symlink.jpg")

	// Create target file
	err = os.WriteFile(targetFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create symlink
	err = os.Symlink(targetFile, symlinkFile)
	if err != nil {
		t.Skipf("Skipping symlink test: %v", err)
	}

	// Validation should succeed for symlink within base dir
	err = ValidateFilePath(symlinkFile, resolvedTmpDir)
	if err != nil {
		t.Errorf("ValidateFilePath() error = %v, want nil for valid symlink", err)
	}
}

// TestValidateFilePath_SymlinkEscape tests validation with symlink escaping base dir
func TestValidateFilePath_SymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	targetFile := filepath.Join(tmpDir, "target.jpg")
	symlinkFile := filepath.Join(subDir, "escape.jpg")

	// Create target file outside subdir
	err = os.WriteFile(targetFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create symlink that points outside subdir
	err = os.Symlink(targetFile, symlinkFile)
	if err != nil {
		t.Skipf("Skipping symlink escape test: %v", err)
	}

	// Validation should fail for symlink escaping base dir
	err = ValidateFilePath(symlinkFile, subDir)
	if err == nil {
		t.Error("Expected error for symlink escaping base dir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("Expected error about escaping base directory, got: %v", err)
	}
}

// TestValidateFilePath_EscapeBaseDir tests validation with path escaping base directory
func TestValidateFilePath_EscapeBaseDir(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create file outside subdir
	outsideFile := filepath.Join(tmpDir, "outside.jpg")
	err = os.WriteFile(outsideFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	// Try to validate with subdir as base (file is outside)
	err = ValidateFilePath(outsideFile, subDir)
	if err == nil {
		t.Error("Expected error for path outside base dir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("Expected error about escaping base directory, got: %v", err)
	}
}

// TestValidateFileSize_Valid tests validation with valid file sizes
func TestValidateFileSize_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	smallFile := filepath.Join(tmpDir, "small.jpg")

	// Create small file (1KB)
	err := os.WriteFile(smallFile, make([]byte, 1024), 0644)
	if err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	// Validate with default max size
	err = ValidateFileSize(smallFile, 0)
	if err != nil {
		t.Errorf("ValidateFileSize() error = %v, want nil", err)
	}

	// Validate with custom max size
	err = ValidateFileSize(smallFile, 2048)
	if err != nil {
		t.Errorf("ValidateFileSize() error = %v, want nil", err)
	}
}

// TestValidateFileSize_TooLarge tests validation with oversized file
func TestValidateFileSize_TooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.jpg")

	// Create file (2KB)
	err := os.WriteFile(largeFile, make([]byte, 2048), 0644)
	if err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	// Validate with 1KB limit
	err = ValidateFileSize(largeFile, 1024)
	if err == nil {
		t.Error("Expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected error about exceeding maximum, got: %v", err)
	}
}

// TestValidateFileSize_FileNotFound tests validation with non-existent file
func TestValidateFileSize_FileNotFound(t *testing.T) {
	err := ValidateFileSize("/nonexistent/file.jpg", 0)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error about file not existing, got: %v", err)
	}
}

// TestValidateMIMEType_Valid tests validation with valid MIME types
func TestValidateMIMEType_Valid(t *testing.T) {
	tests := []struct {
		name        string
		mimeType    string
		contentType string
	}{
		{
			name:        "JPEG image",
			mimeType:    "image/jpeg",
			contentType: "image",
		},
		{
			name:        "PNG image",
			mimeType:    "image/png",
			contentType: "image",
		},
		{
			name:        "MP3 audio",
			mimeType:    "audio/mp3",
			contentType: "audio",
		},
		{
			name:        "MP4 video",
			mimeType:    "video/mp4",
			contentType: "video",
		},
		{
			name:        "MIME type with charset",
			mimeType:    "image/jpeg; charset=utf-8",
			contentType: "image",
		},
		{
			name:        "Unknown content type",
			mimeType:    "application/octet-stream",
			contentType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMIMEType(tt.mimeType, tt.contentType)
			if err != nil {
				t.Errorf("ValidateMIMEType() error = %v, want nil", err)
			}
		})
	}
}

// TestValidateMIMEType_EmptyMIMEType tests validation with empty MIME type
func TestValidateMIMEType_EmptyMIMEType(t *testing.T) {
	err := ValidateMIMEType("", "image")
	if err == nil {
		t.Error("Expected error for empty MIME type, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("Expected error about empty MIME type, got: %v", err)
	}
}

// TestValidateMIMEType_Mismatch tests validation with mismatched MIME type
func TestValidateMIMEType_Mismatch(t *testing.T) {
	tests := []struct {
		name        string
		mimeType    string
		contentType string
	}{
		{
			name:        "Video MIME for image content",
			mimeType:    "video/mp4",
			contentType: "image",
		},
		{
			name:        "Image MIME for audio content",
			mimeType:    "image/jpeg",
			contentType: "audio",
		},
		{
			name:        "Audio MIME for video content",
			mimeType:    "audio/mp3",
			contentType: "video",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMIMEType(tt.mimeType, tt.contentType)
			if err == nil {
				t.Errorf("Expected error for MIME type mismatch, got nil")
			}
			if !strings.Contains(err.Error(), "does not match") {
				t.Errorf("Expected error about MIME type mismatch, got: %v", err)
			}
		})
	}
}

// TestIsSupportedImageMIMEType tests supported image MIME type checking
func TestIsSupportedImageMIMEType(t *testing.T) {
	tests := []struct {
		name      string
		mimeType  string
		supported bool
	}{
		{name: "JPEG", mimeType: types.MIMETypeImageJPEG, supported: true},
		{name: "PNG", mimeType: types.MIMETypeImagePNG, supported: true},
		{name: "GIF", mimeType: types.MIMETypeImageGIF, supported: true},
		{name: "WebP", mimeType: types.MIMETypeImageWebP, supported: true},
		{name: "BMP", mimeType: "image/bmp", supported: false},
		{name: "TIFF", mimeType: "image/tiff", supported: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSupportedImageMIMEType(tt.mimeType)
			if result != tt.supported {
				t.Errorf("IsSupportedImageMIMEType(%s) = %v, want %v", tt.mimeType, result, tt.supported)
			}
		})
	}
}

// TestIsSupportedAudioMIMEType tests supported audio MIME type checking
func TestIsSupportedAudioMIMEType(t *testing.T) {
	tests := []struct {
		name      string
		mimeType  string
		supported bool
	}{
		{name: "MP3", mimeType: types.MIMETypeAudioMP3, supported: true},
		{name: "WAV", mimeType: types.MIMETypeAudioWAV, supported: true},
		{name: "OGG", mimeType: types.MIMETypeAudioOgg, supported: true},
		{name: "MPEG", mimeType: "audio/mpeg", supported: true},
		{name: "MP4", mimeType: "audio/mp4", supported: true},
		{name: "FLAC", mimeType: "audio/flac", supported: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSupportedAudioMIMEType(tt.mimeType)
			if result != tt.supported {
				t.Errorf("IsSupportedAudioMIMEType(%s) = %v, want %v", tt.mimeType, result, tt.supported)
			}
		})
	}
}

// TestIsSupportedVideoMIMEType tests supported video MIME type checking
func TestIsSupportedVideoMIMEType(t *testing.T) {
	tests := []struct {
		name      string
		mimeType  string
		supported bool
	}{
		{name: "MP4", mimeType: types.MIMETypeVideoMP4, supported: true},
		{name: "WebM", mimeType: types.MIMETypeVideoWebM, supported: true},
		{name: "QuickTime", mimeType: "video/quicktime", supported: true},
		{name: "AVI", mimeType: "video/avi", supported: false},
		{name: "MKV", mimeType: "video/x-matroska", supported: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSupportedVideoMIMEType(tt.mimeType)
			if result != tt.supported {
				t.Errorf("IsSupportedVideoMIMEType(%s) = %v, want %v", tt.mimeType, result, tt.supported)
			}
		})
	}
}
