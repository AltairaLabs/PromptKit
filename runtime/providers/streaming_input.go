package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// StreamInputSession manages a bidirectional streaming session with a provider.
// The session allows sending media chunks (e.g., audio from a microphone) and
// receiving streaming responses from the LLM.
//
// Example usage:
//
//	session, err := provider.CreateStreamSession(ctx, &StreamingInputConfig{
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

	// BargeIn returns a channel that fires OUT-OF-BAND when the provider detects
	// the user interrupting the agent mid-response (server-side barge-in),
	// independent of the Response() FIFO.
	//
	// Why a separate channel: Response() carries the (possibly large) buffered
	// audio of the in-flight response, and a consumer paced to real-time playback
	// drains it slowly. An interruption sent in-band would queue behind that
	// backlog and not be seen until playback caught up — defeating barge-in. This
	// channel lets the consumer learn immediately and flush pending audio.
	//
	// It is buffered and best-effort: rapid repeats may coalesce (the consumer
	// only needs to know the user interrupted, not how many times), and the
	// producer never closes it while the session is live, so consumers should
	// also select on Done(). Providers that cannot detect barge-in return a nil
	// channel (never fires); embed BargeInSignal to satisfy this for free.
	BargeIn() <-chan struct{}
}

// BargeInSignal is the shared implementation of StreamInputSession.BargeIn().
// Streaming sessions embed it so every provider exposes barge-in identically —
// the only per-provider code is the one line that calls SignalBargeIn() when
// that provider's wire protocol reports an interruption. The zero value is a
// safe no-op (BargeIn returns nil, SignalBargeIn does nothing); use
// NewBargeInSignal to enable firing.
type BargeInSignal struct {
	ch chan struct{}
}

// NewBargeInSignal returns a BargeInSignal whose channel is ready to fire.
func NewBargeInSignal() BargeInSignal {
	return BargeInSignal{ch: make(chan struct{}, 1)}
}

// BargeIn returns the out-of-band barge-in channel (nil for the zero value).
func (b BargeInSignal) BargeIn() <-chan struct{} { return b.ch }

// SignalBargeIn delivers one barge-in notification, best-effort and
// non-blocking: it coalesces if a prior signal is unconsumed, and no-ops on a
// zero-value (uninitialized) signal. Safe to call from the receive goroutine.
func (b BargeInSignal) SignalBargeIn() {
	if b.ch == nil {
		return
	}
	select {
	case b.ch <- struct{}{}:
	default:
	}
}

// StreamingInputConfig configures a new streaming input session.
type StreamingInputConfig struct {
	// Config specifies the media streaming configuration (codec, sample rate, etc.)
	Config types.StreamingMediaConfig `json:"config"`

	// SystemInstruction is the system prompt to configure the model's behavior.
	// For Gemini Live API, this is included in the setup message.
	SystemInstruction string `json:"system_instruction,omitempty"`

	// Tools defines functions the model can call during the session.
	// When configured, the model returns structured tool calls instead of
	// speaking them as text. Supported by Gemini Live API.
	Tools []StreamingToolDefinition `json:"tools,omitempty"`

	// ToolChoice is how the model may use Tools: "auto", "required", "none",
	// or the name of the one tool it must call. Empty leaves the provider's
	// default. OpenAI Realtime sends it as the session's tool_choice; a
	// provider whose realtime API has no such setting (Gemini Live) ignores
	// it.
	ToolChoice string `json:"tool_choice,omitempty"`

	// Metadata contains provider-specific session configuration
	// Example: {"response_modalities": ["TEXT", "AUDIO"]} for Gemini
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Sampling carries the opened prompt's sampling parameters into the
	// session. A provider sends those its realtime API takes and reports the
	// rest with WarnUnsentParams. Nil sends none.
	Sampling *StreamingSampling `json:"sampling,omitempty"`
}

// StreamingSampling is a prompt's sampling parameters for a realtime session,
// with PredictionRequest's meaning: zero MaxTokens and TopP, and nil pointers,
// are unset, and Temperature counts only with TemperatureSet or when non-zero.
type StreamingSampling struct {
	MaxTokens        int      `json:"max_tokens,omitempty"`
	Temperature      float32  `json:"temperature,omitempty"`
	TemperatureSet   bool     `json:"temperature_set,omitempty"`
	TopP             float32  `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	FrequencyPenalty *float32 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float32 `json:"presence_penalty,omitempty"`
}

// Request returns s as a PredictionRequest's sampling fields, for
// WarnUnsentParams. A nil s returns an empty request.
func (s *StreamingSampling) Request() *PredictionRequest {
	if s == nil {
		return &PredictionRequest{}
	}
	return &PredictionRequest{
		MaxTokens: s.MaxTokens, Temperature: s.Temperature, TemperatureSet: s.TemperatureSet,
		TopP: s.TopP, TopK: s.TopK, FrequencyPenalty: s.FrequencyPenalty, PresencePenalty: s.PresencePenalty,
	}
}

// StreamingToolDefinition represents a function/tool available in streaming sessions.
type StreamingToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"` // JSON Schema
}

// StreamToolUpdater is an optional interface for streaming sessions whose
// tools can change after the session is set up. UpdateTools replaces the
// session's tools with tools; an empty list leaves it none. A nil error means
// the update was sent, not that the provider applied it: a provider that
// rejects it reports so asynchronously, as an error chunk on the session's
// response stream.
//
// A session without it (Gemini Live fixes its tools at setup) keeps the
// tools it was created with: the pipeline refuses a call to a tool the
// conversation made available only after that.
type StreamToolUpdater interface {
	UpdateTools(ctx context.Context, tools []StreamingToolDefinition) error
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

// ResponseInterrupter is an optional StreamInputSession interface for a
// session that can stop a reply on the caller's behalf, the way a barge-in
// stops it. The duplex pipeline calls it when a guardrail blocks the reply.
//
// InterruptResponse stops the reply numbered generation (StreamChunk.Generation)
// and only that one: while the model is generating it, the response is
// canceled and its audio still to be delivered on Response() is dropped; the
// provider's own copy of the reply is cut to the audio the caller has heard,
// so the model's context holds no more of it than the caller heard. A reply
// not created yet is stopped as soon as it is, unless the caller starts a new
// turn first. A reply that has completed is only cut, and a later reply is
// never touched. Generation 0 (a caller that cannot tell) is the response in
// progress, or the next one. It sends no Interrupted chunk: the caller that
// interrupts reports the interruption itself. The response's end still
// arrives on Response() as the provider reports it (OpenAI Realtime: a
// canceled response.done).
//
// A session without it keeps generating, and the pipeline drops the rest of
// the reply. Gemini Live is one: it cannot stop a reply without adding a turn
// to the conversation.
type ResponseInterrupter interface {
	InterruptResponse(ctx context.Context, generation int64) error
}

// PlayoutHolder is an optional StreamInputSession interface for a session that
// estimates how much of a reply's audio the caller has heard from the audio it
// delivers: to tell a caller speaking over a reply that still plays (barge-in)
// from one starting a new turn, and to cut its own copy of an interrupted
// reply to what was heard.
//
// A pipeline that holds a reply's audio back until its guardrails have judged
// it calls HoldPlayout once, before the session delivers any reply. From then
// on the session counts a reply's audio only as the pipeline releases it to
// the caller (AudioReleased, given the chunk that carried it, as delivered),
// so the estimate runs from release, not arrival, and audio the pipeline drops
// is never counted as heard. Both are safe to call from any goroutine.
type PlayoutHolder interface {
	HoldPlayout()
	AudioReleased(chunk *StreamChunk)
}

// TranscriptLagger is an optional StreamInputSession interface for a session
// whose transcript of a reply's audio can arrive after that audio. A pipeline
// that releases a reply's audio as the guardrails pass its transcript would
// otherwise release audio whose words were not judged yet, so it holds back
// OutputTranscriptLag of the audio received most recently. A session that
// does not implement it delivers each piece of transcript no later than the
// last of its audio.
type TranscriptLagger interface {
	// OutputTranscriptLag is the most audio, in playing time, the session's
	// output transcript has been seen to trail by.
	OutputTranscriptLag() time.Duration
}

// TurnForgetter is an optional StreamInputSession interface for a session that
// can take a turn out of the model's conversation. The duplex pipeline calls it
// when an input guardrail blocks a spoken turn: the model heard the audio
// before its transcript existed, so cutting the reply leaves the caller's turn,
// and what was said back, in the model's context, and a later turn can revive
// what the guardrail blocked.
//
// ForgetTurn removes the caller's input named inputID, the ID the session put
// on that input's transcription chunks under InputItemIDKey, and the reply
// that answers that input, whichever reply is streaming when it is called. A
// reply still in progress, or not created yet, is removed once it ends; a
// completed one at once. Earlier rounds of a tool turn, and their tool
// results, stay.
// Removing an item the provider has already dropped is not an error.
//
// OpenAI Realtime deletes the conversation items. A session without it keeps
// the turn: Gemini Live has no way to remove one, so the model still remembers
// what the caller said.
type TurnForgetter interface {
	ForgetTurn(ctx context.Context, inputID string) error
}

// InputItemIDKey is the StreamChunk.Metadata key under which a session that
// implements TurnForgetter puts its ID for the caller's input on each of that
// input's transcription chunks.
const InputItemIDKey = "item_id"

// ContextNoter is an optional StreamInputSession interface for a session that
// can add a note to the model's conversation: an instruction the model reads
// with the turns that follow, that asks for no reply and that the caller
// neither hears nor sees. The duplex pipeline adds one when a guardrail blocks
// a turn, since the guardrail's message is spoken outside the model's
// conversation and the model would otherwise not know its reply, or the
// caller's input, was withheld.
//
// OpenAI Realtime adds a system message item (conversation.item.create, with
// no response.create). Gemini Live sends it as context that does not complete
// the turn (SendSystemContext).
type ContextNoter interface {
	AddContextNote(ctx context.Context, text string) error
}

// ReplyToInputKey is the StreamChunk.Metadata key under which such a session
// puts, on a reply's chunks, the ID (as under InputItemIDKey) of the caller's
// input the reply answers, when it knows it. The pipeline uses it to tell a
// transcript that arrives late for a reply that already ended from the
// transcript of the next turn, whose reply has not begun.
const ReplyToInputKey = "reply_to_input"

// OutputAudioReporter is an optional StreamInputSession interface reporting
// the session's spoken output, so audio the pipeline plays alongside the
// session's replies (a cue's clip) matches their voice and format. ok is false
// for a session whose replies are not spoken.
type OutputAudioReporter interface {
	OutputAudio() (audio OutputAudio, ok bool)
}

// OutputAudio describes a session's spoken output.
type OutputAudio struct {
	// Voice is the session's voice name, as the provider names it.
	Voice string
	// SampleRate is the rate of the session's PCM16 mono output audio.
	SampleRate int
	// Family is the provider family ("openai-realtime", "gemini-live"), for
	// telling apart audio that can sound different. It is never a model name.
	Family string
}

// LateInputTranscriber is an optional interface a StreamInputSupport provider
// implements to declare that it delivers the user's input transcription AFTER
// the assistant response has already begun — e.g. OpenAI Realtime, whose Whisper
// transcription arrives asynchronously, after the model has started replying.
//
// When a provider reports true (and input transcription is enabled), the
// streaming pipeline reorders the transcript so each turn's user text precedes
// its assistant text. Providers that emit the transcript before the reply (or
// not at all) need not implement this — reordering stays off by default.
type LateInputTranscriber interface {
	// EmitsLateInputTranscription reports whether the user transcript can arrive
	// after the assistant reply has started for the same turn.
	EmitsLateInputTranscription() bool
}

// LateUsageReporter is an optional interface a StreamInputSupport provider
// implements to declare that its sessions can report a call's usage after the
// call has ended: Gemini Live reports a tool-call turn's usage, and an
// interrupted reply's, on the turn completion that follows.
//
// When a provider reports true, the streaming pipeline holds a call's
// provider.call.completed that carries no usage for a short while, so the
// usage that follows lands on it rather than on a separate
// provider.call.usage event. Only that event waits. Providers that report a
// call's usage with the call (or not at all) need not implement this: their
// completions go out at once.
type LateUsageReporter interface {
	// ReportsUsageAfterCallEnds reports whether a call's usage can arrive
	// after the call has ended.
	ReportsUsageAfterCallEnds() bool
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

// Validate checks that the streaming media Config is valid.
func (r *StreamingInputConfig) Validate() error {
	return r.Config.Validate()
}

// ToolResponseSupport is an optional interface for streaming sessions that support tool calling.
// When the model returns a tool call, the caller can execute the tool and send the result
// back using this interface. The session will then continue generating a response based
// on the tool result.
//
// Use type assertion to check if a StreamInputSession supports this interface:
//
//	if toolSession, ok := session.(ToolResponseSupport); ok {
//	    err := toolSession.SendToolResponse(ctx, toolCallID, result)
//	}
type ToolResponseSupport interface {
	// SendToolResponse sends the result of a tool execution back to the model.
	// The toolCallID must match the ID from the MessageToolCall.
	// The result is typically JSON-encoded but the format depends on the tool.
	// After receiving the tool response, the model will continue generating.
	SendToolResponse(ctx context.Context, toolCallID string, result string) error

	// SendToolResponses sends multiple tool results at once (for parallel tool calls).
	// This is more efficient than sending individual responses for providers that
	// support batched tool responses.
	SendToolResponses(ctx context.Context, responses []ToolResponse) error
}

// ToolResponse represents a single tool execution result.
type ToolResponse struct {
	ToolCallID string `json:"tool_call_id"`
	// Result is the result's text: the joined text of its text parts. It is
	// what a session that cannot take Parts sends.
	Result  string `json:"result"`
	IsError bool   `json:"is_error,omitempty"` // True if the tool execution failed
	// Parts holds a multimodal result's content parts (text and media, in
	// order); nil for a text-only result. Only a session reporting
	// ToolResponsePartsSupport sends them.
	Parts []types.ContentPart `json:"parts,omitempty"`
}

// ToolResponsePartsSupport is an optional interface for streaming sessions
// that can send a tool result's media parts (ToolResponse.Parts) to the model.
// A session without it, or returning false, is sent the same responses and
// sends only their text Result.
type ToolResponsePartsSupport interface {
	SupportsToolResponseParts() bool
}
