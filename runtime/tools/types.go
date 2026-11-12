// Package tools provides the tool-calling infrastructure: descriptors, a
// registry that executes them, and the built-in executors.
//
// The package contains:
//   - A tool descriptor registry with JSON Schema validation of arguments and results
//   - Mock executors (static and template-based)
//   - An HTTP executor for live API calls, with request and response mapping
//   - Exec and server executors that run tools as subprocesses
//   - An MCP executor that calls tools on MCP servers
//   - Argument coercion for common model mistakes, such as "10" for an integer
//
// Tools load from YAML or JSON files with Registry.LoadToolFromBytes, or are
// registered directly with Registry.Register.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// NamespaceSep is the separator used in qualified tool names.
// Example: "a2a__weather_agent__get_forecast"
const NamespaceSep = "__"

// knownNamespaces lists the namespaces recognized as system/infrastructure tools.
// The image/video namespaces back the mediagen image__generate / video__generate
// tools; their tokens match the corresponding ContentType values.
var knownNamespaces = map[string]bool{
	"a2a":                  true,
	modeMCP:                true,
	"workflow":             true,
	"memory":               true,
	"skill":                true,
	types.ContentTypeImage: true,
	types.ContentTypeVideo: true,
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

// MatchToolPattern reports whether a tool name matches an allowed_tools entry.
// An entry ending in "*" is a prefix match (the text before "*" is treated as a
// literal prefix), so "mcp__memory__*" matches every tool from the "memory" MCP
// server and "mcp__*" matches every MCP tool. Any other entry is an exact match.
func MatchToolPattern(pattern, name string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

// IsImplicitTool reports whether a system tool is auto-surfaced to every prompt
// without an allowed_tools entry. Capability tools (a2a, workflow, memory,
// skill, image/video mediagen) are implicitly available. MCP tools are
// system-namespaced but must be listed explicitly in allowed_tools (or matched
// by an mcp__<server>__* wildcard), so they are excluded here.
func IsImplicitTool(name string) bool {
	ns, _ := ParseToolName(name)
	return knownNamespaces[ns] && ns != modeMCP
}

// ToolConfig represents a K8s-style tool configuration manifest
type ToolConfig struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       ToolDescriptor    `json:"spec" yaml:"spec"`
}

// FunctionName is the name the LLM calls the tool by: spec.name when set,
// otherwise metadata.name. metadata.name is a resource name, where hyphens are
// conventional, so it cannot always be a function name.
func (c *ToolConfig) FunctionName() string {
	if c.Spec.Name != "" {
		return c.Spec.Name
	}
	return c.Metadata.Name
}

// ToolDescriptor represents a normalized tool definition
type ToolDescriptor struct {
	Name         string          `json:"name" yaml:"name"`
	Namespace    string          `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Description  string          `json:"description" yaml:"description"`
	InputSchema  json.RawMessage `json:"input_schema" yaml:"input_schema"`   // JSON Schema Draft-07
	OutputSchema json.RawMessage `json:"output_schema" yaml:"output_schema"` // JSON Schema Draft-07
	Mode         string          `json:"mode" yaml:"mode"`                   // mock|live|local|mcp|client|<executor name>
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

	// Declaration is the pack's definition of this tool, so a
	// host's executor can apply its own policy to action_scope and
	// extensions. It is a copy, never the loaded pack, and is read-only: it
	// may be shared by every conversation opened from one PackTemplate. Nil
	// means the pack does not declare the tool: a capability, MCP or
	// discovered tool, or a handler with no pack entry. How to treat an
	// undeclared tool is the host's decision. PromptKit never reads it.
	Declaration *packspec.Tool `json:"-" yaml:"-"`

	// Agent is the pack's definition of the agent member this tool calls,
	// set on A2A tools resolved from the pack's own agents (a read-only copy,
	// like Declaration). Nil for every other tool, including A2A tools discovered
	// from a remote agent card.
	Agent *packspec.AgentDef `json:"-" yaml:"-"`
}

// HTTPConfig defines configuration for live HTTP tool execution
type HTTPConfig struct {
	// URL is the endpoint. It may contain Go template references to tool
	// arguments in the path and query, but not in the scheme or host.
	URL string `json:"url" yaml:"url"`
	// Method is the HTTP method. It defaults to POST.
	Method string `json:"method" yaml:"method"`
	// HeadersFromEnv lists headers to set from environment variables, each
	// entry in the form "Header-Name=ENV_VAR_NAME". A variable that is unset or
	// empty sets no header.
	HeadersFromEnv []string `json:"headers_from_env,omitempty" yaml:"headers_from_env,omitempty"`
	// TimeoutMs bounds the request, in milliseconds. 0 sets no limit here.
	TimeoutMs int `json:"timeout_ms" yaml:"timeout_ms"`
	// Redact lists top-level keys of a JSON object response whose values are
	// replaced with "[REDACTED]" before the result is returned.
	Redact []string `json:"redact,omitempty" yaml:"redact,omitempty"`
	// Headers are literal headers set on every request.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

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

// ExecConfig defines configuration for exec subprocess tool execution.
// The runtime spawns the command per invocation, passing tool arguments as JSON on stdin
// and reading the result from stdout.
type ExecConfig struct {
	Command string   `json:"command" yaml:"command"`
	Runtime string   `json:"runtime,omitempty" yaml:"runtime,omitempty"` // "exec" (default) or "server"
	Args    []string `json:"args,omitempty" yaml:"args,omitempty"`
	// Env lists the names of the environment variables passed to the exec
	// subprocess. When it is empty the subprocess inherits the full environment.
	Env       []string `json:"env,omitempty" yaml:"env,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
}

// ClientConfig defines configuration for client-side tool execution.
// Tools with mode "client" are fulfilled by the SDK caller's device
// (e.g., GPS, camera, contacts, biometrics).
type ClientConfig struct {
	Consent    *ConsentConfig `json:"consent,omitempty" yaml:"consent,omitempty"`
	TimeoutMs  int            `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Categories []string       `json:"categories,omitempty" yaml:"categories,omitempty"`
	// ValidateOutput checks a result the caller supplies for a deferred call
	// (SendToolResult in the SDK) against the tool's OutputSchema. A result
	// that does not match reaches the model as a failed call. Results from a
	// registered handler are checked against the schema either way.
	ValidateOutput bool `json:"validate_output,omitempty" yaml:"validate_output,omitempty"`
}

// ConsentConfig defines consent requirements for client-side tools. These are
// declarative tool settings: asking for consent and acting on the answer is
// the host's job. Message is surfaced to the host (the SDK's
// ClientToolRequest.ConsentMsg and pending client tool).
type ConsentConfig struct {
	// Required declares that the tool needs the user's permission before it
	// runs. It is a host-declared setting: the runtime and SDK do not ask for
	// consent and do not block the call. The SDK passes it through on the
	// tool descriptor (ClientToolRequest.Descriptor and the conversation's
	// tool registry) for the host to act on.
	Required bool   `json:"required" yaml:"required"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty"`
	// DeclineStrategy tells the model how to proceed when the user declines:
	// "error", "fallback" or "retry" (the DeclineStrategy* constants). It is a
	// declarative setting whose handling is up to the host. When it is set,
	// the SDK's Conversation.RejectClientTool answers the call with
	// ConsentDeclinedMessage for the strategy, followed by the caller's
	// reason, so the model sees what PromptArena's consent simulation gives
	// it. When it is empty, the rejection text is unchanged.
	DeclineStrategy string `json:"decline_strategy,omitempty" yaml:"decline_strategy,omitempty"`
}

// Decline strategy constants for ConsentConfig.DeclineStrategy.
const (
	DeclineStrategyFallback = "fallback"
	DeclineStrategyError    = "error"
	DeclineStrategyRetry    = "retry"
)

// ConsentDeclinedMessage is the tool result text that tells the model the user
// declined consent for toolName, worded for the decline strategy: fallback
// asks for an alternative approach, retry for a different approach, and error
// (or any other value) reports the denial. PromptArena's consent simulation
// uses the same text.
func ConsentDeclinedMessage(toolName, strategy string) string {
	switch strategy {
	case DeclineStrategyFallback:
		return fmt.Sprintf("Tool %s unavailable: user declined consent. Use alternative approach.", toolName)
	case DeclineStrategyRetry:
		return fmt.Sprintf("Tool %s denied: user declined consent. Try a different approach.", toolName)
	default:
		return fmt.Sprintf("Tool %s denied: user declined consent", toolName)
	}
}

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

// ApprovalChecker decides whether a tool call must be held for external
// approval (human-in-the-loop) before execution. It returns a non-nil
// PendingToolInfo to hold the call pending, or nil to let it execute normally.
// It is consulted per tool call before execution; a nil checker is a no-op.
// The context is the tool-execution context, so a checker that persists the
// held call (e.g. to a durable store) honors cancellation and deadlines.
type ApprovalChecker func(ctx context.Context, callID, name string, args map[string]any) *PendingToolInfo

// ApprovalGate is an ApprovalChecker that can also refuse a call. It returns a
// non-nil PendingToolInfo to hold the call pending, nil to let it execute, or
// an error to refuse it: a refused call does not execute, and the model is
// given the error as the call's result. A gate that cannot record a hold (its
// store is unavailable, for example) returns an error rather than nil, so the
// call fails closed instead of running unapproved.
type ApprovalGate func(ctx context.Context, callID, name string, args map[string]any) (*PendingToolInfo, error)

// PendingToolInfo describes a tool call awaiting external input or approval.
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
