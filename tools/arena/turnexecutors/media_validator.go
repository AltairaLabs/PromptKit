package turnexecutors

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// DefaultMaxFileSize is the default maximum file size (50MB)
	DefaultMaxFileSize = 50 * 1024 * 1024

	// MaxPathLength is the maximum allowed file path length
	MaxPathLength = 4096
)

// ValidateTurnMediaContent validates media content configuration for security and correctness
func ValidateTurnMediaContent(media *config.TurnMediaContent, contentType string) error {
	if media == nil {
		return fmt.Errorf("media content is nil")
	}

	// Check that at least one source is provided
	hasSource := media.URL != "" || media.FilePath != "" || media.Data != ""
	if !hasSource {
		return fmt.Errorf("media content has no source (URL, file_path, or data required)")
	}

	// Validate MIME type is present for inline data
	if media.Data != "" && media.MIMEType == "" {
		return fmt.Errorf("media content with inline data missing mime_type")
	}

	// Validate MIME type matches content type
	if media.MIMEType != "" {
		if err := ValidateMIMEType(media.MIMEType, contentType); err != nil {
			return err
		}
	}

	// Validate file path if provided
	if media.FilePath != "" {
		if err := ValidateFilePath(media.FilePath, ""); err != nil {
			return err
		}
	}

	return nil
}

// ValidateFilePath validates a file path for security issues
func ValidateFilePath(filePath, baseDir string) error {
	if err := validatePathFormat(filePath); err != nil {
		return err
	}

	cleanPath := resolveCleanPath(filePath, baseDir)

	if err := validatePathInBaseDir(cleanPath, baseDir, filePath); err != nil {
		return err
	}

	fileInfo, err := os.Lstat(cleanPath)
	if err != nil {
		return handleStatError(err, filePath)
	}

	return validateFileType(fileInfo, cleanPath, baseDir, filePath)
}

// validatePathFormat checks basic path format validity
func validatePathFormat(filePath string) error {
	if filePath == "" {
		return fmt.Errorf("file path is empty")
	}

	if len(filePath) > MaxPathLength {
		return fmt.Errorf("file path exceeds maximum length (%d): %s", MaxPathLength, filePath)
	}

	if strings.Contains(filePath, "..") {
		return fmt.Errorf("file path contains path traversal sequence (..): %s", filePath)
	}

	return nil
}

// resolveCleanPath resolves and cleans the file path
func resolveCleanPath(filePath, baseDir string) string {
	absPath := filePath
	if !filepath.IsAbs(filePath) && baseDir != "" {
		absPath = filepath.Join(baseDir, filePath)
	}
	return filepath.Clean(absPath)
}

// validatePathInBaseDir ensures the path is within the base directory
func validatePathInBaseDir(cleanPath, baseDir, filePath string) error {
	if baseDir == "" {
		return nil
	}

	cleanBaseDir := filepath.Clean(baseDir)
	if !strings.HasPrefix(cleanPath, cleanBaseDir) {
		return fmt.Errorf("file path escapes base directory: %s", filePath)
	}

	return nil
}

// handleStatError handles errors from os.Lstat
func handleStatError(err error, filePath string) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}
	return fmt.Errorf("failed to stat file %s: %w", filePath, err)
}

// validateFileType validates that the file is a regular file, handling symlinks
func validateFileType(fileInfo os.FileInfo, cleanPath, baseDir, filePath string) error {
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return validateSymlink(cleanPath, baseDir, filePath)
	}

	if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", filePath)
	}

	return nil
}

// validateSymlink validates a symlink and its target
func validateSymlink(cleanPath, baseDir, filePath string) error {
	targetPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlink %s: %w", filePath, err)
	}

	//nolint:gocritic // Using = to avoid shadowing err from outer scope
	if err = validateSymlinkTarget(targetPath, baseDir, filePath); err != nil {
		return err
	}

	targetInfo, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to stat symlink target %s: %w", filePath, err)
	}

	if !targetInfo.Mode().IsRegular() {
		return fmt.Errorf("symlink target is not a regular file: %s", filePath)
	}

	return nil
}

// validateSymlinkTarget ensures symlink target is within base directory
func validateSymlinkTarget(targetPath, baseDir, filePath string) error {
	if baseDir == "" {
		return nil
	}

	cleanBaseDir := filepath.Clean(baseDir)
	if !strings.HasPrefix(targetPath, cleanBaseDir) {
		return fmt.Errorf("symlink target escapes base directory: %s -> %s", filePath, targetPath)
	}

	return nil
}

// ValidateFileSize checks if a file size is within limits
func ValidateFileSize(filePath string, maxSize int64) error {
	if maxSize <= 0 {
		maxSize = DefaultMaxFileSize
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", filePath)
		}
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	if fileInfo.Size() > maxSize {
		return fmt.Errorf("file size (%d bytes) exceeds maximum (%d bytes): %s",
			fileInfo.Size(), maxSize, filePath)
	}

	return nil
}

// ValidateMIMEType validates that a MIME type matches the expected content type
func ValidateMIMEType(mimeType, contentType string) error {
	if mimeType == "" {
		return fmt.Errorf("MIME type is empty")
	}

	// Normalize content type
	contentType = strings.ToLower(contentType)

	// Extract base MIME type (ignore parameters like charset)
	baseMimeType := mimeType
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		baseMimeType = strings.TrimSpace(mimeType[:idx])
	}
	baseMimeType = strings.ToLower(baseMimeType)

	// Define valid MIME type prefixes for each content type
	validPrefixes := map[string][]string{
		"image": {
			"image/",
		},
		"audio": {
			"audio/",
		},
		"video": {
			"video/",
		},
		"text": {
			"text/",
		},
	}

	// Check if MIME type matches content type
	if prefixes, ok := validPrefixes[contentType]; ok {
		for _, prefix := range prefixes {
			if strings.HasPrefix(baseMimeType, prefix) {
				return nil
			}
		}
		return fmt.Errorf("MIME type '%s' does not match content type '%s'", mimeType, contentType)
	}

	// Unknown content type - allow any MIME type
	return nil
}

// IsSupportedImageMIMEType checks if a MIME type is a supported image format
func IsSupportedImageMIMEType(mimeType string) bool {
	supportedTypes := []string{
		types.MIMETypeImageJPEG,
		types.MIMETypeImagePNG,
		types.MIMETypeImageGIF,
		types.MIMETypeImageWebP,
	}

	for _, supported := range supportedTypes {
		if mimeType == supported {
			return true
		}
	}
	return false
}

// IsSupportedAudioMIMEType checks if a MIME type is a supported audio format
func IsSupportedAudioMIMEType(mimeType string) bool {
	supportedTypes := []string{
		types.MIMETypeAudioMP3,
		types.MIMETypeAudioWAV,
		types.MIMETypeAudioOgg,
		"audio/mp4",
		"audio/mpeg",
	}

	for _, supported := range supportedTypes {
		if mimeType == supported {
			return true
		}
	}
	return false
}

// IsSupportedVideoMIMEType checks if a MIME type is a supported video format
func IsSupportedVideoMIMEType(mimeType string) bool {
	supportedTypes := []string{
		types.MIMETypeVideoMP4,
		types.MIMETypeVideoWebM,
		"video/quicktime",
	}

	for _, supported := range supportedTypes {
		if mimeType == supported {
			return true
		}
	}
	return false
}
