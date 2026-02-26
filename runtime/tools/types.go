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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Mode         string          `json:"mode" yaml:"mode"`                   // "mock" | "live"
	TimeoutMs    int             `json:"timeout_ms" yaml:"timeout_ms"`

	// Static mock data (in-memory)
	MockResult json.RawMessage `json:"mock_result,omitempty" yaml:"mock_result,omitempty"`
	// Template for dynamic mocks (inline or file)
	MockTemplate     string `json:"mock_template,omitempty" yaml:"mock_template,omitempty"`
	MockResultFile   string `json:"mock_result_file,omitempty" yaml:"mock_result_file,omitempty"`
	MockTemplateFile string `json:"mock_template_file,omitempty" yaml:"mock_template_file,omitempty"`

	HTTPConfig *HTTPConfig `json:"http,omitempty" yaml:"http,omitempty"` // Live HTTP configuration
	A2AConfig  *A2AConfig  `json:"a2a,omitempty" yaml:"a2a,omitempty"`   // A2A agent configuration
}

// HTTPConfig defines configuration for live HTTP tool execution
type HTTPConfig struct {
	URL            string            `json:"url" yaml:"url"`
	Method         string            `json:"method" yaml:"method"`
	HeadersFromEnv []string          `json:"headers_from_env,omitempty" yaml:"headers_from_env,omitempty"`
	TimeoutMs      int               `json:"timeout_ms" yaml:"timeout_ms"`
	Redact         []string          `json:"redact,omitempty" yaml:"redact,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// A2AConfig defines configuration for A2A agent tool execution
type A2AConfig struct {
	AgentURL  string `json:"agent_url" yaml:"agent_url"`
	SkillID   string `json:"skill_id" yaml:"skill_id"`
	TimeoutMs int    `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
}

// ToolCall represents a tool invocation request
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	ID   string          `json:"id"` // Provider-specific call ID
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Name      string          `json:"name"`
	ID        string          `json:"id"` // Matches ToolCall.ID
	Result    json.RawMessage `json:"result"`
	LatencyMs int64           `json:"latency_ms"`
	Error     string          `json:"error,omitempty"`
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

// ToolGuidance provides hints for different interaction modes
// This is a flexible structure that can be extended with task-specific guidance
type ToolGuidance struct {
	Support   string `json:"support,omitempty"`
	Assistant string `json:"assistant,omitempty"`
	Generic   string `json:"generic,omitempty"`
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
	Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error)
	Name() string
}

// AsyncToolExecutor is a tool that can return pending status instead of blocking.
// Tools that require human approval or external async operations should implement this.
type AsyncToolExecutor interface {
	Executor // Still implements the basic Executor interface

	// ExecuteAsync may return immediately with a pending status
	ExecuteAsync(descriptor *ToolDescriptor, args json.RawMessage) (*ToolExecutionResult, error)
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
