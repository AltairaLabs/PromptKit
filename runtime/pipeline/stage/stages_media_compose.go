// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Default configuration values.
const (
	// DefaultCompletionTimeout is the default timeout for waiting for all message parts.
	DefaultCompletionTimeout = 30 * time.Second
)

// MediaComposeConfig configures the MediaComposeStage behavior.
type MediaComposeConfig struct {
	// CompletionTimeout is how long to wait for all parts of a message.
	// If timeout is reached, compose with available parts.
	// Default: 30s.
	CompletionTimeout time.Duration

	// StorageService for externalizing composed media (optional).
	StorageService storage.MediaStorageService
}

// DefaultMediaComposeConfig returns sensible defaults for media composition.
func DefaultMediaComposeConfig() MediaComposeConfig {
	return MediaComposeConfig{
		CompletionTimeout: DefaultCompletionTimeout,
	}
}

// pendingMessage tracks accumulated parts for a single message.
type pendingMessage struct {
	originalMessage *types.Message
	parts           map[int]*processedPart // part_index -> processed data
	totalParts      int
	receivedAt      time.Time
	sequence        int64
	source          string
}

// processedPart holds a processed media element awaiting composition.
type processedPart struct {
	index     int
	mediaType string // "image" or "video"
	image     *ImageData
	video     *VideoData
}

// MediaComposeStage collects processed media and composes back into messages.
// Elements are correlated by message ID from MediaExtractStage metadata.
//
// Input: StreamElements with Image or Video and extract metadata
// Output: StreamElement with Message containing composed Parts[]
//
// Non-media elements (those without extract metadata) are passed through unchanged.
//
// This is an Accumulate stage (N:1 fan-in pattern).
type MediaComposeStage struct {
	BaseStage
	config  MediaComposeConfig
	pending map[string]*pendingMessage // message_id -> pending
	mu      sync.Mutex
}

// NewMediaComposeStage creates a new media composition stage.
func NewMediaComposeStage(config MediaComposeConfig) *MediaComposeStage {
	return &MediaComposeStage{
		BaseStage: NewBaseStage("media-compose", StageTypeAccumulate),
		config:    config,
		pending:   make(map[string]*pendingMessage),
	}
}

// Process implements the Stage interface.
// Collects media elements and composes them back into messages.
func (s *MediaComposeStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	// Start timeout checker
	timeoutDone := make(chan struct{})
	go s.checkTimeouts(ctx, output, timeoutDone)

	for elem := range input {
		// Check if this is a media element with extract metadata
		msgID := elem.GetMetadata(MediaExtractMessageIDKey)
		if msgID == nil {
			// Pass through non-extracted elements
			select {
			case output <- elem:
			case <-ctx.Done():
				close(timeoutDone)
				return ctx.Err()
			}
			continue
		}

		// Accumulate the part
		complete, err := s.accumulatePart(&elem)
		if err != nil {
			logger.Error("Failed to accumulate media part", "error", err)
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				close(timeoutDone)
				return ctx.Err()
			}
			continue
		}

		// If all parts received, compose and emit
		if complete != nil {
			composed, err := s.composeMessage(complete)
			if err != nil {
				logger.Error("Failed to compose message", "error", err)
				// Emit error element
				errElem := NewErrorElement(err)
				errElem.Sequence = complete.sequence
				errElem.Source = complete.source
				select {
				case output <- errElem:
				case <-ctx.Done():
					close(timeoutDone)
					return ctx.Err()
				}
				continue
			}

			outElem := NewMessageElement(composed)
			outElem.Sequence = complete.sequence
			outElem.Source = complete.source

			select {
			case output <- outElem:
			case <-ctx.Done():
				close(timeoutDone)
				return ctx.Err()
			}
		}
	}

	// Process any remaining pending messages
	s.mu.Lock()
	remaining := make([]*pendingMessage, 0, len(s.pending))
	for _, pm := range s.pending {
		remaining = append(remaining, pm)
	}
	s.pending = make(map[string]*pendingMessage)
	s.mu.Unlock()

	for _, pm := range remaining {
		composed, err := s.composeMessage(pm)
		if err != nil {
			logger.Warn("Failed to compose incomplete message", "error", err)
			continue
		}

		outElem := NewMessageElement(composed)
		outElem.Sequence = pm.sequence
		outElem.Source = pm.source

		select {
		case output <- outElem:
		case <-ctx.Done():
			close(timeoutDone)
			return ctx.Err()
		}
	}

	close(timeoutDone)
	return nil
}

// accumulatePart adds a processed part to its pending message.
// Returns the complete pendingMessage if all parts have been received.
func (s *MediaComposeStage) accumulatePart(elem *StreamElement) (*pendingMessage, error) {
	msgID, ok := elem.GetMetadata(MediaExtractMessageIDKey).(string)
	if !ok {
		return nil, fmt.Errorf("invalid message ID type")
	}

	partIdx, ok := elem.GetMetadata(MediaExtractPartIndexKey).(int)
	if !ok {
		return nil, fmt.Errorf("invalid part index type")
	}

	totalParts, ok := elem.GetMetadata(MediaExtractTotalPartsKey).(int)
	if !ok {
		return nil, fmt.Errorf("invalid total parts type")
	}

	mediaType, _ := elem.GetMetadata(MediaExtractMediaTypeKey).(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get or create pending message
	pm, exists := s.pending[msgID]
	if !exists {
		origMsg, _ := elem.GetMetadata(MediaExtractOriginalMessageKey).(*types.Message)
		pm = &pendingMessage{
			originalMessage: origMsg,
			parts:           make(map[int]*processedPart),
			totalParts:      totalParts,
			receivedAt:      time.Now(),
			sequence:        elem.Sequence,
			source:          elem.Source,
		}
		s.pending[msgID] = pm
	}

	// Add the processed part
	part := &processedPart{
		index:     partIdx,
		mediaType: mediaType,
	}

	switch mediaType {
	case types.ContentTypeImage:
		part.image = elem.Image
	case types.ContentTypeVideo:
		part.video = elem.Video
	}

	pm.parts[partIdx] = part

	// Check if complete
	if len(pm.parts) >= pm.totalParts {
		delete(s.pending, msgID)
		return pm, nil
	}

	return nil, nil
}

// composeMessage builds a Message from accumulated parts.
func (s *MediaComposeStage) composeMessage(pm *pendingMessage) (*types.Message, error) {
	// Start with original message or create new one
	var msg *types.Message
	if pm.originalMessage != nil {
		msg = &types.Message{
			Role:      pm.originalMessage.Role,
			Timestamp: pm.originalMessage.Timestamp,
			Meta:      pm.originalMessage.Meta,
		}
	} else {
		msg = &types.Message{
			Role:      "user",
			Timestamp: time.Now(),
		}
	}

	// Rebuild parts with processed media
	var newParts []types.ContentPart

	// If we have original message, preserve non-media parts
	if pm.originalMessage != nil {
		mediaIdx := 0
		for _, origPart := range pm.originalMessage.Parts {
			switch origPart.Type {
			case types.ContentTypeText:
				// Keep text parts as-is
				newParts = append(newParts, origPart)
			case types.ContentTypeImage, types.ContentTypeVideo:
				// Replace with processed media if available
				if processed, ok := pm.parts[mediaIdx]; ok {
					newPart, err := s.createContentPartFromProcessed(processed)
					if err != nil {
						return nil, fmt.Errorf("failed to create content part for index %d: %w", mediaIdx, err)
					}
					newParts = append(newParts, newPart)
				}
				mediaIdx++
			}
		}
	} else {
		// No original message, just add processed parts in order
		for i := 0; i < pm.totalParts; i++ {
			if processed, ok := pm.parts[i]; ok {
				newPart, err := s.createContentPartFromProcessed(processed)
				if err != nil {
					return nil, fmt.Errorf("failed to create content part for index %d: %w", i, err)
				}
				newParts = append(newParts, newPart)
			}
		}
	}

	msg.Parts = newParts
	return msg, nil
}

// createContentPartFromProcessed creates a ContentPart from processed media.
func (s *MediaComposeStage) createContentPartFromProcessed(processed *processedPart) (types.ContentPart, error) {
	switch processed.mediaType {
	case types.ContentTypeImage:
		if processed.image == nil {
			return types.ContentPart{}, fmt.Errorf("image data is nil")
		}
		media := imageDataToMediaContent(processed.image)
		return types.ContentPart{
			Type:  types.ContentTypeImage,
			Media: media,
		}, nil

	case types.ContentTypeVideo:
		if processed.video == nil {
			return types.ContentPart{}, fmt.Errorf("video data is nil")
		}
		media := videoDataToMediaContent(processed.video)
		return types.ContentPart{
			Type:  types.ContentTypeVideo,
			Media: media,
		}, nil

	default:
		return types.ContentPart{}, fmt.Errorf("unsupported media type: %s", processed.mediaType)
	}
}

// imageDataToMediaContent converts ImageData back to MediaContent.
func imageDataToMediaContent(imageData *ImageData) *types.MediaContent {
	media := &types.MediaContent{
		MIMEType: imageData.MIMEType,
	}

	// Handle externalized data
	if imageData.IsExternalized() {
		ref := string(imageData.StorageRef)
		media.StorageReference = &ref
	} else if len(imageData.Data) > 0 {
		b64 := base64.StdEncoding.EncodeToString(imageData.Data)
		media.Data = &b64
	}

	// Set dimensions if available
	if imageData.Width > 0 {
		media.Width = &imageData.Width
	}
	if imageData.Height > 0 {
		media.Height = &imageData.Height
	}
	if imageData.Format != "" {
		media.Format = &imageData.Format
	}

	return media
}

// videoDataToMediaContent converts VideoData back to MediaContent.
func videoDataToMediaContent(videoData *VideoData) *types.MediaContent {
	media := &types.MediaContent{
		MIMEType: videoData.MIMEType,
	}

	// Handle externalized data
	if videoData.IsExternalized() {
		ref := string(videoData.StorageRef)
		media.StorageReference = &ref
	} else if len(videoData.Data) > 0 {
		b64 := base64.StdEncoding.EncodeToString(videoData.Data)
		media.Data = &b64
	}

	// Set dimensions if available
	if videoData.Width > 0 {
		media.Width = &videoData.Width
	}
	if videoData.Height > 0 {
		media.Height = &videoData.Height
	}
	if videoData.Format != "" {
		media.Format = &videoData.Format
	}
	if videoData.FrameRate > 0 {
		fps := int(videoData.FrameRate)
		media.FPS = &fps
	}
	if videoData.Duration > 0 {
		dur := int(videoData.Duration.Seconds())
		media.Duration = &dur
	}

	return media
}

// checkTimeouts periodically checks for timed-out pending messages.
func (s *MediaComposeStage) checkTimeouts(ctx context.Context, output chan<- StreamElement, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processTimeouts(ctx, output)
		}
	}
}

// processTimeouts handles messages that have exceeded the completion timeout.
func (s *MediaComposeStage) processTimeouts(ctx context.Context, output chan<- StreamElement) {
	s.mu.Lock()
	now := time.Now()
	var timedOut []*pendingMessage
	var timedOutIDs []string

	for id, pm := range s.pending {
		if now.Sub(pm.receivedAt) > s.config.CompletionTimeout {
			timedOut = append(timedOut, pm)
			timedOutIDs = append(timedOutIDs, id)
		}
	}

	for _, id := range timedOutIDs {
		delete(s.pending, id)
	}
	s.mu.Unlock()

	// Emit partial messages
	for _, pm := range timedOut {
		logger.Warn("Composing incomplete message due to timeout",
			"received_parts", len(pm.parts),
			"expected_parts", pm.totalParts,
		)

		composed, err := s.composeMessage(pm)
		if err != nil {
			logger.Error("Failed to compose timed-out message", "error", err)
			continue
		}

		outElem := NewMessageElement(composed)
		outElem.Sequence = pm.sequence
		outElem.Source = pm.source

		select {
		case output <- outElem:
		case <-ctx.Done():
			return
		}
	}
}

// GetConfig returns the stage configuration.
func (s *MediaComposeStage) GetConfig() MediaComposeConfig {
	return s.config
}
