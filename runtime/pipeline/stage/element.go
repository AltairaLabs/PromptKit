// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

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
type VideoData struct {
	Data       []byte        // Raw video frame data or encoded video segment
	MIMEType   string        // MIME type (e.g., "video/mp4", "video/webm")
	Width      int           // Frame width in pixels
	Height     int           // Frame height in pixels
	FrameRate  float64       // Frames per second
	Duration   time.Duration // Duration of the video segment
	Timestamp  time.Time     // Timestamp of this frame
	Format     string        // Format identifier (e.g., "h264", "vp8")
	IsKeyFrame bool          // True if this is a key frame
}

// ImageData carries image data with metadata.
type ImageData struct {
	Data     []byte // Raw image data (encoded as JPEG, PNG, etc.)
	MIMEType string // MIME type (e.g., "image/jpeg", "image/png")
	Width    int    // Image width in pixels
	Height   int    // Image height in pixels
	Format   string // Format identifier (e.g., "jpeg", "png", "webp")
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
