// Package providers implements multi-LLM provider support with unified interfaces.
//
// This package provides a common abstraction for predict-based LLM providers including
// OpenAI, Anthropic Claude, and Google Gemini. It handles:
//   - Predict completion requests with streaming support
//   - Tool/function calling with provider-specific formats
//   - Cost tracking and token usage calculation
//   - Rate limiting and error handling
//
// All providers implement the Provider interface for basic predict, and ToolSupport
// interface for function calling capabilities.
package providers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// PredictionRequest represents a request to a predict provider
type PredictionRequest struct {
	System      string                 `json:"system"`
	Messages    []types.Message        `json:"messages"`
	Temperature float32                `json:"temperature"`
	TopP        float32                `json:"top_p"`
	MaxTokens   int                    `json:"max_tokens"`
	Seed        *int                   `json:"seed,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"` // Optional metadata for provider-specific context
}

// PredictionResponse represents a response from a predict provider
type PredictionResponse struct {
	Content    string                  `json:"content"`
	Parts      []types.ContentPart     `json:"parts,omitempty"`     // Multimodal content parts (text, image, audio, video)
	CostInfo   *types.CostInfo         `json:"cost_info,omitempty"` // Cost breakdown for this response (includes token counts)
	Latency    time.Duration           `json:"latency"`
	Raw        []byte                  `json:"raw,omitempty"`
	RawRequest interface{}             `json:"raw_request,omitempty"` // Raw API request (for debugging)
	ToolCalls  []types.MessageToolCall `json:"tool_calls,omitempty"`  // Tools called in this response
}

// Pricing defines cost per 1K tokens for input and output
type Pricing struct {
	InputCostPer1K  float64
	OutputCostPer1K float64
}

// ProviderDefaults holds default parameters for providers
type ProviderDefaults struct {
	Temperature float32
	TopP        float32
	MaxTokens   int
	Pricing     Pricing
}

// Provider interface defines the contract for predict providers
type Provider interface {
	ID() string
	Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error)

	// Streaming support
	PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
	SupportsStreaming() bool

	ShouldIncludeRawOutput() bool
	Close() error // Close cleans up provider resources (e.g., HTTP connections)

	// CalculateCost calculates cost breakdown for given token counts
	CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo
}

// ToolDescriptor represents a tool that can be used by providers
type ToolDescriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

// ToolResult represents the result of a tool execution
// This is an alias to types.MessageToolResult for provider-specific context
type ToolResult = types.MessageToolResult

// ToolSupport interface for providers that support tool/function calling
type ToolSupport interface {
	Provider // Extends the base Provider interface

	// BuildTooling converts tool descriptors to provider-native format
	BuildTooling(descriptors []*ToolDescriptor) (interface{}, error)

	// PredictWithTools performs a predict request with tool support
	PredictWithTools(ctx context.Context, req PredictionRequest, tools interface{}, toolChoice string) (PredictionResponse, []types.MessageToolCall, error)
}
