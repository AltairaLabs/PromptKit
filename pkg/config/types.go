package config

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// ObjectMeta is a simplified metadata structure for PromptKit configs
// Based on K8s ObjectMeta but with YAML-friendly tags and optional fields
type ObjectMeta struct {
	Name        string            `yaml:"name,omitempty" jsonschema:"title=Name,description=Name of the resource"`
	Namespace   string            `yaml:"namespace,omitempty" jsonschema:"title=Namespace,description=Namespace for the resource"`
	Labels      map[string]string `yaml:"labels,omitempty" jsonschema:"title=Labels,description=Key-value pairs for organizing resources"`
	Annotations map[string]string `yaml:"annotations,omitempty" jsonschema:"title=Annotations,description=Additional metadata"`
}

// PromptConfigRef references a prompt builder configuration
type PromptConfigRef struct {
	ID   string            `yaml:"id"`
	File string            `yaml:"file"`
	Vars map[string]string `yaml:"vars,omitempty"`
}

// ArenaConfig represents the main Arena configuration in K8s-style manifest format
type ArenaConfig struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Config     `yaml:"spec"`
}

// ArenaConfigK8s represents the Arena configuration using full K8s ObjectMeta for unmarshaling
// This is used internally for compatibility with k8s.io types
type ArenaConfigK8s struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Config            `yaml:"spec"`
}

// Config represents the main configuration structure
type Config struct {
	// File references for YAML serialization
	PromptConfigs []PromptConfigRef `yaml:"prompt_configs,omitempty"`
	Providers     []ProviderRef     `yaml:"providers"`
	Judges        []JudgeRef        `yaml:"judges,omitempty"`
	JudgeDefaults *JudgeDefaults    `yaml:"judge_defaults,omitempty"`
	Scenarios     []ScenarioRef     `yaml:"scenarios,omitempty"`
	Evals         []EvalRef         `yaml:"evals,omitempty"`
	Tools         []ToolRef         `yaml:"tools,omitempty"`
	PackEvals     []evals.EvalDef   `yaml:"pack_evals,omitempty" json:"pack_evals,omitempty"`
	Workflow      interface{}       `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Agents        interface{}       `yaml:"agents,omitempty" json:"agents,omitempty"`
	Deploy        *DeployConfig     `yaml:"deploy,omitempty" json:"deploy,omitempty"`
	MCPServers    []MCPServerConfig `yaml:"mcp_servers,omitempty"`
	A2AAgents     []A2AAgentConfig  `yaml:"a2a_agents,omitempty"`
	StateStore    *StateStoreConfig `yaml:"state_store,omitempty"`
	Defaults      Defaults          `yaml:"defaults"`
	SelfPlay      *SelfPlayConfig   `yaml:"self_play,omitempty"`
	PackFile      string            `yaml:"pack_file,omitempty"`
	// ProviderGroups maps provider ID to configured group (populated during load)
	ProviderGroups map[string]string `yaml:"-" json:"-"`
	// ProviderCapabilities maps provider ID to its capabilities (populated during load)
	ProviderCapabilities map[string][]string `yaml:"-" json:"-"`

	// Loaded resources (populated by LoadConfig, not serialized)
	LoadedPromptConfigs map[string]*PromptConfigData `yaml:"-" json:"-"` // taskType -> config
	LoadedProviders     map[string]*Provider         `yaml:"-" json:"-"` // provider ID -> provider
	LoadedJudges        map[string]*JudgeTarget      `yaml:"-" json:"-"` // judge name -> resolved target
	LoadedScenarios     map[string]*Scenario         `yaml:"-" json:"-"` // scenario ID -> scenario
	LoadedEvals         map[string]*Eval             `yaml:"-" json:"-"` // eval ID -> eval
	LoadedTools         []ToolData                   `yaml:"-" json:"-"` // list of tool data
	LoadedPersonas      map[string]*UserPersonaPack  `yaml:"-" json:"-"` // persona ID -> persona
	LoadedPack          *prompt.Pack                 `yaml:"-" json:"-"` // loaded pack from pack_file

	// Pack eval settings (set by CLI flags, not serialized)
	SkipPackEvals  bool     `yaml:"-" json:"-"` // Disable pack eval execution
	EvalTypeFilter []string `yaml:"-" json:"-"` // Filter to specific eval types

	// Base directory for resolving relative paths (set during LoadConfig)
	ConfigDir string `yaml:"-" json:"-"`
}

const (
	defaultStateStoreType = "memory"
)

// GetStateStoreType returns the configured state store type, defaulting to "memory" if not specified.
func (c *Config) GetStateStoreType() string {
	if c.StateStore == nil {
		return defaultStateStoreType
	}
	if c.StateStore.Type == "" {
		return defaultStateStoreType
	}
	return c.StateStore.Type
}

// GetStateStoreConfig returns the state store configuration, creating a default memory config if not specified.
func (c *Config) GetStateStoreConfig() *StateStoreConfig {
	if c.StateStore == nil {
		return &StateStoreConfig{
			Type: defaultStateStoreType,
		}
	}
	// Set default type if not specified
	if c.StateStore.Type == "" {
		c.StateStore.Type = defaultStateStoreType
	}
	return c.StateStore
}

// MCPServerConfig represents configuration for an MCP server
type MCPServerConfig struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// A2AAgentConfig defines a mock A2A agent for Arena testing.
type A2AAgentConfig struct {
	Name      string            `yaml:"name"`
	Card      A2ACardConfig     `yaml:"card"`
	Responses []A2AResponseRule `yaml:"responses"`
}

// A2ACardConfig defines the agent card for a mock A2A agent.
type A2ACardConfig struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Skills      []A2ASkillConfig `yaml:"skills"`
}

// A2ASkillConfig defines a single skill in a mock A2A agent card.
type A2ASkillConfig struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags,omitempty"`
}

// A2AResponseRule defines a response rule for a mock A2A agent.
type A2AResponseRule struct {
	Skill    string             `yaml:"skill"`
	Match    *A2AMatchConfig    `yaml:"match,omitempty"`
	Response *A2AResponseConfig `yaml:"response,omitempty"`
	Error    string             `yaml:"error,omitempty"`
}

// A2AMatchConfig defines how to match incoming messages for a mock A2A agent.
type A2AMatchConfig struct {
	Contains string `yaml:"contains,omitempty"`
	Regex    string `yaml:"regex,omitempty"`
}

// A2AResponseConfig defines the response parts for a mock A2A agent.
type A2AResponseConfig struct {
	Parts []A2APartConfig `yaml:"parts"`
}

// A2APartConfig defines a single response part for a mock A2A agent.
type A2APartConfig struct {
	Text string `yaml:"text"`
}

// StateStoreConfig represents configuration for conversation state storage
type StateStoreConfig struct {
	// Type specifies the state store implementation: "memory" or "redis"
	Type string `yaml:"type"`

	// Redis configuration (only used when Type is "redis")
	Redis *RedisConfig `yaml:"redis,omitempty"`
}

// RedisConfig contains Redis-specific configuration
type RedisConfig struct {
	// Address of the Redis server (e.g., "localhost:6379")
	Address string `yaml:"address"`

	// Password for Redis authentication (optional)
	Password string `yaml:"password,omitempty"`

	// Database number (0-15, default is 0)
	Database int `yaml:"database,omitempty"`

	// TTL for conversation state (e.g., "24h", "7d"). Default is "24h"
	TTL string `yaml:"ttl,omitempty"`

	// Prefix for Redis keys (default is "promptkit")
	Prefix string `yaml:"prefix,omitempty"`
}

// DeployConfig represents deployment configuration for the arena
type DeployConfig struct {
	// Provider specifies the deployment provider (e.g., "agentcore", "ecs", "k8s")
	Provider string `yaml:"provider" json:"provider" jsonschema:"required"`
	// Config holds provider-specific configuration (opaque to core)
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
	// Environments defines per-environment configuration overrides
	Environments map[string]*DeployEnvironment `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// DeployEnvironment defines environment-specific deployment configuration
type DeployEnvironment struct {
	// Config holds environment-specific provider configuration (merged with top-level config)
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
}

// PromptConfigData holds a loaded prompt configuration with its file path
type PromptConfigData struct {
	FilePath string            // relative to ConfigDir
	Config   interface{}       // parsed prompt configuration (*prompt.Config at runtime)
	TaskType string            // extracted from Config.Spec.TaskType
	Vars     map[string]string // Variable overrides from arena.yaml
}

// ToolData holds raw tool configuration data
type ToolData struct {
	FilePath string
	Data     []byte
}

// ProviderRef references a provider configuration file
type ProviderRef struct {
	File  string `yaml:"file"`
	Group string `yaml:"group,omitempty"`
}

// JudgeRef references a judge configuration mapped to a provider.
// Mirrors self-play role/provider mapping to allow multiple judge targets.
type JudgeRef struct {
	Name     string `yaml:"name"`            // Judge identifier used in assertions
	Provider string `yaml:"provider"`        // Provider ID reference (must exist in spec.providers)
	Model    string `yaml:"model,omitempty"` // Optional model override for the judge
}

// JudgeTarget is a resolved judge reference with provider config and effective model.
type JudgeTarget struct {
	Name     string    // Judge identifier
	Provider *Provider // Resolved provider config
	Model    string    // Effective model (judge override or provider model)
}

// ScenarioRef references a scenario file
type ScenarioRef struct {
	File string `yaml:"file"`
}

// EvalRef references an eval configuration file
type EvalRef struct {
	File string `yaml:"file"`
}

// ToolRef references a tool configuration file
type ToolRef struct {
	File string `yaml:"file"`
}

// SelfPlayConfig configures self-play functionality
type SelfPlayConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Personas []PersonaRef        `yaml:"personas"`
	Roles    []SelfPlayRoleGroup `yaml:"roles"`
}

// PersonaRef references a persona file
type PersonaRef struct {
	File string `yaml:"file"`
}

// SelfPlayRoleGroup defines user LLM configuration for self-play
type SelfPlayRoleGroup struct {
	ID       string `yaml:"id"`
	Provider string `yaml:"provider"` // Provider ID reference (must exist in spec.providers)
}

// Defaults contains default configuration values
type Defaults struct {
	Temperature float32      `yaml:"temperature,omitempty"`
	MaxTokens   int          `yaml:"max_tokens,omitempty"`
	Seed        int          `yaml:"seed,omitempty"`
	Concurrency int          `yaml:"concurrency,omitempty"`
	Output      OutputConfig `yaml:"output,omitempty"`
	// ConfigDir is the base directory for all config files (prompts, providers, scenarios, tools).
	// If not set, defaults to the directory containing the main config file.
	// If the main config file path is not known, defaults to current working directory.
	ConfigDir string   `yaml:"config_dir,omitempty"`
	FailOn    []string `yaml:"fail_on,omitempty"`
	Verbose   bool     `yaml:"verbose,omitempty"`

	// Deprecated fields for backward compatibility (will be removed)
	HTMLReport     string          `yaml:"html_report,omitempty"`
	OutDir         string          `yaml:"out_dir,omitempty"`
	OutputFormats  []string        `yaml:"output_formats,omitempty"`
	MarkdownConfig *MarkdownConfig `yaml:"markdown_config,omitempty"`
}

// JudgeDefaults configures default judge prompt selection.
type JudgeDefaults struct {
	Prompt         string `yaml:"prompt,omitempty"`
	PromptRegistry string `yaml:"prompt_registry,omitempty"`
}

// OutputConfig contains configuration for all output formats
type OutputConfig struct {
	Dir       string                `yaml:"dir"`                 // Base output directory
	Formats   []string              `yaml:"formats"`             // List of enabled formats: json, html, markdown, junit
	JSON      *JSONOutputConfig     `yaml:"json,omitempty"`      // JSON-specific configuration
	HTML      *HTMLOutputConfig     `yaml:"html,omitempty"`      // HTML-specific configuration
	Markdown  *MarkdownOutputConfig `yaml:"markdown,omitempty"`  // Markdown-specific configuration
	JUnit     *JUnitOutputConfig    `yaml:"junit,omitempty"`     // JUnit-specific configuration
	Recording *RecordingConfig      `yaml:"recording,omitempty"` // Session recording configuration
}

// JSONOutputConfig contains configuration options for JSON output
type JSONOutputConfig struct {
	// Future: could add options like pretty printing, compression, etc.
}

// HTMLOutputConfig contains configuration options for HTML output
type HTMLOutputConfig struct {
	File string `yaml:"file,omitempty"` // Custom HTML output file name
	// Future: could add theme, template, styling options, etc.
}

// MarkdownOutputConfig contains configuration options for markdown output formatting
type MarkdownOutputConfig struct {
	File              string `yaml:"file,omitempty"`      // Custom markdown output file name
	IncludeDetails    bool   `yaml:"include_details"`     // Include detailed test information
	ShowOverview      bool   `yaml:"show_overview"`       // Show executive overview section
	ShowResultsMatrix bool   `yaml:"show_results_matrix"` // Show results matrix table
	ShowFailedTests   bool   `yaml:"show_failed_tests"`   // Show failed tests section
	ShowCostSummary   bool   `yaml:"show_cost_summary"`   // Show cost analysis section
}

// JUnitOutputConfig contains configuration options for JUnit XML output
type JUnitOutputConfig struct {
	File string `yaml:"file,omitempty"` // Custom JUnit output file name
	// Future: could add options like test suite naming, etc.
}

// RecordingConfig contains configuration for session recording.
// Session recordings capture the complete event stream for replay and analysis.
type RecordingConfig struct {
	Enabled bool   `yaml:"enabled"`       // Enable session recording
	Dir     string `yaml:"dir,omitempty"` // Subdirectory within output.dir (default: "recordings")
}

// Deprecated: Use MarkdownOutputConfig instead
type MarkdownConfig struct {
	IncludeDetails    bool `yaml:"include_details"`     // Include detailed test information
	ShowOverview      bool `yaml:"show_overview"`       // Show executive overview section
	ShowResultsMatrix bool `yaml:"show_results_matrix"` // Show results matrix table
	ShowFailedTests   bool `yaml:"show_failed_tests"`   // Show failed tests section
	ShowCostSummary   bool `yaml:"show_cost_summary"`   // Show cost analysis section
}

// GetOutputConfig returns the effective output configuration, handling backward compatibility
func (d *Defaults) GetOutputConfig() OutputConfig {
	// If new format is used, return it directly
	if d.Output.Dir != "" || len(d.Output.Formats) > 0 {
		return d.Output
	}

	// Handle backward compatibility
	output := OutputConfig{
		Dir:     d.OutDir,
		Formats: d.OutputFormats,
	}

	// Default dir if not specified
	if output.Dir == "" {
		output.Dir = "out"
	}

	// Default formats if not specified
	if len(output.Formats) == 0 {
		output.Formats = []string{"json"}
	}

	// Migrate HTML config
	if d.HTMLReport != "" {
		output.HTML = &HTMLOutputConfig{
			File: d.HTMLReport,
		}
	}

	// Migrate Markdown config
	if d.MarkdownConfig != nil {
		output.Markdown = &MarkdownOutputConfig{
			IncludeDetails:    d.MarkdownConfig.IncludeDetails,
			ShowOverview:      d.MarkdownConfig.ShowOverview,
			ShowResultsMatrix: d.MarkdownConfig.ShowResultsMatrix,
			ShowFailedTests:   d.MarkdownConfig.ShowFailedTests,
			ShowCostSummary:   d.MarkdownConfig.ShowCostSummary,
		}
	}

	return output
}

// GetMarkdownOutputConfig returns the markdown configuration with defaults
func (o *OutputConfig) GetMarkdownOutputConfig() *MarkdownOutputConfig {
	if o.Markdown != nil {
		return o.Markdown
	}

	// Return default configuration
	return &MarkdownOutputConfig{
		IncludeDetails:    true,
		ShowOverview:      true,
		ShowResultsMatrix: true,
		ShowFailedTests:   true,
		ShowCostSummary:   true,
	}
}

// GetHTMLOutputConfig returns the HTML configuration with defaults
func (o *OutputConfig) GetHTMLOutputConfig() *HTMLOutputConfig {
	if o.HTML != nil {
		return o.HTML
	}

	// Return default configuration
	return &HTMLOutputConfig{
		File: "report.html",
	}
}

// GetJUnitOutputConfig returns the JUnit configuration with defaults
func (o *OutputConfig) GetJUnitOutputConfig() *JUnitOutputConfig {
	if o.JUnit != nil {
		return o.JUnit
	}

	// Return default configuration
	return &JUnitOutputConfig{
		File: "results.xml",
	}
}

// ContextMetadata provides structured context information for scenarios
type ContextMetadata struct {
	Domain       string `json:"domain,omitempty" yaml:"domain,omitempty"`
	UserRole     string `json:"user_role,omitempty" yaml:"user_role,omitempty"`
	ProjectStage string `json:"project_stage,omitempty" yaml:"project_stage,omitempty"`
}

// ScenarioConfig represents a Scenario in K8s-style manifest format
type ScenarioConfig struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Scenario   `yaml:"spec"`
}

// ScenarioConfigK8s is the K8s-compatible version for unmarshaling
type ScenarioConfigK8s struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Scenario          `yaml:"spec"`
}

// Scenario describes user turns, context, and validation constraints
type Scenario struct {
	ID              string                 `json:"id" yaml:"id"`
	TaskType        string                 `json:"task_type" yaml:"task_type"`
	Mode            string                 `json:"mode,omitempty" yaml:"mode,omitempty"`
	Description     string                 `json:"description" yaml:"description"`
	ContextMetadata *ContextMetadata       `json:"context_metadata,omitempty" yaml:"context_metadata,omitempty"`
	Turns           []TurnDefinition       `json:"turns" yaml:"turns"`
	Context         map[string]interface{} `json:"context,omitempty" yaml:"context,omitempty"`
	Constraints     map[string]interface{} `json:"constraints,omitempty" yaml:"constraints,omitempty"`
	ToolPolicy      *ToolPolicy            `json:"tool_policy,omitempty" yaml:"tool_policy,omitempty"`
	// ProvidersOverride: If empty, uses all arena providers.
	Providers     []string `json:"providers,omitempty" yaml:"providers,omitempty"`
	ProviderGroup string   `json:"provider_group,omitempty" yaml:"provider_group,omitempty"`
	// RequiredCapabilities filters providers to only those supporting all listed capabilities.
	// Valid values: text, streaming, vision, tools, json, audio, video, documents
	RequiredCapabilities []string `json:"required_capabilities,omitempty" yaml:"required_capabilities,omitempty"`
	// Enable streaming for all turns by default.
	Streaming bool `json:"streaming,omitempty" yaml:"streaming,omitempty"`
	// Context management policy for long conversations.
	ContextPolicy *ContextPolicy `json:"context_policy,omitempty" yaml:"context_policy,omitempty"`
	// Assertions evaluated after the entire conversation completes.
	ConversationAssertions []asrt.AssertionConfig `json:"conversation_assertions,omitempty" yaml:"conversation_assertions,omitempty"` //nolint:lll
	// Duplex enables bidirectional streaming mode for voice/audio scenarios.
	Duplex *DuplexConfig `json:"duplex,omitempty" yaml:"duplex,omitempty"`
}

// ShouldStreamTurn returns whether streaming should be used for a specific turn.
// It checks the turn's streaming override first, then falls back to the scenario's streaming setting.
func (s *Scenario) ShouldStreamTurn(turnIndex int) bool {
	if turnIndex < 0 || turnIndex >= len(s.Turns) {
		return s.Streaming // Default to scenario setting if invalid index
	}

	turn := s.Turns[turnIndex]
	if turn.Streaming != nil {
		// Turn has explicit override
		return *turn.Streaming
	}

	// Use scenario-level setting
	return s.Streaming
}

// Validate validates the Scenario and all nested configurations.
func (s *Scenario) Validate() error {
	if s == nil {
		return nil
	}

	// Validate duplex config if present
	if s.Duplex != nil {
		if err := s.Duplex.Validate(); err != nil {
			return fmt.Errorf("scenario %s: %w", s.ID, err)
		}
	}

	// Validate context policy relevance config if present
	if s.ContextPolicy != nil && s.ContextPolicy.Relevance != nil {
		if err := s.ContextPolicy.Relevance.Validate(); err != nil {
			return fmt.Errorf("scenario %s context policy: %w", s.ID, err)
		}
	}

	// Validate TTS configs in turns
	for i := range s.Turns {
		if s.Turns[i].TTS != nil {
			if err := s.Turns[i].TTS.Validate(); err != nil {
				return fmt.Errorf("scenario %s turn %d: %w", s.ID, i, err)
			}
		}
	}

	return nil
}

// ContextPolicy defines context management for a scenario
type ContextPolicy struct {
	// TokenBudget is the maximum tokens for context (0 = unlimited)
	TokenBudget int `json:"token_budget,omitempty" yaml:"token_budget,omitempty"`
	// ReserveForOutput reserves tokens for the response (default 4000)
	ReserveForOutput int `json:"reserve_for_output,omitempty" yaml:"reserve_for_output,omitempty"`
	// Strategy is the truncation strategy: "oldest", "summarize", "relevance", "fail"
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	// CacheBreakpoints enables Anthropic caching
	CacheBreakpoints bool `json:"cache_breakpoints,omitempty" yaml:"cache_breakpoints,omitempty"`
	// Relevance configures embedding-based truncation when Strategy is "relevance"
	Relevance *RelevanceConfig `json:"relevance,omitempty" yaml:"relevance,omitempty"`
}

// RelevanceConfig configures embedding-based relevance truncation.
// Used when ContextPolicy.Strategy is "relevance".
type RelevanceConfig struct {
	// Provider specifies the embedding provider: "openai" or "gemini"
	Provider string `json:"provider" yaml:"provider"`

	// Model optionally overrides the default embedding model for the provider
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// MinRecentMessages always keeps the N most recent messages regardless of relevance.
	// Default: 3
	MinRecentMessages int `json:"min_recent_messages,omitempty" yaml:"min_recent_messages,omitempty"`

	// AlwaysKeepSystemRole keeps all system role messages regardless of score.
	// Default: true
	AlwaysKeepSystemRole *bool `json:"always_keep_system_role,omitempty" yaml:"always_keep_system_role,omitempty"`

	// SimilarityThreshold is the minimum score (0.0-1.0) to consider a message relevant.
	// Messages below this threshold are dropped first. Default: 0.0 (no threshold)
	SimilarityThreshold float64 `json:"similarity_threshold,omitempty" yaml:"similarity_threshold,omitempty"`

	// QuerySource determines what text to compare messages against.
	// Values: "last_user" (default), "last_n", "custom"
	QuerySource string `json:"query_source,omitempty" yaml:"query_source,omitempty"`

	// LastNCount is the number of messages to use when QuerySource is "last_n".
	// Default: 3
	LastNCount int `json:"last_n_count,omitempty" yaml:"last_n_count,omitempty"`

	// CustomQuery is the query text when QuerySource is "custom".
	CustomQuery string `json:"custom_query,omitempty" yaml:"custom_query,omitempty"`

	// CacheEmbeddings enables caching of embeddings across truncation calls.
	// Default: false
	CacheEmbeddings bool `json:"cache_embeddings,omitempty" yaml:"cache_embeddings,omitempty"`
}

// Validate validates the RelevanceConfig settings.
func (r *RelevanceConfig) Validate() error {
	if r == nil {
		return nil
	}

	// Validate provider if specified
	if r.Provider != "" && r.Provider != "openai" && r.Provider != "gemini" && r.Provider != "voyageai" {
		return fmt.Errorf("relevance provider must be 'openai', 'gemini', or 'voyageai', got %q", r.Provider)
	}

	// Validate similarity threshold range
	if r.SimilarityThreshold < 0.0 || r.SimilarityThreshold > 1.0 {
		return fmt.Errorf("similarity_threshold must be between 0.0 and 1.0, got %f", r.SimilarityThreshold)
	}

	// Validate query source
	validQuerySources := map[string]bool{"": true, "last_user": true, "last_n": true, "custom": true}
	if !validQuerySources[r.QuerySource] {
		return fmt.Errorf("query_source must be 'last_user', 'last_n', or 'custom', got %q", r.QuerySource)
	}

	// Validate last_n_count when query_source is "last_n"
	if r.QuerySource == "last_n" && r.LastNCount <= 0 {
		return fmt.Errorf("last_n_count must be positive when query_source is 'last_n'")
	}

	// Validate custom_query when query_source is "custom"
	if r.QuerySource == "custom" && r.CustomQuery == "" {
		return fmt.Errorf("custom_query is required when query_source is 'custom'")
	}

	// Validate min_recent_messages is non-negative
	if r.MinRecentMessages < 0 {
		return fmt.Errorf("min_recent_messages must be non-negative, got %d", r.MinRecentMessages)
	}

	return nil
}

// ToolPolicy defines constraints for tool usage in scenarios
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice" yaml:"tool_choice"` // "auto" | "required" | "none"
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn" yaml:"max_tool_calls_per_turn"`
	MaxTotalToolCalls   int      `json:"max_total_tool_calls" yaml:"max_total_tool_calls"`
	Blocklist           []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

// Turn detection mode constants
const (
	// TurnDetectionModeVAD uses voice activity detection for turn boundaries.
	TurnDetectionModeVAD = "vad"
	// TurnDetectionModeASM uses provider-native (server-side) turn detection.
	TurnDetectionModeASM = "asm"
)

// DuplexConfig enables duplex (bidirectional) streaming mode for a scenario.
// When enabled, audio is streamed in chunks and turn boundaries are detected dynamically.
type DuplexConfig struct {
	// Timeout is the maximum session duration (e.g., "10m", "5m30s").
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// TurnDetection configures how turn boundaries are detected.
	TurnDetection *TurnDetectionConfig `json:"turn_detection,omitempty" yaml:"turn_detection,omitempty"`
	// Resilience configures error handling and retry behavior.
	Resilience *DuplexResilienceConfig `json:"resilience,omitempty" yaml:"resilience,omitempty"`
}

// DuplexResilienceConfig configures error handling and retry behavior for duplex streaming.
// These settings help handle transient failures and provider-specific behaviors.
type DuplexResilienceConfig struct {
	// MaxRetries is the number of retry attempts for failed turns (default: 0).
	// Each retry creates a new session if the previous one ended.
	MaxRetries int `json:"max_retries,omitempty" yaml:"max_retries,omitempty"`
	// RetryDelayMs is the delay in milliseconds between retries (default: 1000).
	RetryDelayMs int `json:"retry_delay_ms,omitempty" yaml:"retry_delay_ms,omitempty"`
	// InterTurnDelayMs is the delay in milliseconds between turns (default: 500).
	// This allows the provider to fully process the previous response.
	InterTurnDelayMs int `json:"inter_turn_delay_ms,omitempty" yaml:"inter_turn_delay_ms,omitempty"`
	// SelfplayInterTurnDelayMs is the delay after selfplay turns (default: 1000).
	// Longer delay needed because TTS audio can be lengthy.
	//nolint:lll // JSON/YAML tag names must match field names for clarity
	SelfplayInterTurnDelayMs int `json:"selfplay_inter_turn_delay_ms,omitempty" yaml:"selfplay_inter_turn_delay_ms,omitempty"`
	// PartialSuccessMinTurns is the minimum completed turns to accept partial success (default: 1).
	// If the session ends unexpectedly but this many turns completed, treat as success.
	PartialSuccessMinTurns int `json:"partial_success_min_turns,omitempty" yaml:"partial_success_min_turns,omitempty"`
	// IgnoreLastTurnSessionEnd treats session end on the final turn as success (default: true).
	// Useful when providers may close sessions after completing the expected conversation.
	//nolint:lll // JSON/YAML tag names must match field names for clarity
	IgnoreLastTurnSessionEnd *bool `json:"ignore_last_turn_session_end,omitempty" yaml:"ignore_last_turn_session_end,omitempty"`
}

// TurnDetectionConfig configures turn detection for duplex mode.
type TurnDetectionConfig struct {
	// Mode specifies the turn detection method: "vad" (voice activity detection) or "asm" (provider-native).
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
	// VAD contains voice activity detection settings (used when Mode is "vad").
	VAD *VADConfig `json:"vad,omitempty" yaml:"vad,omitempty"`
}

// VADConfig configures voice activity detection for turn detection.
type VADConfig struct {
	// SilenceThresholdMs is the silence duration in milliseconds to trigger turn end (default: 500).
	SilenceThresholdMs int `json:"silence_threshold_ms,omitempty" yaml:"silence_threshold_ms,omitempty"`
	// MinSpeechMs is the minimum speech duration before silence counts (default: 1000).
	MinSpeechMs int `json:"min_speech_ms,omitempty" yaml:"min_speech_ms,omitempty"`
	// MaxTurnDurationS forces turn end after this duration in seconds (default: 60).
	MaxTurnDurationS int `json:"max_turn_duration_s,omitempty" yaml:"max_turn_duration_s,omitempty"`
}

// TTSConfig configures text-to-speech for self-play audio generation in duplex mode.
type TTSConfig struct {
	// Provider is the TTS provider (e.g., "openai", "elevenlabs", "cartesia", "mock").
	Provider string `json:"provider" yaml:"provider"`
	// Voice is the voice ID to use for synthesis.
	Voice string `json:"voice" yaml:"voice"`
	// AudioFiles is a list of PCM audio files to use for mock TTS provider.
	// When provider is "mock", these files are loaded and rotated through for each synthesis call.
	// Paths are relative to the scenario file location.
	AudioFiles []string `json:"audio_files,omitempty" yaml:"audio_files,omitempty"`
	// SampleRate is the sample rate of the TTS output in Hz.
	// Default is 24000 for most TTS providers. For mock provider with pre-recorded files,
	// set this to match the actual file sample rate (e.g., 16000 for 16kHz PCM files).
	SampleRate int `json:"sample_rate,omitempty" yaml:"sample_rate,omitempty"`
}

// Validate validates the DuplexConfig settings.
func (d *DuplexConfig) Validate() error {
	if d == nil {
		return nil
	}

	// Validate timeout format if provided
	if d.Timeout != "" {
		if _, err := time.ParseDuration(d.Timeout); err != nil {
			return fmt.Errorf("invalid duplex timeout format %q: %w", d.Timeout, err)
		}
	}

	// Validate turn detection config
	if d.TurnDetection != nil {
		if err := d.TurnDetection.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetTimeoutDuration returns the timeout as a time.Duration, or the default if not set.
func (d *DuplexConfig) GetTimeoutDuration(defaultTimeout time.Duration) time.Duration {
	if d == nil || d.Timeout == "" {
		return defaultTimeout
	}
	duration, err := time.ParseDuration(d.Timeout)
	if err != nil {
		return defaultTimeout
	}
	return duration
}

// GetResilience returns the resilience config, or nil if not set.
func (d *DuplexConfig) GetResilience() *DuplexResilienceConfig {
	if d == nil {
		return nil
	}
	return d.Resilience
}

// GetMaxRetries returns the configured max retries or the default.
func (r *DuplexResilienceConfig) GetMaxRetries(defaultVal int) int {
	if r == nil || r.MaxRetries <= 0 {
		return defaultVal
	}
	return r.MaxRetries
}

// GetRetryDelayMs returns the configured retry delay or the default.
func (r *DuplexResilienceConfig) GetRetryDelayMs(defaultVal int) int {
	if r == nil || r.RetryDelayMs <= 0 {
		return defaultVal
	}
	return r.RetryDelayMs
}

// GetInterTurnDelayMs returns the configured inter-turn delay or the default.
func (r *DuplexResilienceConfig) GetInterTurnDelayMs(defaultVal int) int {
	if r == nil || r.InterTurnDelayMs <= 0 {
		return defaultVal
	}
	return r.InterTurnDelayMs
}

// GetSelfplayInterTurnDelayMs returns the configured selfplay inter-turn delay or the default.
func (r *DuplexResilienceConfig) GetSelfplayInterTurnDelayMs(defaultVal int) int {
	if r == nil || r.SelfplayInterTurnDelayMs <= 0 {
		return defaultVal
	}
	return r.SelfplayInterTurnDelayMs
}

// GetPartialSuccessMinTurns returns the configured partial success threshold or the default.
func (r *DuplexResilienceConfig) GetPartialSuccessMinTurns(defaultVal int) int {
	if r == nil || r.PartialSuccessMinTurns <= 0 {
		return defaultVal
	}
	return r.PartialSuccessMinTurns
}

// ShouldIgnoreLastTurnSessionEnd returns whether to ignore session end on the last turn.
func (r *DuplexResilienceConfig) ShouldIgnoreLastTurnSessionEnd(defaultVal bool) bool {
	if r == nil || r.IgnoreLastTurnSessionEnd == nil {
		return defaultVal
	}
	return *r.IgnoreLastTurnSessionEnd
}

// Validate validates the TurnDetectionConfig settings.
func (t *TurnDetectionConfig) Validate() error {
	if t == nil {
		return nil
	}

	// Validate mode if provided
	if t.Mode != "" && t.Mode != TurnDetectionModeVAD && t.Mode != TurnDetectionModeASM {
		return fmt.Errorf("invalid turn detection mode %q: must be %q or %q",
			t.Mode, TurnDetectionModeVAD, TurnDetectionModeASM)
	}

	// Validate VAD config if mode is vad
	if t.Mode == TurnDetectionModeVAD && t.VAD != nil {
		if err := t.VAD.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates the VADConfig settings.
func (v *VADConfig) Validate() error {
	if v == nil {
		return nil
	}

	if v.SilenceThresholdMs < 0 {
		return fmt.Errorf("silence_threshold_ms must be non-negative, got %d", v.SilenceThresholdMs)
	}
	if v.MinSpeechMs < 0 {
		return fmt.Errorf("min_speech_ms must be non-negative, got %d", v.MinSpeechMs)
	}
	if v.MaxTurnDurationS < 0 {
		return fmt.Errorf("max_turn_duration_s must be non-negative, got %d", v.MaxTurnDurationS)
	}

	return nil
}

// Validate validates the TTSConfig settings.
func (t *TTSConfig) Validate() error {
	if t == nil {
		return nil
	}

	if t.Provider == "" {
		return fmt.Errorf("tts provider is required")
	}
	// Voice is optional for mock provider when audio_files are specified
	if t.Voice == "" && t.Provider != "mock" {
		return fmt.Errorf("tts voice is required")
	}

	return nil
}

// TurnDefinition represents a single conversation turn definition
type TurnDefinition struct {
	Role    string `json:"role" yaml:"role"` // "user", "assistant", or provider selector like "claude-user" (only for self-play turns)
	Content string `json:"content,omitempty" yaml:"content,omitempty"`

	// Multimodal content parts (text, images, audio, video)
	// If Parts is non-empty, it takes precedence over Content.
	Parts []TurnContentPart `json:"parts,omitempty" yaml:"parts,omitempty"`

	// Self-play specific fields (when role is a provider selector like "claude-user")
	Persona       string  `json:"persona,omitempty" yaml:"persona,omitempty"`               // Persona ID for self-play
	Turns         int     `json:"turns,omitempty" yaml:"turns,omitempty"`                   // Number of user messages to generate
	AssistantTemp float32 `json:"assistant_temp,omitempty" yaml:"assistant_temp,omitempty"` // Override assistant temperature
	UserTemp      float32 `json:"user_temp,omitempty" yaml:"user_temp,omitempty"`           // Override user temperature
	Seed          int     `json:"seed,omitempty" yaml:"seed,omitempty"`                     // Override seed

	// TTS configures text-to-speech for self-play audio generation in duplex mode.
	// When set, self-play generates audio responses instead of text.
	TTS *TTSConfig `json:"tts,omitempty" yaml:"tts,omitempty"`

	// Streaming control - if nil, uses scenario-level streaming setting
	Streaming *bool `json:"streaming,omitempty" yaml:"streaming,omitempty"` // Override streaming for this turn

	// Turn-level assertions (for testing only)
	Assertions []asrt.AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty"`
}

// TurnContentPart represents a content part in a scenario turn (simplified for YAML configuration)
type TurnContentPart struct {
	Type string `json:"type" yaml:"type"`                     // "text", "image", "audio", "video"
	Text string `json:"text,omitempty" yaml:"text,omitempty"` // Text content (for type=text)

	// For media content
	Media *TurnMediaContent `json:"media,omitempty" yaml:"media,omitempty"`
}

// TurnMediaContent represents media content in a turn definition
type TurnMediaContent struct {
	FilePath         string `json:"file_path,omitempty" yaml:"file_path,omitempty"`                 // Relative path to media file (resolved at test time)
	URL              string `json:"url,omitempty" yaml:"url,omitempty"`                             // External URL (http/https)
	Data             string `json:"data,omitempty" yaml:"data,omitempty"`                           // Base64-encoded data (for inline media)
	StorageReference string `json:"storage_reference,omitempty" yaml:"storage_reference,omitempty"` // Storage backend reference (for externalized media)
	MIMEType         string `json:"mime_type" yaml:"mime_type"`                                     // MIME type (e.g., "image/jpeg")
	Detail           string `json:"detail,omitempty" yaml:"detail,omitempty"`                       // Detail level for images: "low", "high", "auto"
	Caption          string `json:"caption,omitempty" yaml:"caption,omitempty"`                     // Optional caption/description
}

// ProviderConfig represents a Provider configuration in K8s-style manifest format
type ProviderConfig struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Provider   `yaml:"spec"`
}

// ProviderConfigK8s is the K8s-compatible version for unmarshaling
type ProviderConfigK8s struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Provider          `yaml:"spec"`
}

// Provider defines API connection and defaults
type Provider struct {
	ID               string                 `json:"id" yaml:"id"`
	Type             string                 `json:"type" yaml:"type"`
	Model            string                 `json:"model" yaml:"model"`
	BaseURL          string                 `json:"base_url,omitempty" yaml:"base_url,omitempty"`
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
}

// CredentialConfig defines how to obtain credentials for a provider.
// Resolution order: api_key → credential_file → credential_env → default env vars.
type CredentialConfig struct {
	// APIKey is an explicit API key value (not recommended for production).
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	// CredentialFile is a path to a file containing the API key.
	CredentialFile string `json:"credential_file,omitempty" yaml:"credential_file,omitempty"`
	// CredentialEnv is the name of an environment variable containing the API key.
	CredentialEnv string `json:"credential_env,omitempty" yaml:"credential_env,omitempty"`
}

// PlatformConfig defines platform-specific settings for hyperscaler hosting.
// Platforms are hosting layers (bedrock, vertex, azure) that determine auth and endpoints,
// while provider type determines message/response handling.
type PlatformConfig struct {
	// Type is the platform type: "bedrock", "vertex", or "azure".
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Region is the cloud region (e.g., "us-west-2", "us-central1").
	Region string `json:"region,omitempty" yaml:"region,omitempty"`
	// Project is the cloud project ID (required for Vertex).
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
	// Endpoint is an optional custom endpoint URL.
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// AdditionalConfig holds platform-specific settings.
	AdditionalConfig map[string]interface{} `json:"additional_config,omitempty" yaml:"additional_config,omitempty"`
}

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
}

// PromptConfigSchema represents a PromptConfig in K8s-style manifest format for schema generation
type PromptConfigSchema struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   ObjectMeta  `yaml:"metadata,omitempty"`
	Spec       prompt.Spec `yaml:"spec"`
}

// ToolConfigSchema represents a Tool configuration for schema generation with simplified ObjectMeta
type ToolConfigSchema struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       ToolSpec   `yaml:"spec"`
}

// ToolSpec represents a tool descriptor (re-exported from runtime/tools for schema generation)
type ToolSpec struct {
	Name         string      `json:"name" yaml:"name"`
	Description  string      `json:"description" yaml:"description"`
	InputSchema  interface{} `json:"input_schema" yaml:"input_schema"`   // JSON Schema Draft-07
	OutputSchema interface{} `json:"output_schema" yaml:"output_schema"` // JSON Schema Draft-07
	Mode         string      `json:"mode" yaml:"mode"`                   // "mock" | "live"
	TimeoutMs    int         `json:"timeout_ms" yaml:"timeout_ms"`
	MockResult   interface{} `json:"mock_result,omitempty" yaml:"mock_result,omitempty"`     // Static mock data
	MockTemplate string      `json:"mock_template,omitempty" yaml:"mock_template,omitempty"` // Template for dynamic mocks
	HTTPConfig   *HTTPConfig `json:"http,omitempty" yaml:"http,omitempty"`                   // Live HTTP configuration
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

// PersonaConfigSchema represents a Persona configuration for schema generation with simplified ObjectMeta
type PersonaConfigSchema struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   ObjectMeta      `yaml:"metadata,omitempty"`
	Spec       UserPersonaPack `yaml:"spec"`
}

// EvalConfig represents an Eval (saved conversation evaluation) configuration
// in K8s-style manifest format
type EvalConfig struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Eval       `yaml:"spec"`
}

// EvalConfigK8s represents the Eval configuration using full K8s ObjectMeta for unmarshaling
type EvalConfigK8s struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       Eval              `yaml:"spec"`
}

// Eval describes an evaluation configuration for replaying and validating saved conversations
type Eval struct {
	// ID uniquely identifies this evaluation configuration
	ID string `json:"id" yaml:"id" jsonschema:"required,description=Unique identifier for this evaluation"`
	// Description provides a human-readable explanation of what this eval tests
	Description string `json:"description" yaml:"description" jsonschema:"description=Human-readable description"`
	// Recording specifies the path to the saved conversation to evaluate
	Recording RecordingSource `json:"recording" yaml:"recording" jsonschema:"required,description=Recording source"`
	// Turns configures turn-level assertion evaluation (array format matching Scenario structure)
	Turns []EvalTurnConfig `json:"turns,omitempty" yaml:"turns,omitempty" jsonschema:"description=Turn-level assertions"`
	// ConversationAssertions are evaluated on the entire conversation after replay
	ConversationAssertions []asrt.AssertionConfig `json:"conversation_assertions,omitempty" yaml:"conversation_assertions,omitempty" jsonschema:"description=Conversation-level assertions"` //nolint:lll
	// Tags for categorizing and filtering evaluations
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" jsonschema:"description=Tags for categorization"`
	// Mode controls replay behavior: "instant", "realtime", or "accelerated"
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty" jsonschema:"enum=instant,enum=realtime,enum=accelerated,description=Replay timing mode (instant, realtime, or accelerated)"` //nolint:lll
	// Speed controls playback speed when Mode is "accelerated" (default: 1.0)
	Speed float64 `json:"speed,omitempty" yaml:"speed,omitempty" jsonschema:"description=Playback speed (default 1.0)"`
}

// EvalTurnConfig configures turn-level assertions for evaluations
// Uses a structure parallel to Scenario turns but with all_turns instead of role
type EvalTurnConfig struct {
	// AllTurns contains assertions to apply to all assistant turns
	AllTurns *EvalAllTurnsConfig `json:"all_turns,omitempty" yaml:"all_turns,omitempty" jsonschema:"description=Assertions to apply to all assistant messages"` //nolint:lll
}

// EvalAllTurnsConfig contains the assertions for all turns
type EvalAllTurnsConfig struct {
	Assertions []asrt.AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty" jsonschema:"description=Assertions to apply to each assistant message"` //nolint:lll
}

// RecordingSource specifies where to load the saved conversation from
type RecordingSource struct {
	// Path to the recording file (relative to eval config file or absolute)
	Path string `json:"path" yaml:"path" jsonschema:"required,description=Path to recording file"`
	// Type hints the recording format: "session", "arena_output", "transcript", or "generic"
	// If omitted, format is auto-detected from file extension
	Type string `json:"type,omitempty" yaml:"type,omitempty" jsonschema:"enum=session,enum=arena_output,enum=transcript,enum=generic,description=Recording format type hint (auto-detected if omitted)"` //nolint:lll
}
