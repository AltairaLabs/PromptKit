package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// ServerMessage represents a message from the Gemini server (BidiGenerateContentServerMessage)
type ServerMessage struct {
	SetupComplete *SetupComplete `json:"setupComplete,omitempty"`
	ServerContent *ServerContent `json:"serverContent,omitempty"`
	ToolCall      *ToolCallMsg   `json:"toolCall,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// UsageMetadata contains token usage information. The *Details fields break
// the counts down by modality; audio is billed at several times the text rate,
// so they are what makes a voice session's cost right.
type UsageMetadata struct {
	PromptTokenCount           int                  `json:"promptTokenCount,omitempty"`
	CachedContentTokenCount    int                  `json:"cachedContentTokenCount,omitempty"`
	ResponseTokenCount         int                  `json:"responseTokenCount,omitempty"`
	ToolUsePromptTokenCount    int                  `json:"toolUsePromptTokenCount,omitempty"`
	ThoughtsTokenCount         int                  `json:"thoughtsTokenCount,omitempty"`
	TotalTokenCount            int                  `json:"totalTokenCount,omitempty"`
	PromptTokensDetails        []ModalityTokenCount `json:"promptTokensDetails,omitempty"`
	CacheTokensDetails         []ModalityTokenCount `json:"cacheTokensDetails,omitempty"`
	ResponseTokensDetails      []ModalityTokenCount `json:"responseTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []ModalityTokenCount `json:"toolUsePromptTokensDetails,omitempty"`
}

// tokens maps the Live API usage into canonical units; see usageCounts.tokens.
func (u *UsageMetadata) tokens() base.TokenUsage {
	return usageCounts{
		prompt:         u.PromptTokenCount,
		cached:         u.CachedContentTokenCount,
		toolUse:        u.ToolUsePromptTokenCount,
		response:       u.ResponseTokenCount,
		thoughts:       u.ThoughtsTokenCount,
		promptDetails:  u.PromptTokensDetails,
		cacheDetails:   u.CacheTokensDetails,
		toolUseDetails: u.ToolUsePromptTokensDetails,
		output:         u.ResponseTokensDetails,
	}.tokens()
}

// SetupComplete indicates setup is complete (empty object per docs)
type SetupComplete struct{}

// ToolCallMsg represents a tool call from the model
type ToolCallMsg struct {
	FunctionCalls []FunctionCall `json:"functionCalls,omitempty"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name string                 `json:"name,omitempty"`
	ID   string                 `json:"id,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// ServerContent represents the server content (BidiGenerateContentServerContent)
type ServerContent struct {
	ModelTurn           *ModelTurn     `json:"modelTurn,omitempty"`
	TurnComplete        bool           `json:"turnComplete,omitempty"`
	GenerationComplete  bool           `json:"generationComplete,omitempty"`
	Interrupted         bool           `json:"interrupted,omitempty"`
	InputTranscription  *Transcription `json:"inputTranscription,omitempty"`  // User speech transcription
	OutputTranscription *Transcription `json:"outputTranscription,omitempty"` // Model speech transcription
}

// Transcription represents audio transcription (BidiGenerateContentTranscription)
type Transcription struct {
	Text string `json:"text,omitempty"`
}

// ModelTurn represents a model response turn
type ModelTurn struct {
	Parts []Part `json:"parts,omitempty"`
}

// Part represents a content part (text or inline data)
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"` // camelCase!
	// Thought marks this text part as the model's reasoning (Gemini native-audio
	// thinking models). Reasoning is surfaced separately, not as spoken text.
	Thought bool `json:"thought,omitempty"`
}

// InlineData represents inline media data
type InlineData struct {
	MimeType string `json:"mimeType,omitempty"` // camelCase!
	Data     string `json:"data,omitempty"`     // Base64 encoded
}
