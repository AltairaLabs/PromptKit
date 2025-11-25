package providers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StringPtr is a helper function that returns a pointer to a string.
// This is commonly used across provider implementations for optional fields.
func StringPtr(s string) *string {
	return &s
}

// LoadFileAsBase64 reads a file and returns its content as a base64-encoded string.
//
// Deprecated: Use MediaLoader.GetBase64Data instead for better functionality including
// storage reference support, URL loading, and proper context handling.
//
// This function is kept for backward compatibility but will be removed in a future version.
// It now delegates to the new MediaLoader implementation.
func LoadFileAsBase64(filePath string) (string, error) {
	// Delegate to the new media_loader.go implementation
	// This maintains backward compatibility while using the new infrastructure
	loader := NewMediaLoader(MediaLoaderConfig{})
	media := &types.MediaContent{
		FilePath: &filePath,
	}
	return loader.GetBase64Data(context.Background(), media)
}
