package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const bytesPerKB = 1024 // Constant for byte to KB conversion

// MediaExternalizerConfig configures media externalization behavior.
type MediaExternalizerConfig struct {
	// Enabled controls whether media externalization is active
	Enabled bool

	// StorageService is the backend for storing media files
	StorageService storage.MediaStorageService

	// SizeThresholdKB is the minimum size (in KB) for externalizing media
	// Media smaller than this threshold stays inline as base64
	SizeThresholdKB int64

	// DefaultPolicy is the retention policy applied to externalized media
	DefaultPolicy string

	// RunID identifies the test run (for organization)
	RunID string

	// SessionID identifies the session (optional, for by-session organization)
	SessionID string

	// ConversationID identifies the conversation (optional, for by-conversation organization)
	ConversationID string
}

// mediaExternalizerMiddleware externalizes media content from provider responses.
type mediaExternalizerMiddleware struct {
	config MediaExternalizerConfig
}

// MediaExternalizerMiddleware creates middleware that externalizes media content to storage.
// It processes provider responses and moves large media from inline base64 to file storage,
// reducing memory usage and enabling better media management.
func MediaExternalizerMiddleware(config *MediaExternalizerConfig) pipeline.Middleware {
	return &mediaExternalizerMiddleware{
		config: *config,
	}
}

// Process externalizes media from the most recent response.
func (m *mediaExternalizerMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Continue to next middleware first (to get provider response)
	if err := next(); err != nil {
		return err
	}

	// Skip if disabled or no storage service configured
	if !m.config.Enabled || m.config.StorageService == nil {
		return nil
	}

	// Skip if there's an error or no response
	if execCtx.Error != nil || execCtx.Response == nil {
		return nil
	}

	// Externalize media in the response message
	if err := m.externalizeResponseMedia(execCtx); err != nil {
		return fmt.Errorf("failed to externalize media: %w", err)
	}

	return nil
}

// StreamChunk processes streaming chunks but doesn't externalize during streaming.
// Media externalization happens after streaming completes.
func (m *mediaExternalizerMiddleware) StreamChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	// Media externalization happens after streaming completes, not per-chunk
	return nil
}

// externalizeResponseMedia externalizes media from the provider response.
func (m *mediaExternalizerMiddleware) externalizeResponseMedia(execCtx *pipeline.ExecutionContext) error {
	if execCtx.Response == nil || len(execCtx.Response.Parts) == 0 {
		return nil
	}

	messageIdx := len(execCtx.Messages) // Index of this message in conversation

	// Process each content part
	for partIdx := range execCtx.Response.Parts {
		part := &execCtx.Response.Parts[partIdx]

		// Skip non-media parts
		if part.Media == nil {
			continue
		}

		// Externalize this media
		if err := m.externalizeMedia(execCtx.Context, part.Media, messageIdx, partIdx); err != nil {
			return fmt.Errorf("failed to externalize media at message %d, part %d: %w", messageIdx, partIdx, err)
		}
	}

	return nil
}

// externalizeMedia moves media content to external storage.
func (m *mediaExternalizerMiddleware) externalizeMedia(
	ctx context.Context,
	media *types.MediaContent,
	messageIdx, partIdx int,
) error {
	// Skip if already externalized
	if media.StorageReference != nil {
		return nil
	}

	// Skip if no inline data (already using FilePath or URL)
	if media.Data == nil || *media.Data == "" {
		return nil
	}

	// Check size threshold
	if m.config.SizeThresholdKB > 0 {
		// Estimate size from base64 data
		estimatedSizeKB := int64(len(*media.Data) * 3 / 4 / bytesPerKB) // base64 is ~4/3 original size
		if estimatedSizeKB < m.config.SizeThresholdKB {
			// Too small to externalize, keep inline
			return nil
		}
	}

	// Build metadata for storage
	metadata := &storage.MediaMetadata{
		RunID:          m.config.RunID,
		SessionID:      m.config.SessionID,
		ConversationID: m.config.ConversationID,
		MessageIdx:     messageIdx,
		PartIdx:        partIdx,
		MIMEType:       media.MIMEType,
		Timestamp:      time.Now(),
		PolicyName:     m.config.DefaultPolicy,
	}

	// Store media
	ref, err := m.config.StorageService.StoreMedia(ctx, media, metadata)
	if err != nil {
		return fmt.Errorf("failed to store media: %w", err)
	}

	// Update media content to reference storage
	refStr := string(ref)
	media.StorageReference = &refStr

	// Clear inline data to save memory
	media.Data = nil

	// Optionally set size if not already set
	if media.SizeKB == nil && metadata.SizeBytes > 0 {
		sizeKB := metadata.SizeBytes / bytesPerKB
		media.SizeKB = &sizeKB
	}

	return nil
}
