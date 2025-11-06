package config

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PromptConfigRef references a prompt builder configuration
type PromptConfigRef struct {
	ID   string `yaml:"id"`
	File string `yaml:"file"`
}

// ArenaConfig represents the main Arena configuration in K8s-style manifest format
type ArenaConfig struct {
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
	Scenarios     []ScenarioRef     `yaml:"scenarios"`
	Tools         []ToolRef         `yaml:"tools,omitempty"`
	MCPServers    []MCPServerConfig `yaml:"mcp_servers,omitempty"`
	StateStore    *StateStoreConfig `yaml:"state_store,omitempty"`
	Defaults      Defaults          `yaml:"defaults"`
	SelfPlay      *SelfPlayConfig   `yaml:"self_play,omitempty"`

	// Loaded resources (populated by LoadConfig, not serialized)
	LoadedPromptConfigs map[string]*PromptConfigData `yaml:"-" json:"-"` // taskType -> config
	LoadedProviders     map[string]*Provider         `yaml:"-" json:"-"` // provider ID -> provider
	LoadedScenarios     map[string]*Scenario         `yaml:"-" json:"-"` // scenario ID -> scenario
	LoadedTools         []ToolData                   `yaml:"-" json:"-"` // list of tool data
	LoadedPersonas      map[string]*UserPersonaPack  `yaml:"-" json:"-"` // persona ID -> persona

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

// PromptConfigData holds a loaded prompt configuration with its file path
type PromptConfigData struct {
	FilePath string      // relative to ConfigDir
	Config   interface{} // parsed prompt configuration (*prompt.PromptConfig at runtime)
	TaskType string      // extracted from Config.Spec.TaskType
}

// ToolData holds raw tool configuration data
type ToolData struct {
	FilePath string
	Data     []byte
}

// ProviderRef references a provider configuration file
type ProviderRef struct {
	File string `yaml:"file"`
}

// ScenarioRef references a scenario file
type ScenarioRef struct {
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
	Temperature float32      `yaml:"temperature"`
	MaxTokens   int          `yaml:"max_tokens"`
	Seed        int          `yaml:"seed"`
	Concurrency int          `yaml:"concurrency"`
	Output      OutputConfig `yaml:"output"`
	// ConfigDir is the base directory for all config files (prompts, providers, scenarios, tools).
	// If not set, defaults to the directory containing the main config file.
	// If the main config file path is not known, defaults to current working directory.
	ConfigDir string   `yaml:"config_dir"`
	FailOn    []string `yaml:"fail_on"`
	Verbose   bool     `yaml:"verbose"`

	// Deprecated fields for backward compatibility (will be removed)
	HTMLReport     string          `yaml:"html_report,omitempty"`
	OutDir         string          `yaml:"out_dir,omitempty"`
	OutputFormats  []string        `yaml:"output_formats,omitempty"`
	MarkdownConfig *MarkdownConfig `yaml:"markdown_config,omitempty"`
}

// OutputConfig contains configuration for all output formats
type OutputConfig struct {
	Dir      string                `yaml:"dir"`                // Base output directory
	Formats  []string              `yaml:"formats"`            // List of enabled formats: json, html, markdown, junit
	JSON     *JSONOutputConfig     `yaml:"json,omitempty"`     // JSON-specific configuration
	HTML     *HTMLOutputConfig     `yaml:"html,omitempty"`     // HTML-specific configuration
	Markdown *MarkdownOutputConfig `yaml:"markdown,omitempty"` // Markdown-specific configuration
	JUnit    *JUnitOutputConfig    `yaml:"junit,omitempty"`    // JUnit-specific configuration
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
	Providers       []string               `json:"providers,omitempty" yaml:"providers,omitempty"`           // Optional: override which providers to test. If empty, uses all arena providers.
	Streaming       bool                   `json:"streaming,omitempty" yaml:"streaming,omitempty"`           // Enable streaming for all turns by default
	ContextPolicy   *ContextPolicy         `json:"context_policy,omitempty" yaml:"context_policy,omitempty"` // Context management for long conversations
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

// ContextPolicy defines context management for a scenario
type ContextPolicy struct {
	TokenBudget      int    `json:"token_budget,omitempty" yaml:"token_budget,omitempty"`             // Max tokens (0 = unlimited, default)
	ReserveForOutput int    `json:"reserve_for_output,omitempty" yaml:"reserve_for_output,omitempty"` // Reserve for response (default 4000)
	Strategy         string `json:"strategy,omitempty" yaml:"strategy,omitempty"`                     // "oldest", "summarize", "relevance", "fail"
	CacheBreakpoints bool   `json:"cache_breakpoints,omitempty" yaml:"cache_breakpoints,omitempty"`   // Enable Anthropic caching
}

// ToolPolicy defines constraints for tool usage in scenarios
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice" yaml:"tool_choice"` // "auto" | "required" | "none"
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn" yaml:"max_tool_calls_per_turn"`
	MaxTotalToolCalls   int      `json:"max_total_tool_calls" yaml:"max_total_tool_calls"`
	Blocklist           []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
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

	// Streaming control - if nil, uses scenario-level streaming setting
	Streaming *bool `json:"streaming,omitempty" yaml:"streaming,omitempty"` // Override streaming for this turn

	// Turn-level assertions (for testing only)
	Assertions []assertions.AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty"`
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
	FilePath string `json:"file_path,omitempty" yaml:"file_path,omitempty"` // Relative path to media file (resolved at test time)
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`             // External URL (http/https)
	Data     string `json:"data,omitempty" yaml:"data,omitempty"`           // Base64-encoded data (for inline media)
	MIMEType string `json:"mime_type" yaml:"mime_type"`                     // MIME type (e.g., "image/jpeg")
	Detail   string `json:"detail,omitempty" yaml:"detail,omitempty"`       // Detail level for images: "low", "high", "auto"
	Caption  string `json:"caption,omitempty" yaml:"caption,omitempty"`     // Optional caption/description
}

// ProviderConfig represents a Provider in K8s-style manifest format
type ProviderConfig struct {
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
	BaseURL          string                 `json:"base_url" yaml:"base_url"`
	RateLimit        RateLimit              `json:"rate_limit" yaml:"rate_limit"`
	Defaults         ProviderDefaults       `json:"defaults" yaml:"defaults"`
	Pricing          Pricing                `json:"pricing" yaml:"pricing"`
	PricingCorrectAt string                 `json:"pricing_correct_at" yaml:"pricing_correct_at"`
	IncludeRawOutput bool                   `json:"include_raw_output" yaml:"include_raw_output"`                   // Include raw API requests in output for debugging
	AdditionalConfig map[string]interface{} `json:"additional_config,omitempty" yaml:"additional_config,omitempty"` // Additional provider-specific configuration
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
