package gemini

import "encoding/json"

// ServerMessage represents a message from the Gemini server (BidiGenerateContentServerMessage)
type ServerMessage struct {
	SetupComplete *SetupComplete `json:"setupComplete,omitempty"`
	ServerContent *ServerContent `json:"serverContent,omitempty"`
	ToolCall      *ToolCallMsg   `json:"toolCall,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// UsageMetadata contains token usage information
type UsageMetadata struct {
	PromptTokenCount   int `json:"promptTokenCount,omitempty"`
	ResponseTokenCount int `json:"responseTokenCount,omitempty"`
	TotalTokenCount    int `json:"totalTokenCount,omitempty"`
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
}

// InlineData represents inline media data
type InlineData struct {
	MimeType string `json:"mimeType,omitempty"` // camelCase!
	Data     string `json:"data,omitempty"`     // Base64 encoded
}

// UnmarshalJSON unmarshals ServerMessage from JSON with custom handling.
func (s *ServerMessage) UnmarshalJSON(data []byte) error {
	type Alias ServerMessage
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	return json.Unmarshal(data, aux)
}
