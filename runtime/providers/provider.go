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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// systemRole is the message role used for system instructions.
const systemRole = "system"

// ResponseFormatType defines the type of response format
type ResponseFormatType string

const (
	// ResponseFormatText is the default text response format
	ResponseFormatText ResponseFormatType = "text"
	// ResponseFormatJSON requests JSON output from the model
	ResponseFormatJSON ResponseFormatType = "json_object"
	// ResponseFormatJSONSchema requests JSON output conforming to a schema
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat specifies the format of the model's response
type ResponseFormat struct {
	// Type specifies the response format type
	Type ResponseFormatType `json:"type"`
	// JSONSchema is the schema to use when Type is ResponseFormatJSONSchema
	// This should be a valid JSON Schema object
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	// SchemaName is an optional name for the schema (used by OpenAI)
	SchemaName string `json:"schema_name,omitempty"`
	// Strict enables strict schema validation (OpenAI-specific)
	Strict bool `json:"strict,omitempty"`
}

// PredictionRequest represents a request to a predict provider
type PredictionRequest struct {
	System      string          `json:"system"`
	Messages    []types.Message `json:"messages"`
	Temperature float32         `json:"temperature"`
	// TemperatureSet marks Temperature as set explicitly, so a zero is sent
	// as zero instead of being replaced by the provider's default temperature
	// (see ResolveTemperature).
	TemperatureSet bool    `json:"temperature_set,omitempty"`
	TopP           float32 `json:"top_p"`
	MaxTokens      int     `json:"max_tokens"`
	// FrequencyPenalty and PresencePenalty are sent by providers whose API
	// takes them; nil sends nothing.
	FrequencyPenalty *float32 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float32 `json:"presence_penalty,omitempty"`
	// TopK limits sampling to the K most likely tokens; nil sends nothing.
	// A provider whose API takes no top_k, or no penalties, drops them and
	// says so with WarnUnsentParams.
	TopK           *int            `json:"top_k,omitempty"`
	Seed           *int            `json:"seed,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"` // Optional response format (JSON mode)
	Metadata       map[string]any  `json:"metadata,omitempty"`        // Provider-specific context
}

// ResolveTemperature returns the temperature req sends: its own when it set
// one (explicitly, or any non-zero value), otherwise the provider's default
// def. A zero without TemperatureSet is "unset", which is what every caller
// that predates TemperatureSet means by it.
func ResolveTemperature(req *PredictionRequest, def float32) float32 {
	if req.TemperatureSet || req.Temperature != 0 {
		return req.Temperature
	}
	return def
}

// Wire names of the optional sampling parameters WarnUnsentParams reports.
const (
	ParamTopK             = "top_k"
	ParamFrequencyPenalty = "frequency_penalty"
	ParamPresencePenalty  = "presence_penalty"
)

// unsentParamOnce keys the provider|param pairs WarnUnsentParams has warned for.
var unsentParamOnce sync.Map

// WarnUnsentParams logs, once per provider and parameter, each of params
// (ParamTopK, ParamFrequencyPenalty, ParamPresencePenalty) that req sets but
// providerID does not send, because its API rejects or has no such parameter.
// A parameter req leaves unset logs nothing.
func WarnUnsentParams(providerID string, req *PredictionRequest, params ...string) {
	for _, param := range params {
		if !req.setsParam(param) {
			continue
		}
		if _, loaded := unsentParamOnce.LoadOrStore(providerID+"|"+param, struct{}{}); loaded {
			continue
		}
		logger.Warn("a prompt parameter is not sent: the provider's API does not take it",
			"provider", providerID, "param", param)
	}
}

// setsParam reports whether r sets the optional sampling parameter param.
func (r *PredictionRequest) setsParam(param string) bool {
	switch param {
	case ParamTopK:
		return r.TopK != nil
	case ParamFrequencyPenalty:
		return r.FrequencyPenalty != nil
	case ParamPresencePenalty:
		return r.PresencePenalty != nil
	}
	return false
}

// NormalizeMessages extracts system-role messages from Messages, merges their
// content into the System field, and removes them from Messages. This ensures
// all providers receive system context through the dedicated System field
// rather than as role entries that some providers silently drop.
//
// Ordering: existing System content first, then system-role message content
// in original order, separated by double newlines.
//
// This method is idempotent — calling it on an already-normalized request
// (no system-role messages in Messages) is a no-op.
func (r *PredictionRequest) NormalizeMessages() {
	var systemParts []string
	hasSystemMessages := false

	for i := range r.Messages {
		if r.Messages[i].Role == systemRole {
			hasSystemMessages = true
			break
		}
	}

	if !hasSystemMessages {
		return
	}

	if r.System != "" {
		systemParts = append(systemParts, r.System)
	}

	filtered := make([]types.Message, 0, len(r.Messages))
	for i := range r.Messages {
		if r.Messages[i].Role == systemRole {
			content := r.Messages[i].GetContent()
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		filtered = append(filtered, r.Messages[i])
	}

	r.System = strings.Join(systemParts, "\n\n")
	r.Messages = filtered
}

// PredictionResponse represents a response from a predict provider
type PredictionResponse struct {
	Content    string                  `json:"content"`
	Parts      []types.ContentPart     `json:"parts,omitempty"`     // Multimodal content parts (text, image, audio, video)
	CostInfo   *types.CostInfo         `json:"cost_info,omitempty"` // Cost breakdown for this response (includes token counts)
	Latency    time.Duration           `json:"latency"`
	Raw        []byte                  `json:"raw,omitempty"`
	RawRequest any                     `json:"raw_request,omitempty"` // Raw API request (for debugging)
	ToolCalls  []types.MessageToolCall `json:"tool_calls,omitempty"`  // Tools called in this response
	// Reasoning holds the model's reasoning/"thinking" for this response, carried
	// onto Message.Reasoning (off Parts) so it is excluded from content/exports/
	// future context by construction. Nil when the model reported none.
	Reasoning *types.ReasoningTrace `json:"reasoning,omitempty"`
	// FinishReason is the canonical (normalized) reason the model stopped,
	// or "" if the provider did not report one. See runtime/types FinishReason* constants.
	FinishReason string `json:"finish_reason,omitempty"`
}

// Pricing defines cost per 1K tokens for input and output
type Pricing struct {
	InputCostPer1K  float64
	OutputCostPer1K float64
}

// MaxTokensUnlimited is the ProviderDefaults.MaxTokens value that asks the
// provider to send no output-token limit, so the model's own maximum applies.
// Claude requires a limit on every request and rejects it at construction.
const MaxTokensUnlimited = -1

// ProviderDefaults holds default parameters for providers.
//
// Each applies only when the request leaves the field at zero; a positive
// request value always wins. A MaxTokens of zero or MaxTokensUnlimited sends no
// output-token limit, except on Claude, where zero falls back to 4096.
type ProviderDefaults struct {
	Temperature float32
	TopP        float32
	MaxTokens   int
	Pricing     Pricing
	// DisablePromptCaching, when true, disables Anthropic prompt caching.
	// Default (false) means caching is on for all models that support it.
	DisablePromptCaching bool
}

// ResolveMaxTokens returns the output-token limit for a request: the
// request's own positive value, else the provider default. A zero or negative
// request value counts as unset, so MaxTokensUnlimited only takes effect as a
// provider default. A zero result means send no limit, so the model's own
// maximum applies.
func ResolveMaxTokens(requested int, defaults ProviderDefaults) int {
	if requested > 0 {
		return requested
	}
	if defaults.MaxTokens > 0 {
		return defaults.MaxTokens
	}
	return 0
}

// Provider interface defines the contract for predict providers.
// It embeds base.Provider for cross-cutting concerns (identity, lifecycle,
// pricing) and adds inference-specific operations.
type Provider interface {
	base.Provider // adds Name, Type, Pricing, Validate, Init, HealthCheck, Close

	// ID returns the provider ID. Deprecated alias for Name(); kept for back-compat.
	ID() string

	// Model returns the model name/identifier used by this provider.
	Model() string

	Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error)

	// Streaming support
	PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)

	SupportsStreaming() bool

	ShouldIncludeRawOutput() bool

	// CalculateCost calculates cost breakdown for given token counts.
	CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo
}

// InferenceProvider is the unified name for predict-based LLM providers.
// Provider remains as a deprecated alias for back-compat with existing call sites.
type InferenceProvider = Provider

// AssertInferenceProvider type-asserts a base.Provider as an InferenceProvider.
// Returns an error if the provider is not an inference provider.
func AssertInferenceProvider(p base.Provider) (InferenceProvider, error) {
	inf, ok := p.(InferenceProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q (type=%s) is not an InferenceProvider", p.Name(), p.Type())
	}
	return inf, nil
}

// ContextWindowProvider is an optional interface for providers that can report
// their context window size. The compactor budget is auto-configured from it.
type ContextWindowProvider interface {
	MaxContextTokens() int
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

// ProviderTools represents provider-specific tool configuration.
// Each provider returns its own native format:
//   - OpenAI: []openAITool
//   - Claude: []claudeTool
//   - Gemini: geminiToolWrapper
//   - Ollama: []ollamaTool
//   - vLLM: []vllmTool
//   - Mock: []*ToolDescriptor
//
// The value returned by BuildTooling should be passed directly to PredictWithTools.
type ProviderTools = any

// ToolSupport interface for providers that support tool/function calling
type ToolSupport interface {
	Provider // Extends the base Provider interface

	// BuildTooling converts tool descriptors to provider-native format.
	// Returns a provider-specific type that should be passed to PredictWithTools.
	BuildTooling(descriptors []*ToolDescriptor) (ProviderTools, error)

	// PredictWithTools performs a predict request with tool support.
	// The tools parameter should be the value returned by BuildTooling.
	PredictWithTools(
		ctx context.Context,
		req PredictionRequest,
		tools ProviderTools,
		toolChoice string,
	) (PredictionResponse, []types.MessageToolCall, error)

	// PredictStreamWithTools performs a streaming predict request with tool support.
	// The tools parameter should be the value returned by BuildTooling.
	PredictStreamWithTools(
		ctx context.Context,
		req PredictionRequest,
		tools ProviderTools,
		toolChoice string,
	) (<-chan StreamChunk, error)
}
