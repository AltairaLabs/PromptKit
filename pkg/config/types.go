package config

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// ObjectMeta is a simplified metadata structure for PromptKit configs
// Based on K8s ObjectMeta but with YAML-friendly tags and optional fields
type ObjectMeta struct {
	Name        string            `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"title=Name,description=Name of the resource"`                           //nolint:lll
	Namespace   string            `yaml:"namespace,omitempty" json:"namespace,omitempty" jsonschema:"title=Namespace,description=Namespace for the resource"`      //nolint:lll
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty" jsonschema:"title=Labels,description=Key-value pairs for organizing resources"` //nolint:lll
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty" jsonschema:"title=Annotations,description=Additional metadata"`       //nolint:lll
}

// MCPToolFilter controls which tools from an MCP server are exposed to the LLM.
type MCPToolFilter struct {
	Allowlist []string `yaml:"allowlist,omitempty" json:"allowlist,omitempty"`
	Blocklist []string `yaml:"blocklist,omitempty" json:"blocklist,omitempty"`
}

// MCPServerConfig represents configuration for an MCP server.
//
// Exactly one transport must be specified:
//   - Command: stdio (PromptKit spawns a local subprocess).
//   - URL:     HTTP transport — by default the legacy SSE adapter is
//     used. Set Transport to "streamable_http" to opt into the modern
//     Streamable HTTP transport (MCP 2025-03-26).
//   - Source:  host-provisioned (a named MCPSource opens the endpoint at
//     a scope boundary). Requires Scope.
type MCPServerConfig struct {
	Name       string            `yaml:"name" json:"name"`
	Command    string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args       []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env        map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	URL        string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	//nolint:lll // jsonschema tags require single line
	Transport string `yaml:"transport,omitempty" json:"transport,omitempty" jsonschema:"title=MCP Transport,description=Explicit transport adapter. Defaults: url→sse for back-compat; command→stdio. Set to 'streamable_http' to opt into the MCP 2025-03-26 Streamable HTTP transport.,enum=,enum=stdio,enum=sse,enum=streamable_http"`
	//nolint:lll // jsonschema tags require single line
	Source string `yaml:"source,omitempty" json:"source,omitempty" jsonschema:"title=MCPSource Name,description=Name of a host-registered MCPSource that provisions the endpoint per scope (e.g. 'docker'). Mutually exclusive with command and url."`
	//nolint:lll // jsonschema tags require single line
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty" jsonschema:"title=Source Scope,description=Lifecycle boundary at which the source opens and closes. Required when source is set.,enum=run,enum=scenario,enum=session"`
	//nolint:lll // jsonschema tags require single line
	SourceArgs map[string]any `yaml:"source_args,omitempty" json:"source_args,omitempty" jsonschema:"title=Source Args,description=Opaque arguments passed to the named MCPSource. Schema is source-specific. See the source's reference for accepted fields."`
	TimeoutMs  int            `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	ToolFilter *MCPToolFilter `yaml:"tool_filter,omitempty" json:"tool_filter,omitempty"`
}

// StateStoreConfig represents configuration for conversation state storage
type StateStoreConfig struct {
	// Type specifies the state store implementation: "memory", "redis", or "file"
	Type string `yaml:"type" json:"type"`

	// Redis configuration (only used when Type is "redis")
	Redis *RedisConfig `yaml:"redis,omitempty" json:"redis,omitempty"`

	// File configuration (only used when Type is "file")
	File *FileStateStoreConfig `yaml:"file,omitempty" json:"file,omitempty"`
}

// FileStateStoreConfig configures the filesystem-backed statestore.
// Required when StateStoreConfig.Type == "file".
type FileStateStoreConfig struct {
	// Root is the directory under which per-conversation directories live.
	// Required. Created if absent.
	Root string `yaml:"root" json:"root"`

	// FSync controls fsync behavior: "off", "on-save" (default), "on-append".
	//nolint:lll // jsonschema tags require single line
	FSync string `yaml:"fsync,omitempty" json:"fsync,omitempty" jsonschema:"title=FSync Policy,enum=,enum=off,enum=on-save,enum=on-append"`

	// TTLDays, if non-zero, removes conversation directories whose state.json
	// mtime is older than now-TTLDays days at startup.
	TTLDays int `yaml:"ttl_days,omitempty" json:"ttl_days,omitempty"`
}

// RedisConfig contains Redis-specific configuration
type RedisConfig struct {
	// Address of the Redis server (e.g., "localhost:6379")
	Address string `yaml:"address" json:"address"`

	// Password for Redis authentication (optional)
	Password string `yaml:"password,omitempty" json:"password,omitempty"`

	// Database number (0-15, default is 0)
	Database int `yaml:"database,omitempty" json:"database,omitempty"`

	// TTL for conversation state (e.g., "24h", "7d"). Default is "24h"
	TTL string `yaml:"ttl,omitempty" json:"ttl,omitempty"`

	// Prefix for Redis keys (default is "promptkit")
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
}

// ToolData holds raw tool configuration data
type ToolData struct {
	FilePath string `json:"file_path,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

// InferenceDefaults pins which inference provider id serves each
// runtime/classify task by default. Ids resolve into
// cfg.LoadedInferenceProviders (populated from `providers:` entries
// declaring `role: inference`). Handlers may override per-call.
type InferenceDefaults struct {
	AudioClassifier string `yaml:"audio_classifier,omitempty" json:"audio_classifier,omitempty"`
	TextClassifier  string `yaml:"text_classifier,omitempty" json:"text_classifier,omitempty"`
	ImageClassifier string `yaml:"image_classifier,omitempty" json:"image_classifier,omitempty"`
	VideoClassifier string `yaml:"video_classifier,omitempty" json:"video_classifier,omitempty"`
	Embedder        string `yaml:"embedder,omitempty" json:"embedder,omitempty"`
}

// ProviderConfig represents a Provider configuration in K8s-style manifest format
type ProviderConfig struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Provider   `yaml:"spec" json:"spec"`
}

// ProviderConfigK8s is the K8s-compatible version for unmarshaling
type ProviderConfigK8s struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Provider          `yaml:"spec" json:"spec"`
}

// Provider defines API connection and defaults
type Provider struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Type  string `json:"type" yaml:"type"`
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Voice is the vendor-specific voice identifier used when Capability is
	// "tts". For Cartesia it's the voice UUID; for ElevenLabs the voice ID;
	// for OpenAI the voice name (alloy, nova, etc.). Ignored for LLM/STT
	// providers.
	Voice string `json:"voice,omitempty" yaml:"voice,omitempty"`
	// SampleRate is the audio sample rate in Hz for TTS providers. Common
	// values: 16000 (telephony), 24000 (default for most TTS vendors),
	// 48000 (high quality). Ignored for non-TTS providers.
	SampleRate int `json:"sample_rate,omitempty" yaml:"sample_rate,omitempty"`
	// AudioFiles is the list of PCM fixtures used by the mock TTS provider
	// (capability=tts, type=mock). The mock service rotates through these
	// files on each Synthesize() call. Paths are relative to the arena
	// config directory. Ignored when type != "mock" or capability != "tts".
	AudioFiles []string `json:"audio_files,omitempty" yaml:"audio_files,omitempty"`
	// Role tags what this provider does. One of "llm" (default), "tts", or
	// "stt". The arena uses this to route the provider to the correct
	// registry and to skip non-llm providers when building the
	// agent-under-test matrix. Distinct from the Capabilities field which
	// lists per-model feature flags (vision, tools, etc.).
	//
	// Renamed from "capability" 2026-05-18 to avoid singular/plural
	// collision with Capabilities. The field is required for tts/stt
	// providers; defaults to "llm" when empty.
	Role    string `json:"role,omitempty" yaml:"role,omitempty" jsonschema:"enum=llm,enum=tts,enum=stt,enum=embedding,enum=image,enum=video,enum=inference"` //nolint:lll // enum list can't be split inside a struct tag
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	// Headers specifies custom HTTP headers to include in every request to
	// this provider. Useful for OpenAI-compatible gateways (OpenRouter,
	// LiteLLM, etc.) that require app attribution or custom auth headers.
	// Values are plain strings — use the credentials field for secrets.
	// Collisions with built-in provider headers (Authorization, Content-Type,
	// etc.) are rejected at request time.
	Headers          map[string]string      `json:"headers,omitempty" yaml:"headers,omitempty"`
	RateLimit        RateLimit              `json:"rate_limit,omitempty" yaml:"rate_limit,omitempty"`
	Defaults         ProviderDefaults       `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Pricing          Pricing                `json:"pricing,omitempty" yaml:"pricing,omitempty"`
	PricingCorrectAt string                 `json:"pricing_correct_at,omitempty" yaml:"pricing_correct_at,omitempty"`
	IncludeRawOutput bool                   `json:"include_raw_output,omitempty" yaml:"include_raw_output,omitempty"` // Include raw API requests in output for debugging
	AdditionalConfig map[string]interface{} `json:"additional_config,omitempty" yaml:"additional_config,omitempty"`   // Additional provider-specific configuration
	Credential       *CredentialConfig      `json:"credential,omitempty" yaml:"credential,omitempty"`
	Platform         *PlatformConfig        `json:"platform,omitempty" yaml:"platform,omitempty"`
	// Capabilities lists what this provider supports: text, streaming, vision, tools, json, audio, video, documents
	Capabilities []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	// UnsupportedParams lists model parameters not supported by this provider model
	// (e.g. "temperature", "top_p", "max_tokens").
	UnsupportedParams []string `json:"unsupported_params,omitempty" yaml:"unsupported_params,omitempty"`
	// RequestTimeout caps the wall-clock duration of request/response HTTP
	// calls (Predict, embeddings, etc.) via http.Client.Timeout. Does NOT
	// apply to SSE streaming calls, which are unbounded by wall-clock and
	// governed only by StreamIdleTimeout and context cancellation. Empty
	// falls back to the provider's default (typically 60s). Go duration
	// string, e.g. "2m", "90s".
	RequestTimeout string `json:"request_timeout,omitempty" yaml:"request_timeout,omitempty"`
	// StreamIdleTimeout bounds how long an SSE streaming body may remain
	// silent (no bytes) between reads before the stream is aborted. The
	// timer resets on every byte received, so legitimately long-running
	// streams (e.g. hours-long sessions delivering sparse output) are not
	// affected. Empty falls back to providers.DefaultStreamIdleTimeout
	// (30s). Go duration string, e.g. "60s", "2m".
	StreamIdleTimeout string `json:"stream_idle_timeout,omitempty" yaml:"stream_idle_timeout,omitempty"`
	// StreamRetry configures bounded retry for streaming requests that fail
	// before any content chunk has been forwarded downstream (the
	// "pre-first-chunk window"). Targets transient HTTP/2 stream resets and
	// initial-connection errors without risking duplicate content emission.
	// Disabled by default. See docs/local-backlog/STREAMING_RETRY_AT_SCALE.md.
	StreamRetry *StreamRetryConfig `json:"stream_retry,omitempty" yaml:"stream_retry,omitempty"`
	// StreamMaxConcurrent caps the number of concurrent streaming requests
	// the provider will have in flight at any time. Requests beyond the
	// limit block on the caller's context — a short deadline acts as
	// fail-fast, a long one queues. Zero or negative means unlimited
	// (current default). Reduces goroutine/timer explosion under load.
	StreamMaxConcurrent int `json:"stream_max_concurrent,omitempty" yaml:"stream_max_concurrent,omitempty"`
	// HTTPTransport configures the per-provider HTTP connection pool.
	// Lets operators raise MaxConnsPerHost and related limits above the
	// single-process defaults when scaling up concurrent streams per
	// upstream. Note that raising MaxConnsPerHost alone is not sufficient
	// if the upstream advertises a low SETTINGS_MAX_CONCURRENT_STREAMS
	// (RFC 7540 §6.5.2) — the effective ceiling is
	// MaxConnsPerHost × SETTINGS_MAX_CONCURRENT_STREAMS per upstream.
	// See AltairaLabs/PromptKit#873.
	HTTPTransport *HTTPTransportConfig `json:"http_transport,omitempty" yaml:"http_transport,omitempty"`
}

// ProviderSpec is the unified, post-compat shape used internally for any
// provider type (inference, TTS, STT, embedding, image).
//
// Loaded by config.LoadProviderSpec which accepts both legacy (top-level
// type/model/pricing) and unified (impl + capabilities[]) YAML shapes.
type ProviderSpec struct {
	Name         string // from metadata.name
	Impl         string // implementation discriminator (e.g. "openai", "imagen")
	Endpoint     string // base URL
	Auth         AuthSpec
	Timeouts     TimeoutsSpec
	Retry        RetrySpec
	Capabilities []CapabilitySpec
}

// CapabilitySpec is one capability entry under a ProviderSpec.
type CapabilitySpec struct {
	Type     base.ProviderType
	Model    string
	Defaults map[string]any
	Pricing  *base.PricingDescriptor
}

// AuthSpec describes how the provider authenticates.
type AuthSpec struct {
	Type string `yaml:"type,omitempty"` // "api_key" | "oauth" | etc.
	Env  string `yaml:"env,omitempty"`  // env var name for api_key auth
}

// TimeoutsSpec holds request-level timeouts.
type TimeoutsSpec struct {
	Request time.Duration `yaml:"request,omitempty"`
}

// RetrySpec configures generic retry policy.
type RetrySpec struct {
	MaxAttempts int    `yaml:"max_attempts,omitempty"`
	Backoff     string `yaml:"backoff,omitempty"`
}

// HTTPTransportConfig configures the per-provider HTTP connection pool
// used for both request/response and streaming calls. Empty or zero
// fields fall back to the runtime's built-in defaults
// (providers.DefaultMaxConnsPerHost etc.), so operators only need to
// set the values they want to override.
//
// See AltairaLabs/PromptKit#873 for motivation. The
// promptkit_http_conns_in_use gauge is the operational signal for
// tuning these values.
type HTTPTransportConfig struct {
	// MaxConnsPerHost caps the total TCP connections the transport may
	// open to any single upstream host (in-use + idle). Zero means
	// unlimited (the default, matching Go's http.Transport). Set a
	// positive value to cap connections for backends that need it.
	MaxConnsPerHost int `json:"max_conns_per_host,omitempty" yaml:"max_conns_per_host,omitempty"`
	// MaxIdleConnsPerHost caps idle keep-alive connections retained per
	// host for reuse. Zero or negative falls back to
	// providers.DefaultMaxIdleConnsPerHost (100). Usually tracked to
	// MaxConnsPerHost so the whole pool is reusable.
	MaxIdleConnsPerHost int `json:"max_idle_conns_per_host,omitempty" yaml:"max_idle_conns_per_host,omitempty"`
	// IdleConnTimeout is how long an idle keep-alive connection lingers
	// before being closed. Empty falls back to
	// providers.DefaultIdleConnTimeout (90s). Go duration string, e.g.
	// "60s", "5m".
	IdleConnTimeout string `json:"idle_conn_timeout,omitempty" yaml:"idle_conn_timeout,omitempty"`
}

// StreamRetryConfig configures pre-first-chunk streaming retry behavior for
// a provider. See docs/local-backlog/STREAMING_RETRY_AT_SCALE.md for design.
type StreamRetryConfig struct {
	// Enabled turns the retry loop on. Defaults to false — streaming retry
	// changes latency/billing semantics and must be opt-in per provider.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// MaxAttempts is the total number of attempts including the initial
	// request. A value of 2 means "initial request plus at most one retry".
	// Values <1 are treated as 1 (no retry). Empty falls back to 2.
	MaxAttempts int `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`
	// InitialDelay is the base delay before the first retry. Subsequent
	// retries use exponential backoff with full jitter up to MaxDelay. Go
	// duration string. Empty falls back to "250ms".
	InitialDelay string `json:"initial_delay,omitempty" yaml:"initial_delay,omitempty"`
	// MaxDelay caps the per-attempt backoff delay. Go duration string.
	// Empty falls back to "2s".
	MaxDelay string `json:"max_delay,omitempty" yaml:"max_delay,omitempty"`
	// RetryWindow controls which point in the stream lifecycle is still
	// eligible for retry. "pre_first_chunk" (the only currently supported
	// value) retries only if no content chunk has been forwarded yet.
	// Future values may be gated on deduplication support.
	RetryWindow string `json:"retry_window,omitempty" yaml:"retry_window,omitempty"`
	// Budget configures a token bucket that rate-limits retry attempts
	// across all in-flight requests on this provider. Protects against
	// thundering-herd reconnects when a single upstream connection reset
	// kills many streams simultaneously. When nil, retries are unbounded
	// (Phase 1 behavior).
	Budget *StreamRetryBudgetConfig `json:"budget,omitempty" yaml:"budget,omitempty"`
}

// StreamRetryBudgetConfig configures the token bucket that gates retry
// attempts to prevent thundering-herd reconnects under upstream degradation.
// Only retries consume tokens; the initial attempt of each request is
// always allowed through.
type StreamRetryBudgetConfig struct {
	// RatePerSec is the sustained token refill rate. Empty or non-positive
	// disables the budget (unbounded retries).
	RatePerSec float64 `json:"rate_per_sec,omitempty" yaml:"rate_per_sec,omitempty"`
	// Burst is the maximum number of tokens that can accumulate. Empty
	// or non-positive disables the budget.
	Burst int `json:"burst,omitempty" yaml:"burst,omitempty"`
}

// CredentialConfig is an alias for credentials.CredentialConfig.
// The canonical type lives in runtime/credentials to break the circular
// module dependency between pkg and runtime.
type CredentialConfig = credentials.CredentialConfig

// PlatformConfig is an alias for credentials.PlatformConfig.
type PlatformConfig = credentials.PlatformConfig

// Pricing defines cost per 1K tokens for input and output
type Pricing struct {
	InputCostPer1K  float64 `json:"input_cost_per_1k" yaml:"input_cost_per_1k"`
	OutputCostPer1K float64 `json:"output_cost_per_1k" yaml:"output_cost_per_1k"`
}

// RateLimit defines rate limiting parameters
type RateLimit struct {
	RPS   int `json:"rps" yaml:"rps"`
	Burst int `json:"burst" yaml:"burst"`
}

// ProviderDefaults defines default parameters for a provider
type ProviderDefaults struct {
	Temperature float32 `json:"temperature" yaml:"temperature"`
	TopP        float32 `json:"top_p" yaml:"top_p"`
	MaxTokens   int     `json:"max_tokens" yaml:"max_tokens"`
	// PromptCaching controls Anthropic prompt caching. Omit or set true to enable
	// (the default); set false to disable. Only affects Claude providers.
	PromptCaching *bool `json:"prompt_caching,omitempty" yaml:"prompt_caching,omitempty"`
}

// ToolSpec represents a tool descriptor (re-exported from runtime/tools for schema generation)
type ToolSpec struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Description string `json:"description" yaml:"description"`
	// InputSchema is a JSON Schema Draft-07 document for the tool's arguments.
	InputSchema interface{} `json:"input_schema" yaml:"input_schema"`
	// OutputSchema is a JSON Schema Draft-07 document for the tool's return value.
	OutputSchema interface{}    `json:"output_schema" yaml:"output_schema"`
	Mode         string         `json:"mode" yaml:"mode" jsonschema:"description=Execution mode. One of: 'mock' (use mock_result or mock_template) - 'live' (HTTP via 'http') - 'mcp' (MCP server) - 'exec' (subprocess) - 'client' (client-side handler)."` //nolint:lll
	TimeoutMs    int            `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	MockResult   interface{}    `json:"mock_result,omitempty" yaml:"mock_result,omitempty" jsonschema:"description=Static mock response returned regardless of tool-call args. Use when the response does not depend on inputs. Mutually exclusive with mock_template."`                                                                                                                                                            //nolint:lll
	MockTemplate string         `json:"mock_template,omitempty" yaml:"mock_template,omitempty" jsonschema:"description=Go text/template rendered against tool-call args (parsed as a JSON map). Rendered output is parsed back as JSON. Use this instead of mock_result when the response should depend on inputs (e.g. branching on order_id with {{ if eq .order_id \"X\" }}...{{ end }}). Mutually exclusive with mock_result."` //nolint:lll
	MockParts    []MockPartSpec `json:"mock_parts,omitempty" yaml:"mock_parts,omitempty"`
	HTTPConfig   *HTTPConfig    `json:"http,omitempty" yaml:"http,omitempty"`
	// Exec subprocess configuration (mode: exec)
	ExecConfig *ExecBinding `json:"exec,omitempty" yaml:"exec,omitempty"`
	// Client-side execution configuration
	ClientConfig *ToolClientConfig `json:"client,omitempty" yaml:"client,omitempty"`
}

// MockPartSpec describes a single multimodal content part in mock_parts (schema generation).
type MockPartSpec struct {
	Type  string         `json:"type" yaml:"type"`                     // "text", "image", "audio", "video", "document"
	Text  string         `json:"text,omitempty" yaml:"text,omitempty"` // For type=text
	Media *MockMediaSpec `json:"media,omitempty" yaml:"media,omitempty"`
}

// MockMediaSpec describes media content in a mock part (schema generation).
type MockMediaSpec struct {
	FilePath string `json:"file_path,omitempty" yaml:"file_path,omitempty"` // Local file path (resolved at execution)
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`             // External URL
	MIMEType string `json:"mime_type" yaml:"mime_type"`
	Width    *int   `json:"width,omitempty" yaml:"width,omitempty"`
	Height   *int   `json:"height,omitempty" yaml:"height,omitempty"`
	Caption  string `json:"caption,omitempty" yaml:"caption,omitempty"`
}

// ToolClientConfig defines configuration for client-side tool execution (schema generation)
type ToolClientConfig struct {
	Consent        *ToolConsentConfig `json:"consent,omitempty" yaml:"consent,omitempty"`
	TimeoutMs      int                `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Categories     []string           `json:"categories,omitempty" yaml:"categories,omitempty"`
	ValidateOutput bool               `json:"validate_output,omitempty" yaml:"validate_output,omitempty"`
}

// ToolConsentConfig defines consent requirements for client-side tools (schema generation)
type ToolConsentConfig struct {
	Required        bool   `json:"required" yaml:"required"`
	Message         string `json:"message,omitempty" yaml:"message,omitempty"`
	DeclineStrategy string `json:"decline_strategy,omitempty" yaml:"decline_strategy,omitempty"`
}

// HTTPConfig is an alias for the canonical type in runtime/tools.
type HTTPConfig = tools.HTTPConfig

// RequestMapping is an alias for the canonical type in runtime/tools.
type RequestMapping = tools.RequestMapping

// ResponseMapping is an alias for the canonical type in runtime/tools.
type ResponseMapping = tools.ResponseMapping

// MultimodalConfig is an alias for the canonical type in runtime/tools.
type MultimodalConfig = tools.MultimodalConfig
