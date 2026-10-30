// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// MediaExtractConfig configures the MediaExtractStage behavior.
type MediaExtractConfig struct {
	// ExtractImages enables image extraction.
	// Default: true.
	ExtractImages bool

	// ExtractVideos enables video extraction.
	// Default: true.
	ExtractVideos bool

	// PreserveStorageRefs when true, keeps storage references without loading data.
	// This enables lazy loading where data is only fetched when needed.
	// Default: true.
	PreserveStorageRefs bool

	// StorageService for loading externalized media (optional).
	// Only needed if PreserveStorageRefs is false and media has storage references.
	StorageService storage.MediaStorageService
}

// DefaultMediaExtractConfig returns sensible defaults for media extraction.
func DefaultMediaExtractConfig() MediaExtractConfig {
	return MediaExtractConfig{
		ExtractImages:       true,
		ExtractVideos:       true,
		PreserveStorageRefs: true,
	}
}

// MediaExtractStage extracts media from messages into individual StreamElements.
// This enables batch processing of images/videos through separate pipeline stages.
//
// Input: StreamElement with Message containing Parts[]
// Output: Multiple StreamElements with Image or Video, preserving correlation metadata
//
// For messages without media, the element is passed through unchanged.
//
// This is a Transform stage with fan-out behavior (1 message → N media elements).
type MediaExtractStage struct {
	BaseStage
	config    MediaExtractConfig
	messageID int64 // atomic counter for unique message IDs
}

// NewMediaExtractStage creates a new media extraction stage.
func NewMediaExtractStage(config MediaExtractConfig) *MediaExtractStage {
	return &MediaExtractStage{
		BaseStage: NewBaseStage("media-extract", StageTypeTransform),
		config:    config,
	}
}

// Process implements the Stage interface.
// Extracts media from messages and emits individual elements for each.
func (s *MediaExtractStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Only process messages with parts
		if elem.Message == nil || len(elem.Message.Parts) == 0 {
			// Pass through non-message elements or empty messages
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Extract media from message parts
		if err := s.extractMediaFromMessage(ctx, &elem, output); err != nil {
			// Set error on element and forward it
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// extractMediaFromMessage extracts media parts as separate elements.
func (s *MediaExtractStage) extractMediaFromMessage(
	ctx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	msg := elem.Message

	// Count media parts first
	mediaParts := s.selectMediaParts(msg)

	// If no media parts, pass through the original element
	if len(mediaParts) == 0 {
		select {
		case output <- *elem:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	// Generate unique message ID for correlation
	msgID := s.generateMessageID()

	// Extract each media part
	for partIdx, mp := range mediaParts {
		outElem, err := s.mediaPartElement(mp)
		if err != nil {
			return err
		}

		// Set correlation metadata
		outElem.Meta.MediaExtract = &MediaExtractInfo{
			MessageID:       msgID,
			PartIndex:       partIdx,
			TotalParts:      len(mediaParts),
			MediaType:       mp.mediaType,
			OriginalMessage: msg,
		}

		// Preserve provenance from the source element.
		outElem.Source = elem.Source
		outElem.Sequence = elem.Sequence

		select {
		case output <- outElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// extractedMediaPart is one media part selected for extraction.
type extractedMediaPart struct {
	index     int
	part      *types.ContentPart
	mediaType string
}

// selectMediaParts returns msg's media parts that the config extracts, in
// part order.
func (s *MediaExtractStage) selectMediaParts(msg *types.Message) []extractedMediaPart {
	var mediaParts []extractedMediaPart
	for i := range msg.Parts {
		part := &msg.Parts[i]
		if part.Media == nil {
			continue
		}

		switch part.Type {
		case types.ContentTypeImage:
			if s.config.ExtractImages {
				mediaParts = append(mediaParts, extractedMediaPart{i, part, types.ContentTypeImage})
			}
		case types.ContentTypeVideo:
			if s.config.ExtractVideos {
				mediaParts = append(mediaParts, extractedMediaPart{i, part, types.ContentTypeVideo})
			}
		}
	}
	return mediaParts
}

// mediaPartElement converts one selected media part into its stream element.
func (s *MediaExtractStage) mediaPartElement(mp extractedMediaPart) (StreamElement, error) {
	var outElem StreamElement

	switch mp.mediaType {
	case types.ContentTypeImage:
		imageData, err := mediaContentToImageData(mp.part.Media, s.config.PreserveStorageRefs)
		if err != nil {
			return outElem, fmt.Errorf("failed to convert image part %d: %w", mp.index, err)
		}
		outElem = NewImageElement(imageData)

	case types.ContentTypeVideo:
		videoData, err := mediaContentToVideoData(mp.part.Media, s.config.PreserveStorageRefs)
		if err != nil {
			return outElem, fmt.Errorf("failed to convert video part %d: %w", mp.index, err)
		}
		outElem = NewVideoElement(videoData)
	}
	return outElem, nil
}

// generateMessageID creates a unique identifier for message correlation.
func (s *MediaExtractStage) generateMessageID() string {
	id := atomic.AddInt64(&s.messageID, 1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), id)
}

// mediaContentToImageData converts MediaContent to ImageData.
// If preserveStorageRef is true, storage references are kept without loading data.
func mediaContentToImageData(media *types.MediaContent, preserveStorageRef bool) (*ImageData, error) {
	imageData := &ImageData{
		MIMEType: media.MIMEType,
	}

	// Copy optional dimensions if available
	if media.Width != nil {
		imageData.Width = *media.Width
	}
	if media.Height != nil {
		imageData.Height = *media.Height
	}
	if media.Format != nil {
		imageData.Format = *media.Format
	}

	// Handle storage reference (lazy loading)
	if media.StorageReference != nil && *media.StorageReference != "" {
		if preserveStorageRef {
			imageData.StorageRef = storage.Reference(*media.StorageReference)
			return imageData, nil
		}
		// If not preserving, caller needs to provide storage service to load
		return nil, fmt.Errorf("storage reference requires StorageService to load data")
	}

	data, err := readMediaBytes(media, mediaKindImage)
	if err != nil {
		return nil, err
	}
	imageData.Data = data
	return imageData, nil
}

// mediaContentToVideoData converts MediaContent to VideoData.
// If preserveStorageRef is true, storage references are kept without loading data.
func mediaContentToVideoData(media *types.MediaContent, preserveStorageRef bool) (*VideoData, error) {
	videoData := &VideoData{
		MIMEType:  media.MIMEType,
		Timestamp: time.Now(),
	}

	// Copy optional metadata if available
	if media.Width != nil {
		videoData.Width = *media.Width
	}
	if media.Height != nil {
		videoData.Height = *media.Height
	}
	if media.Format != nil {
		videoData.Format = *media.Format
	}
	if media.FPS != nil {
		videoData.FrameRate = float64(*media.FPS)
	}
	if media.Duration != nil {
		videoData.Duration = time.Duration(*media.Duration) * time.Second
	}

	// Handle storage reference (lazy loading)
	if media.StorageReference != nil && *media.StorageReference != "" {
		if preserveStorageRef {
			videoData.StorageRef = storage.Reference(*media.StorageReference)
			return videoData, nil
		}
		return nil, fmt.Errorf("storage reference requires StorageService to load data")
	}

	data, err := readMediaBytes(media, mediaKindVideo)
	if err != nil {
		return nil, err
	}
	videoData.Data = data
	return videoData, nil
}

// Media kinds named in readMediaBytes error messages.
const (
	mediaKindImage = "image"
	mediaKindVideo = "video"
)

// readMediaBytes loads media bytes from inline base64 data or a file path.
// URL-based media requires external fetching and is not supported in the
// extract stage. kind ("image"/"video") names the media in error messages.
func readMediaBytes(media *types.MediaContent, kind string) ([]byte, error) {
	// Handle inline base64 data
	if media.Data != nil && *media.Data != "" {
		data, err := base64.StdEncoding.DecodeString(*media.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 %s data: %w", kind, err)
		}
		return data, nil
	}

	// Handle file path
	if media.FilePath != nil && *media.FilePath != "" {
		return readMediaFile(media, kind)
	}

	// URL requires external fetching
	if media.URL != nil && *media.URL != "" {
		return nil, fmt.Errorf("URL-based media requires external loading; use MediaLoader first")
	}

	return nil, fmt.Errorf("no data source available for %s", kind)
}

// readMediaFile reads all bytes from media's file path.
func readMediaFile(media *types.MediaContent, kind string) ([]byte, error) {
	reader, err := media.ReadData()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s file: %w", kind, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s data: %w", kind, err)
	}
	return data, nil
}

// GetConfig returns the stage configuration.
func (s *MediaExtractStage) GetConfig() MediaExtractConfig {
	return s.config
}
