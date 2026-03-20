// Package tools provides tool/function calling infrastructure for LLM testing.
//
// This package implements a flexible tool execution system with:
//   - Tool descriptor registry with JSON Schema validation
//   - Mock executors for testing (static and template-based)
//   - HTTP executor for live API calls
//   - Type coercion and result validation
//   - Adapter for prompt registry integration
//
// Tools can be loaded from YAML/JSON files and executed with argument validation,
// result schema checking, and automatic type coercion for common mismatches.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// NamespaceSep is the separator used in qualified tool names.
// Example: "a2a__weather_agent__get_forecast"
const NamespaceSep = "__"

// knownNamespaces lists the namespaces recognized as system/infrastructure tools.
var knownNamespaces = map[string]bool{
	"a2a":      true,
	"mcp":      true,
	"workflow": true,
	"memory":   true,
	"skill":    true,
}

// ParseToolName splits a qualified tool name on the first NamespaceSep.
// "a2a__weather__forecast" → ("a2a", "weather__forecast")
// "get_weather"            → ("", "get_weather")
// ""                       → ("", "")
func ParseToolName(name string) (namespace, localName string) {
	ns, local, found := strings.Cut(name, NamespaceSep)
	if !found {
		return "", name
	}
	return ns, local
}

// QualifyToolName joins a namespace and local name with NamespaceSep.
// ("mcp", "fs__read") → "mcp__fs__read"
// ("", "get_weather") → "get_weather"
func QualifyToolName(namespace, localName string) string {
	if namespace == "" {
		return localName
	}
	return namespace + NamespaceSep + localName
}

// IsSystemTool returns true if name belongs to a known system namespace.
func IsSystemTool(name string) bool {
	ns, _ := ParseToolName(name)
	return knownNamespaces[ns]
}

// ToolConfig represents a K8s-style tool configuration manifest
type ToolConfig struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       ToolDescriptor    `json:"spec" yaml:"spec"`
}

// ToolDescriptor represents a normalized tool definition
type ToolDescriptor struct {
	Name         string          `json:"name" yaml:"name"`
	Namespace    string          `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Description  string          `json:"description" yaml:"description"`
	InputSchema  json.RawMessage `json:"input_schema" yaml:"input_schema"`   // JSON Schema Draft-07
	OutputSchema json.RawMessage `json:"output_schema" yaml:"output_schema"` // JSON Schema Draft-07
	Mode         string          `json:"mode" yaml:"mode"`                   // mock|live|local|mcp|client
	TimeoutMs    int             `json:"timeout_ms" yaml:"timeout_ms"`

	// Static mock data (in-memory)
	MockResult json.RawMessage `json:"mock_result,omitempty" yaml:"mock_result,omitempty"`
	// Multimodal mock parts (text, image, audio, etc.)
	MockParts []types.ContentPart `json:"mock_parts,omitempty" yaml:"mock_parts,omitempty"`
	// Template for dynamic mocks (inline or file)
	MockTemplate     string `json:"mock_template,omitempty" yaml:"mock_template,omitempty"`
	MockResultFile   string `json:"mock_result_file,omitempty" yaml:"mock_result_file,omitempty"`
	MockTemplateFile string `json:"mock_template_file,omitempty" yaml:"mock_template_file,omitempty"`

	// Labels are optional key-value pairs propagated to events, metrics, and traces.
	// Use for dynamic dimensions like handler type, service tier, or team ownership.
	// These appear as additional Prometheus metric labels and OTel span attributes.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	HTTPConfig   *HTTPConfig   `json:"http,omitempty" yaml:"http,omitempty"`     // Live HTTP configuration
	A2AConfig    *A2AConfig    `json:"a2a,omitempty" yaml:"a2a,omitempty"`       // A2A agent configuration
	ClientConfig *ClientConfig `json:"client,omitempty" yaml:"client,omitempty"` // Client-side execution configuration
	ExecConfig   *ExecConfig   `json:"exec,omitempty" yaml:"exec,omitempty"`     // Exec subprocess configuration
}

// HTTPConfig defines configuration for live HTTP tool execution
type HTTPConfig struct {
	URL            string            `json:"url" yaml:"url"`
	Method         string            `json:"method" yaml:"method"`
	HeadersFromEnv []string          `json:"headers_from_env,omitempty" yaml:"headers_from_env,omitempty"`
	TimeoutMs      int               `json:"timeout_ms" yaml:"timeout_ms"`
	Redact         []string          `json:"redact,omitempty" yaml:"redact,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Request/response mapping configuration
	Request  *RequestMapping  `json:"request,omitempty" yaml:"request,omitempty"`
	Response *ResponseMapping `json:"response,omitempty" yaml:"response,omitempty"`

	// Multimodal response handling
	Multimodal *MultimodalConfig `json:"multimodal,omitempty" yaml:"multimodal,omitempty"`
}

// RequestMapping configures how LLM tool arguments are mapped to HTTP request components.
type RequestMapping struct {
	// QueryParams lists argument keys to route as URL query parameters.
	QueryParams []string `json:"query_params,omitempty" yaml:"query_params,omitempty"`

	// HeaderParams maps HTTP header names to Go text/template strings
	// that interpolate tool arguments. E.g. {"Authorization": "Bearer {{.token}}"}.
	HeaderParams map[string]string `json:"header_params,omitempty" yaml:"header_params,omitempty"`

	// BodyMapping is a JMESPath expression to reshape the body arguments
	// before sending. Only applies to POST/PUT/PATCH requests.
	BodyMapping string `json:"body_mapping,omitempty" yaml:"body_mapping,omitempty"`

	// Exclude lists argument keys to omit from the request entirely.
	// Keys consumed by URL path templates and header templates are
	// automatically excluded from the body.
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`

	// StaticQuery injects fixed query parameters into every request.
	// These are not part of the LLM's input schema — they control
	// API behavior (e.g. result count, language, format).
	StaticQuery map[string]string `json:"static_query,omitempty" yaml:"static_query,omitempty"`

	// StaticHeaders injects fixed headers into every request.
	// Unlike header_params, these are literal values, not templates.
	StaticHeaders map[string]string `json:"static_headers,omitempty" yaml:"static_headers,omitempty"`

	// StaticBody injects fixed fields into the JSON request body.
	// Only applies to POST/PUT/PATCH. Merged with LLM-provided body fields.
	StaticBody map[string]any `json:"static_body,omitempty" yaml:"static_body,omitempty"`
}

// ResponseMapping configures how HTTP response bodies are mapped to tool results.
type ResponseMapping struct {
	// BodyMapping is a JMESPath expression to extract or reshape the response JSON.
	BodyMapping string `json:"body_mapping,omitempty" yaml:"body_mapping,omitempty"`
}

// MultimodalConfig configures multimodal response handling for HTTP tools.
type MultimodalConfig struct {
	// Enabled activates Content-Type-based detection of binary responses.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// AcceptTypes lists the MIME types the tool may return (e.g. "image/png", "audio/wav").
	// If empty, common image/audio/video types are auto-detected.
	AcceptTypes []string `json:"accept_types,omitempty" yaml:"accept_types,omitempty"`
}

// A2AConfig defines configuration for A2A agent tool execution
type A2AConfig struct {
	AgentURL       string            `json:"agent_url" yaml:"agent_url"`
	SkillID        string            `json:"skill_id" yaml:"skill_id"`
	TimeoutMs      int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	HeadersFromEnv []string          `json:"headers_from_env,omitempty" yaml:"headers_from_env,omitempty"`
	Auth           *A2AAuthConfig    `json:"auth,omitempty" yaml:"auth,omitempty"`
	RetryPolicy    *A2ARetryConfig   `json:"retry,omitempty" yaml:"retry,omitempty"`
	SkillFilter    *A2ASkillFilter   `json:"skill_filter,omitempty" yaml:"skill_filter,omitempty"`
}

// A2AAuthConfig defines authentication for an A2A agent connection.
type A2AAuthConfig struct {
	Scheme string `json:"scheme" yaml:"scheme"` // e.g. "Bearer", "Basic"
	// Token is the static token value. Excluded from JSON serialization to prevent leaks.
	Token    string `json:"-" yaml:"token,omitempty"`
	TokenEnv string `json:"token_env,omitempty" yaml:"token_env,omitempty"` // Env var containing the token
}

// A2ARetryConfig defines per-agent retry policy overrides.
type A2ARetryConfig struct {
	MaxRetries     int `json:"max_retries,omitempty" yaml:"max_retries,omitempty"`
	InitialDelayMs int `json:"initial_delay_ms,omitempty" yaml:"initial_delay_ms,omitempty"`
	MaxDelayMs     int `json:"max_delay_ms,omitempty" yaml:"max_delay_ms,omitempty"`
}

// A2ASkillFilter controls which skills from an A2A agent are exposed to the LLM.
type A2ASkillFilter struct {
	Allowlist []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Blocklist []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

// IncludesSkill returns true if the given skill ID passes the filter.
func (f *A2ASkillFilter) IncludesSkill(skillID string) bool {
	if f == nil {
		return true
	}
	if len(f.Allowlist) > 0 {
		for _, a := range f.Allowlist {
			if a == skillID {
				return true
			}
		}
		return false
	}
	for _, b := range f.Blocklist {
		if b == skillID {
			return false
		}
	}
	return true
}

// ClientConfig defines configuration for client-side tool execution.
// Tools with mode "client" are fulfilled by the SDK caller's device
// (e.g., GPS, camera, contacts, biometrics).
// ExecConfig defines configuration for exec subprocess tool execution.
// The runtime spawns the command per invocation, passing tool arguments as JSON on stdin
// and reading the result from stdout.
type ExecConfig struct {
	Command   string   `json:"command" yaml:"command"`
	Runtime   string   `json:"runtime,omitempty" yaml:"runtime,omitempty"` // "exec" (default) or "server"
	Args      []string `json:"args,omitempty" yaml:"args,omitempty"`
	Env       []string `json:"env,omitempty" yaml:"env,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
}

type ClientConfig struct {
	Consent        *ConsentConfig `json:"consent,omitempty" yaml:"consent,omitempty"`
	TimeoutMs      int            `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Categories     []string       `json:"categories,omitempty" yaml:"categories,omitempty"`
	ValidateOutput bool           `json:"validate_output,omitempty" yaml:"validate_output,omitempty"`
}

// ConsentConfig defines consent requirements for client-side tools.
type ConsentConfig struct {
	Required bool   `json:"required" yaml:"required"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty"`
	// DeclineStrategy: "fallback" | "error" | "retry"
	DeclineStrategy string `json:"decline_strategy,omitempty" yaml:"decline_strategy,omitempty"`
}

// Decline strategy constants for ConsentConfig.DeclineStrategy.
const (
	DeclineStrategyFallback = "fallback"
	DeclineStrategyError    = "error"
	DeclineStrategyRetry    = "retry"
)

// ToolCall represents a tool invocation request
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	ID   string          `json:"id"` // Provider-specific call ID
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Name      string              `json:"name"`
	ID        string              `json:"id"` // Matches ToolCall.ID
	Result    json.RawMessage     `json:"result"`
	Parts     []types.ContentPart `json:"parts,omitempty"`
	LatencyMs int64               `json:"latency_ms"`
	Error     string              `json:"error,omitempty"`
}

// ToolExecutionStatus represents whether a tool completed or needs external input
type ToolExecutionStatus string

const (
	// ToolStatusComplete indicates the tool finished executing
	ToolStatusComplete ToolExecutionStatus = "complete"
	// ToolStatusPending indicates the tool is waiting for external input (e.g., human approval)
	ToolStatusPending ToolExecutionStatus = "pending"
	// ToolStatusFailed indicates the tool execution failed
	ToolStatusFailed ToolExecutionStatus = "failed"
)

// ToolExecutionResult includes status and optional pending information
type ToolExecutionResult struct {
	Status  ToolExecutionStatus `json:"status"`
	Content json.RawMessage     `json:"content,omitempty"`
	Parts   []types.ContentPart `json:"parts,omitempty"`
	Error   string              `json:"error,omitempty"`

	// Present when Status == ToolStatusPending
	PendingInfo *PendingToolInfo `json:"pending_info,omitempty"`
}

// PendingToolInfo provides context for middleware (email templates, notifications)
type PendingToolInfo struct {
	// Reason for pending (e.g., "requires_approval", "waiting_external_api")
	Reason string `json:"reason"`

	// Human-readable description
	Message string `json:"message"`

	// Tool details (for middleware to use in notifications)
	ToolName string          `json:"tool_name"`
	Args     json.RawMessage `json:"args"`

	// Optional: expiration, callback URL, etc.
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CallbackURL string     `json:"callback_url,omitempty"`

	// Arbitrary metadata for custom middleware
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToolPolicy defines constraints for tool usage in scenarios
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice"` // "auto" | "required" | "none"
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn"`
	MaxTotalToolCalls   int      `json:"max_total_tool_calls"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// ValidationError represents a tool validation failure
type ValidationError struct {
	Type   string `json:"type"` // "args_invalid" | "result_invalid" | "policy_violation"
	Tool   string `json:"tool"`
	Detail string `json:"detail"`
	Path   string `json:"path,omitempty"`
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	return fmt.Sprintf("tool %s validation error (%s): %s", e.Tool, e.Type, e.Detail)
}

// Executor interface defines how tools are executed
type Executor interface {
	Execute(ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error)
	Name() string
}

// MultimodalExecutor extends Executor with support for returning multimodal content parts.
// Executors that can return images, audio, or other non-text content should implement this.
type MultimodalExecutor interface {
	Executor

	// ExecuteMultimodal returns both the JSON result and optional content parts.
	ExecuteMultimodal(
		ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
	) (json.RawMessage, []types.ContentPart, error)
}

// AsyncToolExecutor is a tool that can return pending status instead of blocking.
// Tools that require human approval or external async operations should implement this.
type AsyncToolExecutor interface {
	Executor // Still implements the basic Executor interface

	// ExecuteAsync may return immediately with a pending status
	ExecuteAsync(ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage) (*ToolExecutionResult, error)
}

// PredictionRequest represents a predict request (extending existing type)
type PredictionRequest struct {
	System      string           `json:"system"`
	Messages    []PredictMessage `json:"messages"`
	Temperature float32          `json:"temperature"`
	TopP        float32          `json:"top_p"`
	MaxTokens   int              `json:"max_tokens"`
	Seed        *int             `json:"seed,omitempty"`
}

// PredictMessage represents a predict message (simplified version for tool context)
type PredictMessage struct {
	Role               string     `json:"role"`
	Content            string     `json:"content"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"`
	ToolCallResponseID string     `json:"tool_call_id,omitempty"` // For tool result messages
}

// PredictionResponse represents a predict response (extending existing type)
type PredictionResponse struct {
	Content   string        `json:"content"`
	TokensIn  int           `json:"tokens_in"`
	TokensOut int           `json:"tokens_out"`
	Latency   time.Duration `json:"latency"`
	Raw       []byte        `json:"raw,omitempty"`
	ToolCalls []ToolCall    `json:"tool_calls,omitempty"` // Tools called in this response
}
