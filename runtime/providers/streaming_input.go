package providers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StreamInputSession manages a bidirectional streaming session with a provider.
// The session allows sending media chunks (e.g., audio from a microphone) and
// receiving streaming responses from the LLM.
//
// Example usage:
//
//	session, err := provider.CreateStreamSession(ctx, StreamInputRequest{
//	    Config: types.StreamingMediaConfig{
//	        Type:       types.ContentTypeAudio,
//	        ChunkSize:  8192,
//	        SampleRate: 16000,
//	        Encoding:   "pcm",
//	        Channels:   1,
//	    },
//	})
//	if err != nil {
//	    return err
//	}
//	defer session.Close()
//
//	// Send audio chunks in a goroutine
//	go func() {
//	    for chunk := range micInput {
//	        if err := session.SendChunk(ctx, chunk); err != nil {
//	            log.Printf("send error: %v", err)
//	            break
//	        }
//	    }
//	}()
//
//	// Receive responses
//	for chunk := range session.Response() {
//	    if chunk.Error != nil {
//	        log.Printf("response error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Delta)
//	}
type StreamInputSession interface {
	// SendChunk sends a media chunk to the provider.
	// Returns an error if the chunk cannot be sent or the session is closed.
	// This method is safe to call from multiple goroutines.
	SendChunk(ctx context.Context, chunk *types.MediaChunk) error

	// SendText sends a text message to the provider during the streaming session.
	// This is useful for sending text prompts or instructions during audio streaming.
	// Note: This marks the turn as complete, triggering a response.
	SendText(ctx context.Context, text string) error

	// SendSystemContext sends a text message as context without completing the turn.
	// Use this for system prompts that provide context but shouldn't trigger an immediate response.
	// The audio/text that follows will be processed with this context in mind.
	SendSystemContext(ctx context.Context, text string) error

	// Response returns a receive-only channel for streaming responses.
	// The channel is closed when the session ends or encounters an error.
	// Consumers should read from this channel in a separate goroutine.
	Response() <-chan StreamChunk

	// Close ends the streaming session and releases resources.
	// After calling Close, SendChunk and SendText will return errors.
	// The Response channel will be closed.
	// Close is safe to call multiple times.
	Close() error

	// Error returns any error that occurred during the session.
	// Returns nil if no error has occurred.
	Error() error

	// Done returns a channel that's closed when the session ends.
	// This is useful for select statements to detect session completion.
	Done() <-chan struct{}
}

// ActivitySignaler is an optional interface for sessions that support
// explicit turn control via activity signals. When automatic VAD is disabled,
// callers must use SendActivityStart before sending audio and SendActivityEnd
// after sending all audio to signal turn boundaries.
//
// This is useful for pre-recorded audio (like TTS selfplay) where the caller
// knows exactly when the audio starts and ends, avoiding false turn detections
// from natural speech pauses.
type ActivitySignaler interface {
	// SendActivityStart signals the start of user audio input.
	// Must be called before sending audio chunks for a turn when VAD is disabled.
	SendActivityStart() error

	// SendActivityEnd signals the end of user audio input.
	// Call after sending all audio chunks for a turn to trigger model response.
	SendActivityEnd() error

	// IsVADDisabled returns true if automatic VAD is disabled for this session.
	// When true, callers must use SendActivityStart/SendActivityEnd for turn control.
	IsVADDisabled() bool
}

// StreamingInputConfig configures a new streaming input session.
type StreamingInputConfig struct {
	// Config specifies the media streaming configuration (codec, sample rate, etc.)
	Config types.StreamingMediaConfig `json:"config"`

	// SystemInstruction is the system prompt to configure the model's behavior.
	// For Gemini Live API, this is included in the setup message.
	SystemInstruction string `json:"system_instruction,omitempty"`

	// Metadata contains provider-specific session configuration
	// Example: {"response_modalities": ["TEXT", "AUDIO"]} for Gemini
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// StreamInputSupport extends the Provider interface for bidirectional streaming.
// Providers that implement this interface can handle streaming media input
// (e.g., real-time audio) and provide streaming responses.
type StreamInputSupport interface {
	Provider // Extends the base Provider interface

	// CreateStreamSession creates a new bidirectional streaming session.
	// The session remains active until Close() is called or an error occurs.
	// Returns an error if the provider doesn't support the requested media type.
	CreateStreamSession(ctx context.Context, req *StreamingInputConfig) (StreamInputSession, error)

	// SupportsStreamInput returns the media types supported for streaming input.
	// Common values: types.ContentTypeAudio, types.ContentTypeVideo
	SupportsStreamInput() []string

	// GetStreamingCapabilities returns detailed information about streaming support.
	// This includes supported codecs, sample rates, and other constraints.
	GetStreamingCapabilities() StreamingCapabilities
}

// StreamingCapabilities describes what streaming features a provider supports.
type StreamingCapabilities struct {
	// SupportedMediaTypes lists the media types that can be streamed
	// Values: types.ContentTypeAudio, types.ContentTypeVideo
	SupportedMediaTypes []string `json:"supported_media_types"`

	// Audio capabilities
	Audio *AudioStreamingCapabilities `json:"audio,omitempty"`

	// Video capabilities
	Video *VideoStreamingCapabilities `json:"video,omitempty"`

	// BidirectionalSupport indicates if the provider supports full bidirectional streaming
	BidirectionalSupport bool `json:"bidirectional_support"`

	// MaxSessionDuration is the maximum duration for a streaming session (in seconds)
	// Zero means no limit
	MaxSessionDuration int `json:"max_session_duration,omitempty"`

	// MinChunkSize is the minimum chunk size in bytes
	MinChunkSize int `json:"min_chunk_size,omitempty"`

	// MaxChunkSize is the maximum chunk size in bytes
	MaxChunkSize int `json:"max_chunk_size,omitempty"`
}

// AudioStreamingCapabilities describes audio streaming support.
type AudioStreamingCapabilities struct {
	// SupportedEncodings lists supported audio encodings
	// Common values: "pcm", "opus", "mp3", "aac"
	SupportedEncodings []string `json:"supported_encodings"`

	// SupportedSampleRates lists supported sample rates in Hz
	// Common values: 8000, 16000, 24000, 44100, 48000
	SupportedSampleRates []int `json:"supported_sample_rates"`

	// SupportedChannels lists supported channel counts
	// Common values: 1 (mono), 2 (stereo)
	SupportedChannels []int `json:"supported_channels"`

	// SupportedBitDepths lists supported bit depths
	// Common values: 16, 24, 32
	SupportedBitDepths []int `json:"supported_bit_depths,omitempty"`

	// PreferredEncoding is the recommended encoding for best quality/latency
	PreferredEncoding string `json:"preferred_encoding"`

	// PreferredSampleRate is the recommended sample rate
	PreferredSampleRate int `json:"preferred_sample_rate"`
}

// VideoStreamingCapabilities describes video streaming support.
type VideoStreamingCapabilities struct {
	// SupportedEncodings lists supported video encodings
	// Common values: "h264", "vp8", "vp9", "av1"
	SupportedEncodings []string `json:"supported_encodings"`

	// SupportedResolutions lists supported resolutions (width x height)
	SupportedResolutions []VideoResolution `json:"supported_resolutions"`

	// SupportedFrameRates lists supported frame rates
	// Common values: 15, 24, 30, 60
	SupportedFrameRates []int `json:"supported_frame_rates"`

	// PreferredEncoding is the recommended encoding
	PreferredEncoding string `json:"preferred_encoding"`

	// PreferredResolution is the recommended resolution
	PreferredResolution VideoResolution `json:"preferred_resolution"`

	// PreferredFrameRate is the recommended frame rate
	PreferredFrameRate int `json:"preferred_frame_rate"`
}

// VideoResolution represents a video resolution.
type VideoResolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// String returns a string representation of the resolution (e.g., "1920x1080")
func (r VideoResolution) String() string {
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

// Validate checks if the StreamInputRequest is valid
func (r *StreamingInputConfig) Validate() error {
	return r.Config.Validate()
}
