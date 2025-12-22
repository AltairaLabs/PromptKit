// Package openai provides OpenAI Realtime API streaming support.
package openai

import "encoding/json"

// Client Events - sent from client to server

// ClientEvent is the base structure for all client events.
type ClientEvent struct {
	EventID string `json:"event_id,omitempty"`
	Type    string `json:"type"`
}

// SessionUpdateEvent updates session configuration.
type SessionUpdateEvent struct {
	ClientEvent
	Session SessionConfig `json:"session"`
}

// SessionConfig is the session configuration sent in session.update.
// Note: TurnDetection uses a pointer without omitempty so we can explicitly
// send null to disable VAD. Omitting it causes OpenAI to use default (server_vad).
type SessionConfig struct {
	Modalities              []string             `json:"modalities,omitempty"`
	Instructions            string               `json:"instructions,omitempty"`
	Voice                   string               `json:"voice,omitempty"`
	InputAudioFormat        string               `json:"input_audio_format,omitempty"`
	OutputAudioFormat       string               `json:"output_audio_format,omitempty"`
	InputAudioTranscription *TranscriptionConfig `json:"input_audio_transcription,omitempty"`
	TurnDetection           *TurnDetectionConfig `json:"turn_detection"` // No omitempty - null disables VAD
	Tools                   []RealtimeToolDef    `json:"tools,omitempty"`
	ToolChoice              interface{}          `json:"tool_choice,omitempty"`
	Temperature             float64              `json:"temperature,omitempty"`
	MaxResponseOutputTokens interface{}          `json:"max_response_output_tokens,omitempty"`
}

// RealtimeToolDef is the tool definition format for session config.
type RealtimeToolDef struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// InputAudioBufferAppendEvent appends audio to the input buffer.
type InputAudioBufferAppendEvent struct {
	ClientEvent
	Audio string `json:"audio"` // Base64-encoded audio data
}

// InputAudioBufferCommitEvent commits the audio buffer for processing.
type InputAudioBufferCommitEvent struct {
	ClientEvent
}

// InputAudioBufferClearEvent clears the audio buffer.
type InputAudioBufferClearEvent struct {
	ClientEvent
}

// ConversationItemCreateEvent adds an item to the conversation.
type ConversationItemCreateEvent struct {
	ClientEvent
	PreviousItemID string           `json:"previous_item_id,omitempty"`
	Item           ConversationItem `json:"item"`
}

// ConversationItem represents an item in the conversation.
type ConversationItem struct {
	ID        string                `json:"id,omitempty"`
	Type      string                `json:"type"` // "message", "function_call", "function_call_output"
	Status    string                `json:"status,omitempty"`
	Role      string                `json:"role,omitempty"` // "user", "assistant", "system"
	Content   []ConversationContent `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`   // For function_call_output
	Output    string                `json:"output,omitempty"`    // For function_call_output
	Name      string                `json:"name,omitempty"`      // For function_call
	Arguments string                `json:"arguments,omitempty"` // For function_call
}

// ConversationContent represents content within a conversation item.
type ConversationContent struct {
	Type       string `json:"type"` // "input_text", "input_audio", "text", "audio"
	Text       string `json:"text,omitempty"`
	Audio      string `json:"audio,omitempty"`      // Base64-encoded
	Transcript string `json:"transcript,omitempty"` // For audio content
}

// ResponseCreateEvent triggers a response from the model.
type ResponseCreateEvent struct {
	ClientEvent
	Response *ResponseConfig `json:"response,omitempty"`
}

// ResponseConfig configures a response.
type ResponseConfig struct {
	Modalities        []string          `json:"modalities,omitempty"`
	Instructions      string            `json:"instructions,omitempty"`
	Voice             string            `json:"voice,omitempty"`
	OutputAudioFormat string            `json:"output_audio_format,omitempty"`
	Tools             []RealtimeToolDef `json:"tools,omitempty"`
	ToolChoice        interface{}       `json:"tool_choice,omitempty"`
	Temperature       float64           `json:"temperature,omitempty"`
	MaxOutputTokens   interface{}       `json:"max_output_tokens,omitempty"`
}

// ResponseCancelEvent cancels an in-progress response.
type ResponseCancelEvent struct {
	ClientEvent
}

// Server Events - received from server

// ServerEvent is the base structure for all server events.
type ServerEvent struct {
	EventID string `json:"event_id"`
	Type    string `json:"type"`
}

// ErrorEvent indicates an error occurred.
type ErrorEvent struct {
	ServerEvent
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information.
type ErrorDetail struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"`
	EventID string `json:"event_id,omitempty"`
}

// SessionCreatedEvent is sent when the session is established.
type SessionCreatedEvent struct {
	ServerEvent
	Session SessionInfo `json:"session"`
}

// SessionInfo contains session details.
type SessionInfo struct {
	ID                      string               `json:"id"`
	Object                  string               `json:"object"`
	Model                   string               `json:"model"`
	Modalities              []string             `json:"modalities"`
	Instructions            string               `json:"instructions"`
	Voice                   string               `json:"voice"`
	InputAudioFormat        string               `json:"input_audio_format"`
	OutputAudioFormat       string               `json:"output_audio_format"`
	InputAudioTranscription *TranscriptionConfig `json:"input_audio_transcription"`
	TurnDetection           *TurnDetectionConfig `json:"turn_detection"`
	Tools                   []RealtimeToolDef    `json:"tools"`
	Temperature             float64              `json:"temperature"`
	MaxResponseOutputTokens interface{}          `json:"max_response_output_tokens"`
}

// SessionUpdatedEvent confirms a session update.
type SessionUpdatedEvent struct {
	ServerEvent
	Session SessionInfo `json:"session"`
}

// InputAudioBufferCommittedEvent confirms audio buffer was committed.
type InputAudioBufferCommittedEvent struct {
	ServerEvent
	PreviousItemID string `json:"previous_item_id"`
	ItemID         string `json:"item_id"`
}

// InputAudioBufferClearedEvent confirms audio buffer was cleared.
type InputAudioBufferClearedEvent struct {
	ServerEvent
}

// InputAudioBufferSpeechStartedEvent indicates speech was detected.
type InputAudioBufferSpeechStartedEvent struct {
	ServerEvent
	AudioStartMs int    `json:"audio_start_ms"`
	ItemID       string `json:"item_id"`
}

// InputAudioBufferSpeechStoppedEvent indicates speech ended.
type InputAudioBufferSpeechStoppedEvent struct {
	ServerEvent
	AudioEndMs int    `json:"audio_end_ms"`
	ItemID     string `json:"item_id"`
}

// ConversationItemCreatedEvent confirms an item was added.
type ConversationItemCreatedEvent struct {
	ServerEvent
	PreviousItemID string           `json:"previous_item_id"`
	Item           ConversationItem `json:"item"`
}

// ConversationItemInputAudioTranscriptionCompletedEvent provides transcription.
type ConversationItemInputAudioTranscriptionCompletedEvent struct {
	ServerEvent
	ItemID       string `json:"item_id"`
	ContentIndex int    `json:"content_index"`
	Transcript   string `json:"transcript"`
}

// ConversationItemInputAudioTranscriptionFailedEvent indicates transcription failed.
type ConversationItemInputAudioTranscriptionFailedEvent struct {
	ServerEvent
	ItemID       string      `json:"item_id"`
	ContentIndex int         `json:"content_index"`
	Error        ErrorDetail `json:"error"`
}

// ResponseCreatedEvent indicates a response is starting.
type ResponseCreatedEvent struct {
	ServerEvent
	Response ResponseInfo `json:"response"`
}

// ResponseInfo contains response details.
type ResponseInfo struct {
	ID            string             `json:"id"`
	Object        string             `json:"object"`
	Status        string             `json:"status"`
	StatusDetails interface{}        `json:"status_details"`
	Output        []ConversationItem `json:"output"`
	Usage         *UsageInfo         `json:"usage"`
}

// UsageInfo contains token usage information.
type UsageInfo struct {
	TotalTokens       int `json:"total_tokens"`
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	InputTokenDetails struct {
		CachedTokens int `json:"cached_tokens"`
		TextTokens   int `json:"text_tokens"`
		AudioTokens  int `json:"audio_tokens"`
	} `json:"input_token_details"`
	OutputTokenDetails struct {
		TextTokens  int `json:"text_tokens"`
		AudioTokens int `json:"audio_tokens"`
	} `json:"output_token_details"`
}

// ResponseDoneEvent indicates a response completed.
type ResponseDoneEvent struct {
	ServerEvent
	Response ResponseInfo `json:"response"`
}

// ResponseOutputItemAddedEvent indicates an output item was added.
type ResponseOutputItemAddedEvent struct {
	ServerEvent
	ResponseID  string           `json:"response_id"`
	OutputIndex int              `json:"output_index"`
	Item        ConversationItem `json:"item"`
}

// ResponseOutputItemDoneEvent indicates an output item completed.
type ResponseOutputItemDoneEvent struct {
	ServerEvent
	ResponseID  string           `json:"response_id"`
	OutputIndex int              `json:"output_index"`
	Item        ConversationItem `json:"item"`
}

// ResponseContentPartAddedEvent indicates content was added.
type ResponseContentPartAddedEvent struct {
	ServerEvent
	ResponseID   string              `json:"response_id"`
	ItemID       string              `json:"item_id"`
	OutputIndex  int                 `json:"output_index"`
	ContentIndex int                 `json:"content_index"`
	Part         ConversationContent `json:"part"`
}

// ResponseContentPartDoneEvent indicates content part completed.
type ResponseContentPartDoneEvent struct {
	ServerEvent
	ResponseID   string              `json:"response_id"`
	ItemID       string              `json:"item_id"`
	OutputIndex  int                 `json:"output_index"`
	ContentIndex int                 `json:"content_index"`
	Part         ConversationContent `json:"part"`
}

// ResponseTextDeltaEvent provides streaming text.
type ResponseTextDeltaEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

// ResponseTextDoneEvent indicates text streaming completed.
type ResponseTextDoneEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

// ResponseAudioDeltaEvent provides streaming audio.
type ResponseAudioDeltaEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"` // Base64-encoded audio
}

// ResponseAudioDoneEvent indicates audio streaming completed.
type ResponseAudioDoneEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
}

// ResponseAudioTranscriptDeltaEvent provides streaming transcript.
type ResponseAudioTranscriptDeltaEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

// ResponseAudioTranscriptDoneEvent indicates transcript completed.
type ResponseAudioTranscriptDoneEvent struct {
	ServerEvent
	ResponseID   string `json:"response_id"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Transcript   string `json:"transcript"`
}

// ResponseFunctionCallArgumentsDeltaEvent provides streaming function args.
type ResponseFunctionCallArgumentsDeltaEvent struct {
	ServerEvent
	ResponseID  string `json:"response_id"`
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	CallID      string `json:"call_id"`
	Delta       string `json:"delta"`
}

// ResponseFunctionCallArgumentsDoneEvent indicates function args completed.
type ResponseFunctionCallArgumentsDoneEvent struct {
	ServerEvent
	ResponseID  string `json:"response_id"`
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	CallID      string `json:"call_id"`
	Name        string `json:"name"`
	Arguments   string `json:"arguments"`
}

// RateLimitsUpdatedEvent provides rate limit information.
type RateLimitsUpdatedEvent struct {
	ServerEvent
	RateLimits []RateLimit `json:"rate_limits"`
}

// RateLimit contains rate limit details.
type RateLimit struct {
	Name         string  `json:"name"`
	Limit        int     `json:"limit"`
	Remaining    int     `json:"remaining"`
	ResetSeconds float64 `json:"reset_seconds"`
}

// ParseServerEvent parses a raw JSON message into the appropriate event type.
func ParseServerEvent(data []byte) (interface{}, error) {
	// First, parse just the type
	var base ServerEvent
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}

	// Then parse into the specific type
	switch base.Type {
	case "error":
		var e ErrorEvent
		return &e, json.Unmarshal(data, &e)
	case "session.created":
		var e SessionCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case "session.updated":
		var e SessionUpdatedEvent
		return &e, json.Unmarshal(data, &e)
	case "input_audio_buffer.committed":
		var e InputAudioBufferCommittedEvent
		return &e, json.Unmarshal(data, &e)
	case "input_audio_buffer.cleared":
		var e InputAudioBufferClearedEvent
		return &e, json.Unmarshal(data, &e)
	case "input_audio_buffer.speech_started":
		var e InputAudioBufferSpeechStartedEvent
		return &e, json.Unmarshal(data, &e)
	case "input_audio_buffer.speech_stopped":
		var e InputAudioBufferSpeechStoppedEvent
		return &e, json.Unmarshal(data, &e)
	case "conversation.item.created":
		var e ConversationItemCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case "conversation.item.input_audio_transcription.completed":
		var e ConversationItemInputAudioTranscriptionCompletedEvent
		return &e, json.Unmarshal(data, &e)
	case "conversation.item.input_audio_transcription.failed":
		var e ConversationItemInputAudioTranscriptionFailedEvent
		return &e, json.Unmarshal(data, &e)
	case "response.created":
		var e ResponseCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case "response.done":
		var e ResponseDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.output_item.added":
		var e ResponseOutputItemAddedEvent
		return &e, json.Unmarshal(data, &e)
	case "response.output_item.done":
		var e ResponseOutputItemDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.content_part.added":
		var e ResponseContentPartAddedEvent
		return &e, json.Unmarshal(data, &e)
	case "response.content_part.done":
		var e ResponseContentPartDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.text.delta":
		var e ResponseTextDeltaEvent
		return &e, json.Unmarshal(data, &e)
	case "response.text.done":
		var e ResponseTextDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.audio.delta":
		var e ResponseAudioDeltaEvent
		return &e, json.Unmarshal(data, &e)
	case "response.audio.done":
		var e ResponseAudioDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.audio_transcript.delta":
		var e ResponseAudioTranscriptDeltaEvent
		return &e, json.Unmarshal(data, &e)
	case "response.audio_transcript.done":
		var e ResponseAudioTranscriptDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "response.function_call_arguments.delta":
		var e ResponseFunctionCallArgumentsDeltaEvent
		return &e, json.Unmarshal(data, &e)
	case "response.function_call_arguments.done":
		var e ResponseFunctionCallArgumentsDoneEvent
		return &e, json.Unmarshal(data, &e)
	case "rate_limits.updated":
		var e RateLimitsUpdatedEvent
		return &e, json.Unmarshal(data, &e)
	default:
		// Return the base event for unknown types
		return &base, nil
	}
}
