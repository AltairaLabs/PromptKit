// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// loadExternalizedData loads data from storage using the given reference.
// Returns nil if ref is empty. This is a helper to reduce duplication in Load methods.
func loadExternalizedData(
	ctx context.Context,
	store storage.MediaStorageService,
	ref storage.Reference,
	mediaType string,
) ([]byte, error) {
	content, err := store.RetrieveMedia(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to load externalized %s: %w", mediaType, err)
	}

	reader, err := content.ReadData()
	if err != nil {
		return nil, fmt.Errorf("failed to get %s reader: %w", mediaType, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s data: %w", mediaType, err)
	}

	return data, nil
}

// externalizeData stores data to external storage and returns the reference.
// This is a helper to reduce duplication in Externalize methods.
func externalizeData(
	ctx context.Context,
	store storage.MediaStorageService,
	data []byte,
	mimeType string,
	metadata *storage.MediaMetadata,
	mediaType string,
) (storage.Reference, error) {
	content := &types.MediaContent{
		MIMEType: mimeType,
	}
	dataStr := base64.StdEncoding.EncodeToString(data)
	content.Data = &dataStr

	ref, err := store.StoreMedia(ctx, content, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to externalize %s: %w", mediaType, err)
	}

	return ref, nil
}

// StreamElement is the unit of data flowing through the pipeline.
// It can carry different types of content and supports backpressure.
// Each element should contain at most one content type.
type StreamElement struct {
	// Content types (at most one should be set per element)
	Text      *string                // Text content
	Audio     *AudioData             // Audio samples
	Video     *VideoData             // Video frame
	Image     *ImageData             // Image data
	Message   *types.Message         // Complete message
	ToolCall  *types.MessageToolCall // Tool invocation
	Part      *types.ContentPart     // Generic content part (text, image, audio, video)
	MediaData *types.MediaContent    // Media content with MIME type

	// Metadata
	Sequence  int64                  // Monotonic sequence number
	Timestamp time.Time              // When element was created
	Source    string                 // Stage that produced this element
	Priority  Priority               // Scheduling priority (for QoS)
	Metadata  map[string]interface{} // Additional metadata for passing data between stages

	// Control signals
	EndOfStream bool  // No more elements after this
	Error       error // Error propagation
}

// Priority defines the scheduling priority for stream elements.
// Higher priority elements are processed before lower priority ones.
type Priority int

const (
	// PriorityLow is for non-critical data like logs or metrics
	PriorityLow Priority = iota
	// PriorityNormal is the default priority for most elements
	PriorityNormal
	// PriorityHigh is for real-time audio/video that requires low latency
	PriorityHigh
	// PriorityCritical is for control signals, errors, and system messages
	PriorityCritical
)

// AudioData carries audio samples with metadata.
type AudioData struct {
	Samples    []byte        // Raw audio samples
	SampleRate int           // Sample rate in Hz (e.g., 16000, 44100)
	Channels   int           // Number of audio channels (1=mono, 2=stereo)
	Format     AudioFormat   // Audio encoding format
	Duration   time.Duration // Duration of the audio segment
	Encoding   string        // Encoding scheme (e.g., "pcm", "opus")
}

// AudioFormat represents the encoding format of audio data.
type AudioFormat int

const (
	// AudioFormatPCM16 is 16-bit PCM encoding
	AudioFormatPCM16 AudioFormat = iota
	// AudioFormatFloat32 is 32-bit floating point encoding
	AudioFormatFloat32
	// AudioFormatOpus is Opus codec encoding
	AudioFormatOpus
	// AudioFormatMP3 is MP3 encoding
	AudioFormatMP3
	// AudioFormatAAC is AAC encoding
	AudioFormatAAC
)

// String returns the string representation of the audio format.
func (af AudioFormat) String() string {
	switch af {
	case AudioFormatPCM16:
		return "pcm16"
	case AudioFormatFloat32:
		return "float32"
	case AudioFormatOpus:
		return "opus"
	case AudioFormatMP3:
		return "mp3"
	case AudioFormatAAC:
		return "aac"
	default:
		return "unknown"
	}
}

// VideoData carries video frame data with metadata.
// Supports externalization to avoid holding large data in memory.
type VideoData struct {
	Data       []byte        // Raw video frame data or encoded video segment
	MIMEType   string        // MIME type (e.g., "video/mp4", "video/webm")
	Width      int           // Frame width in pixels
	Height     int           // Frame height in pixels
	FrameRate  float64       // Frames per second
	Duration   time.Duration // Duration of the video segment
	Timestamp  time.Time     // Timestamp of this frame/chunk
	Format     string        // Format identifier (e.g., "h264", "vp8")
	IsKeyFrame bool          // True if this is a key frame
	FrameNum   int64         // Frame/chunk sequence number (for ordering in streams)

	// StorageRef holds a reference to externalized data (when Data is nil).
	// Use IsExternalized() to check, Load() to retrieve data.
	StorageRef storage.Reference
}

// IsExternalized returns true if the video data has been externalized to storage.
func (d *VideoData) IsExternalized() bool {
	return d.StorageRef != "" && len(d.Data) == 0
}

// Load retrieves externalized video data from storage.
func (d *VideoData) Load(ctx context.Context, store storage.MediaStorageService) error {
	if !d.IsExternalized() {
		return nil
	}

	data, err := loadExternalizedData(ctx, store, d.StorageRef, "video")
	if err != nil {
		return err
	}

	d.Data = data
	return nil
}

// Externalize stores the video data to external storage and clears in-memory data.
func (d *VideoData) Externalize(
	ctx context.Context,
	store storage.MediaStorageService,
	metadata *storage.MediaMetadata,
) error {
	if len(d.Data) == 0 {
		return nil
	}

	ref, err := externalizeData(ctx, store, d.Data, d.MIMEType, metadata, "video")
	if err != nil {
		return err
	}

	d.StorageRef = ref
	d.Data = nil
	return nil
}

// EnsureLoaded ensures the video data is loaded into memory.
func (d *VideoData) EnsureLoaded(ctx context.Context, store storage.MediaStorageService) ([]byte, error) {
	if err := d.Load(ctx, store); err != nil {
		return nil, err
	}
	return d.Data, nil
}

// ImageData carries image data with metadata.
// Supports externalization to avoid holding large data in memory.
type ImageData struct {
	Data     []byte // Raw image data (encoded as JPEG, PNG, etc.)
	MIMEType string // MIME type (e.g., "image/jpeg", "image/png")
	Width    int    // Image width in pixels
	Height   int    // Image height in pixels
	Format   string // Format identifier (e.g., "jpeg", "png", "webp")

	// Streaming fields (for realtime video/image streaming)
	FrameNum  int64     // Frame sequence number (for ordering in streams)
	Timestamp time.Time // Frame capture timestamp (for synchronization)

	// StorageRef holds a reference to externalized data (when Data is nil).
	// Use IsExternalized() to check, Load() to retrieve data.
	StorageRef storage.Reference
}

// IsExternalized returns true if the image data has been externalized to storage.
// When externalized, Data is nil and StorageRef contains the storage reference.
func (d *ImageData) IsExternalized() bool {
	return d.StorageRef != "" && len(d.Data) == 0
}

// Load retrieves externalized image data from storage.
// Returns immediately if data is already in memory.
func (d *ImageData) Load(ctx context.Context, store storage.MediaStorageService) error {
	if !d.IsExternalized() {
		return nil // Already loaded or never externalized
	}

	data, err := loadExternalizedData(ctx, store, d.StorageRef, "image")
	if err != nil {
		return err
	}

	d.Data = data
	return nil
}

// Externalize stores the image data to external storage and clears in-memory data.
// The StorageRef is updated to point to the stored data.
func (d *ImageData) Externalize(
	ctx context.Context,
	store storage.MediaStorageService,
	metadata *storage.MediaMetadata,
) error {
	if len(d.Data) == 0 {
		return nil // Nothing to externalize
	}

	ref, err := externalizeData(ctx, store, d.Data, d.MIMEType, metadata, "image")
	if err != nil {
		return err
	}

	d.StorageRef = ref
	d.Data = nil
	return nil
}

// EnsureLoaded ensures the image data is loaded into memory.
// This is a convenience method that calls Load if externalized.
func (d *ImageData) EnsureLoaded(ctx context.Context, store storage.MediaStorageService) ([]byte, error) {
	if err := d.Load(ctx, store); err != nil {
		return nil, err
	}
	return d.Data, nil
}

// NewTextElement creates a new StreamElement with text content.
func NewTextElement(text string) StreamElement {
	return StreamElement{
		Text:      &text,
		Timestamp: time.Now(),
		Priority:  PriorityNormal,
		Metadata:  make(map[string]interface{}),
	}
}

// NewMessageElement creates a new StreamElement with a message.
func NewMessageElement(msg *types.Message) StreamElement {
	return StreamElement{
		Message:   msg,
		Timestamp: time.Now(),
		Priority:  PriorityNormal,
		Metadata:  make(map[string]interface{}),
	}
}

// NewAudioElement creates a new StreamElement with audio data.
func NewAudioElement(audio *AudioData) StreamElement {
	return StreamElement{
		Audio:     audio,
		Timestamp: time.Now(),
		Priority:  PriorityHigh, // Audio typically needs high priority
		Metadata:  make(map[string]interface{}),
	}
}

// NewVideoElement creates a new StreamElement with video data.
func NewVideoElement(video *VideoData) StreamElement {
	return StreamElement{
		Video:     video,
		Timestamp: time.Now(),
		Priority:  PriorityHigh, // Video typically needs high priority
		Metadata:  make(map[string]interface{}),
	}
}

// NewImageElement creates a new StreamElement with image data.
func NewImageElement(image *ImageData) StreamElement {
	return StreamElement{
		Image:     image,
		Timestamp: time.Now(),
		Priority:  PriorityNormal,
		Metadata:  make(map[string]interface{}),
	}
}

// NewErrorElement creates a new StreamElement with an error.
func NewErrorElement(err error) StreamElement {
	return StreamElement{
		Error:     err,
		Timestamp: time.Now(),
		Priority:  PriorityCritical, // Errors are always critical
		Metadata:  make(map[string]interface{}),
	}
}

// NewEndOfStreamElement creates a new StreamElement marking end of stream.
func NewEndOfStreamElement() StreamElement {
	return StreamElement{
		EndOfStream: true,
		Timestamp:   time.Now(),
		Priority:    PriorityCritical,
		Metadata:    make(map[string]interface{}),
	}
}

// IsEmpty returns true if the element contains no content.
func (e *StreamElement) IsEmpty() bool {
	return e.Text == nil &&
		e.Audio == nil &&
		e.Video == nil &&
		e.Image == nil &&
		e.Message == nil &&
		e.ToolCall == nil &&
		e.Part == nil &&
		e.MediaData == nil &&
		!e.EndOfStream &&
		e.Error == nil
}

// HasContent returns true if the element contains any content (excluding control signals).
func (e *StreamElement) HasContent() bool {
	return e.Text != nil ||
		e.Audio != nil ||
		e.Video != nil ||
		e.Image != nil ||
		e.Message != nil ||
		e.ToolCall != nil ||
		e.Part != nil ||
		e.MediaData != nil
}

// IsControl returns true if the element is a control signal (error or end-of-stream).
func (e *StreamElement) IsControl() bool {
	return e.Error != nil || e.EndOfStream
}

// WithSource sets the source stage name for this element.
func (e *StreamElement) WithSource(source string) *StreamElement {
	e.Source = source
	return e
}

// WithPriority sets the priority for this element.
func (e *StreamElement) WithPriority(priority Priority) *StreamElement {
	e.Priority = priority
	return e
}

// WithSequence sets the sequence number for this element.
func (e *StreamElement) WithSequence(seq int64) *StreamElement {
	e.Sequence = seq
	return e
}

// WithMetadata adds metadata to this element.
func (e *StreamElement) WithMetadata(key string, value interface{}) *StreamElement {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// GetMetadata retrieves metadata by key, returning nil if not found.
func (e *StreamElement) GetMetadata(key string) interface{} {
	if e.Metadata == nil {
		return nil
	}
	return e.Metadata[key]
}

// timeNow is a helper function that returns the current time.
// It's extracted for easier testing (can be mocked).
func timeNow() time.Time {
	return time.Now()
}
