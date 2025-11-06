package turnexecutors

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ConvertTurnPartsToMessageParts converts scenario turn parts to runtime message parts,
// loading media files from disk as needed
func ConvertTurnPartsToMessageParts(turnParts []config.TurnContentPart, baseDir string) ([]types.ContentPart, error) {
	if len(turnParts) == 0 {
		return nil, nil
	}

	messageParts := make([]types.ContentPart, 0, len(turnParts))

	for i, turnPart := range turnParts {
		messagePart, err := convertSinglePart(turnPart, baseDir, i)
		if err != nil {
			return nil, err
		}
		messageParts = append(messageParts, messagePart)
	}

	return messageParts, nil
}

// convertSinglePart converts a single turn content part to a message content part
func convertSinglePart(turnPart config.TurnContentPart, baseDir string, index int) (types.ContentPart, error) {
	switch turnPart.Type {
	case "text":
		return convertTextPart(turnPart, index)
	case "image":
		return convertImagePart(turnPart, baseDir, index)
	case "audio":
		return convertAudioPart(turnPart, baseDir, index)
	case "video":
		return convertVideoPart(turnPart, baseDir, index)
	default:
		return types.ContentPart{}, fmt.Errorf("unsupported content part type at index %d: %s", index, turnPart.Type)
	}
}

// convertTextPart converts a text content part
func convertTextPart(turnPart config.TurnContentPart, index int) (types.ContentPart, error) {
	if turnPart.Text == "" {
		return types.ContentPart{}, fmt.Errorf("text part at index %d has empty text", index)
	}
	return types.NewTextPart(turnPart.Text), nil
}

// convertImagePart converts an image content part, loading from file if needed
func convertImagePart(turnPart config.TurnContentPart, baseDir string, index int) (types.ContentPart, error) {
	if turnPart.Media == nil {
		return types.ContentPart{}, fmt.Errorf("image part at index %d missing media content", index)
	}

	// Handle URL-based images
	if turnPart.Media.URL != "" {
		detail := parseDetailLevel(turnPart.Media.Detail)
		return types.NewImagePartFromURL(turnPart.Media.URL, detail), nil
	}

	// Handle inline base64 data
	if turnPart.Media.Data != "" {
		mimeType := turnPart.Media.MIMEType
		if mimeType == "" {
			mimeType = "image/jpeg" // Default
		}
		detail := parseDetailLevel(turnPart.Media.Detail)
		return types.NewImagePartFromData(turnPart.Media.Data, mimeType, detail), nil
	}

	// Handle file path - load from disk
	if turnPart.Media.FilePath != "" {
		return loadImageFromFile(turnPart.Media.FilePath, baseDir, turnPart.Media.Detail, index)
	}

	return types.ContentPart{}, fmt.Errorf("image part at index %d has no URL, data, or file_path", index)
}

// convertAudioPart converts an audio content part, loading from file if needed
func convertAudioPart(turnPart config.TurnContentPart, baseDir string, index int) (types.ContentPart, error) {
	if turnPart.Media == nil {
		return types.ContentPart{}, fmt.Errorf("audio part at index %d missing media content", index)
	}

	// Handle inline base64 data
	if turnPart.Media.Data != "" {
		mimeType := turnPart.Media.MIMEType
		if mimeType == "" {
			return types.ContentPart{}, fmt.Errorf("audio part at index %d with inline data missing mime_type", index)
		}
		return types.NewAudioPartFromData(turnPart.Media.Data, mimeType), nil
	}

	// Handle file path - load from disk
	if turnPart.Media.FilePath != "" {
		return loadAudioFromFile(turnPart.Media.FilePath, baseDir, index)
	}

	return types.ContentPart{}, fmt.Errorf("audio part at index %d has no data or file_path (URLs not supported for audio)", index)
}

// convertVideoPart converts a video content part, loading from file if needed
func convertVideoPart(turnPart config.TurnContentPart, baseDir string, index int) (types.ContentPart, error) {
	if turnPart.Media == nil {
		return types.ContentPart{}, fmt.Errorf("video part at index %d missing media content", index)
	}

	// Handle inline base64 data
	if turnPart.Media.Data != "" {
		mimeType := turnPart.Media.MIMEType
		if mimeType == "" {
			return types.ContentPart{}, fmt.Errorf("video part at index %d with inline data missing mime_type", index)
		}
		return types.NewVideoPartFromData(turnPart.Media.Data, mimeType), nil
	}

	// Handle file path - load from disk
	if turnPart.Media.FilePath != "" {
		return loadVideoFromFile(turnPart.Media.FilePath, baseDir, index)
	}

	return types.ContentPart{}, fmt.Errorf("video part at index %d has no data or file_path (URLs not supported for video)", index)
}

// loadImageFromFile loads an image from disk and returns a content part
func loadImageFromFile(filePath, baseDir, detail string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, index)
	if err != nil {
		return types.ContentPart{}, err
	}

	detailPtr := parseDetailLevel(detail)
	return types.NewImagePartFromData(data, mimeType, detailPtr), nil
}

// loadAudioFromFile loads audio from disk and returns a content part
func loadAudioFromFile(filePath, baseDir string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, index)
	if err != nil {
		return types.ContentPart{}, err
	}

	return types.NewAudioPartFromData(data, mimeType), nil
}

// loadVideoFromFile loads video from disk and returns a content part
func loadVideoFromFile(filePath, baseDir string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, index)
	if err != nil {
		return types.ContentPart{}, err
	}

	return types.NewVideoPartFromData(data, mimeType), nil
}

// loadMediaFile reads a media file and returns base64-encoded data and MIME type
func loadMediaFile(fullPath string, index int) (string, string, error) {
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("media file at index %d does not exist: %s", index, fullPath)
	}

	// Read file
	fileData, err := os.ReadFile(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read media file at index %d (%s): %w", index, fullPath, err)
	}

	// Detect MIME type from file extension
	mimeType := detectMIMEType(fullPath)

	// Base64 encode
	base64Data := base64.StdEncoding.EncodeToString(fileData)

	return base64Data, mimeType, nil
}

// resolveFilePath resolves a file path relative to the base directory
func resolveFilePath(filePath, baseDir string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(baseDir, filePath)
}

// detectMIMEType detects MIME type from file extension
func detectMIMEType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	mimeTypes := map[string]string{
		// Images
		".jpg":  types.MIMETypeImageJPEG,
		".jpeg": types.MIMETypeImageJPEG,
		".png":  types.MIMETypeImagePNG,
		".gif":  types.MIMETypeImageGIF,
		".webp": types.MIMETypeImageWebP,

		// Audio
		".mp3": types.MIMETypeAudioMP3,
		".wav": types.MIMETypeAudioWAV,
		".ogg": types.MIMETypeAudioOgg,
		".m4a": "audio/mp4", // Not defined in types package

		// Video
		".mp4":  types.MIMETypeVideoMP4,
		".webm": types.MIMETypeVideoWebM,
		".mov":  "video/quicktime", // Not defined in types package
	}

	if mimeType, ok := mimeTypes[ext]; ok {
		return mimeType
	}

	// Default fallback
	return "application/octet-stream"
}

// parseDetailLevel converts string detail level to pointer for images
func parseDetailLevel(detail string) *string {
	if detail == "" {
		return nil
	}
	return &detail
}
