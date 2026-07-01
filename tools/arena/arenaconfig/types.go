package arenaconfig

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// AssertionConfig represents an assertion configuration used in arena scenarios.
// This type is defined here (rather than in tools/arena/assertions) to avoid
// an inverted dependency from the shared pkg/config package to the arena tool.
type AssertionConfig struct {
	Type string `json:"type" yaml:"type"`
	// Params is optional: many assertion types take no parameters, so an
	// entry with only `type:` is valid and must not require an empty
	// `params: {}` stub. omitempty also drops it from the schema's required set.
	Params  map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
	Message string                 `json:"message,omitempty" yaml:"message,omitempty"`
	When    *AssertionWhen         `json:"when,omitempty" yaml:"when,omitempty"`
	// PassThreshold is the minimum pass rate (0.0-1.0) required across trials.
	// Only meaningful when the parent scenario has Trials > 1.
	// If unset, defaults to 1.0 (must pass every trial).
	PassThreshold *float64 `json:"pass_threshold,omitempty" yaml:"pass_threshold,omitempty"`
}

// Globals holds arena-level cross-cutting config that applies to every
// scenario in addition to its own definitions. Distinct from Defaults
// (which are "values when unspecified") — Globals is for entries that
// always merge in additively.
type Globals struct {
	// ConversationAssertions are appended to every scenario's own
	// conversation_assertions list at evaluation time. Useful for the
	// "DRY across scenarios" case (e.g. capture a metric on every run).
	// For pass/fail gates that must apply to all scenarios — though if
	// a gate truly belongs everywhere, an eval (in pack_evals) is the
	// more honest expression.
	ConversationAssertions []AssertionConfig `yaml:"conversation_assertions,omitempty" json:"conversation_assertions,omitempty"` //nolint:lll
}

// AssertionWhen specifies preconditions that must be met for an assertion to run.
// If any condition is not met, the assertion is skipped (not failed).
type AssertionWhen struct {
	// ToolCalled requires an exact tool name to have been called.
	ToolCalled string `json:"tool_called,omitempty" yaml:"tool_called,omitempty"`
	// ToolCalledPattern is a regex that must match at least one tool name.
	ToolCalledPattern string `json:"tool_called_pattern,omitempty" yaml:"tool_called_pattern,omitempty"`
	// AnyToolCalled requires at least one tool to have been called.
	AnyToolCalled bool `json:"any_tool_called,omitempty" yaml:"any_tool_called,omitempty"`
	// MinToolCalls is the minimum number of tool calls required.
	MinToolCalls int `json:"min_tool_calls,omitempty" yaml:"min_tool_calls,omitempty"`
}

// PromptConfigRef references a prompt builder configuration
type PromptConfigRef struct {
	ID   string            `yaml:"id" json:"id"`
	File string            `yaml:"file" json:"file"`
	Vars map[string]string `yaml:"vars,omitempty" json:"vars,omitempty"`
}

// ArenaConfig represents the main Arena configuration in K8s-style manifest format
type ArenaConfig struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Config            `yaml:"spec" json:"spec"`
}

// ArenaConfigK8s represents the Arena configuration using full K8s ObjectMeta for unmarshaling
// This is used internally for compatibility with k8s.io types
type ArenaConfigK8s struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Config            `yaml:"spec" json:"spec"`
}

// Config represents the main configuration structure
type Config struct {
	// File references for YAML serialization
	PromptConfigs []PromptConfigRef          `yaml:"prompt_configs,omitempty" json:"prompt_configs,omitempty"`
	Providers     []ProviderRef              `yaml:"providers" json:"providers"`
	Judges        []JudgeRef                 `yaml:"judges,omitempty" json:"judges,omitempty"`
	JudgeDefaults *JudgeDefaults             `yaml:"judge_defaults,omitempty" json:"judge_defaults,omitempty"`
	Scenarios     []ScenarioRef              `yaml:"scenarios,omitempty" json:"scenarios,omitempty"`
	Evals         []EvalRef                  `yaml:"evals,omitempty" json:"evals,omitempty"`
	Tools         []ToolRef                  `yaml:"tools,omitempty" json:"tools,omitempty"`
	Skills        []prompt.SkillSourceConfig `yaml:"skills,omitempty" json:"skills,omitempty"`
	PackEvals     []evals.EvalDef            `yaml:"pack_evals,omitempty" json:"pack_evals,omitempty"`
	// Globals holds arena-level cross-cutting config that applies to
	// every scenario in addition to its own definitions. Distinct from
	// Defaults (which are "values when unspecified") — Globals is for
	// "always-additive" entries.
	Globals *Globals `yaml:"globals,omitempty" json:"globals,omitempty"`
	// Runtime carries a runtime configuration spec passed straight through to the
	// runtime layer (hooks, sandboxes, …). Arena wraps the runtime, so anything
	// the runtime config supports is available here under `runtime:` without
	// Arena needing a bespoke field for each.
	Runtime      *config.RuntimeConfigSpec `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Workflow     interface{}               `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Compositions interface{}               `yaml:"compositions,omitempty" json:"compositions,omitempty"`
	Memory       interface{}               `yaml:"memory,omitempty" json:"memory,omitempty"`
	Agents       interface{}               `yaml:"agents,omitempty" json:"agents,omitempty"`
	Deploy       *DeployConfig             `yaml:"deploy,omitempty" json:"deploy,omitempty"`
	MCPServers   []config.MCPServerConfig  `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
	A2AAgents    []A2AAgentConfig          `yaml:"a2a_agents,omitempty" json:"a2a_agents,omitempty"`
	StateStore   *config.StateStoreConfig  `yaml:"state_store,omitempty" json:"state_store,omitempty"`
	Defaults     Defaults                  `yaml:"defaults" json:"defaults"`
	SelfPlay     *SelfPlayConfig           `yaml:"self_play,omitempty" json:"self_play,omitempty"`
	PackFile     string                    `yaml:"pack_file,omitempty" json:"pack_file,omitempty"`

	// Inline resource specs (alternative to file refs, merged into LoadedX during load)
	ProviderSpecs map[string]*config.Provider `yaml:"provider_specs,omitempty" json:"provider_specs,omitempty"`
	ScenarioSpecs map[string]*Scenario        `yaml:"scenario_specs,omitempty" json:"scenario_specs,omitempty"`
	EvalSpecs     map[string]*Eval            `yaml:"eval_specs,omitempty" json:"eval_specs,omitempty"`
	ToolSpecs     map[string]*config.ToolSpec `yaml:"tool_specs,omitempty" json:"tool_specs,omitempty"`
	JudgeSpecs    map[string]*JudgeSpec       `yaml:"judge_specs,omitempty" json:"judge_specs,omitempty"`
	PromptSpecs   map[string]*prompt.Spec     `yaml:"prompt_specs,omitempty" json:"prompt_specs,omitempty"`

	// ProviderGroups maps provider ID to configured group (populated during load)
	ProviderGroups map[string]string `yaml:"-" json:"provider_groups,omitempty"`
	// ProviderCapabilities maps provider ID to its capabilities (populated during load)
	ProviderCapabilities map[string][]string `yaml:"-" json:"provider_capabilities,omitempty"`

	// TTSProviders / STTProviders / EmbeddingProviders / ImageProviders are
	// the legacy role-specific slots. They still load correctly — every
	// entry's `role:` is validated against the slot — but the preferred
	// shape is a single unified `providers:` list where the loader routes
	// each provider into the right Loaded* map based on its `role:` value.
	// Mixing the legacy slots and the unified list is supported during
	// migration; both populate the same Loaded* maps.
	TTSProviders       []ProviderRef `yaml:"tts_providers,omitempty" json:"tts_providers,omitempty"`
	STTProviders       []ProviderRef `yaml:"stt_providers,omitempty" json:"stt_providers,omitempty"`
	EmbeddingProviders []ProviderRef `yaml:"embedding_providers,omitempty" json:"embedding_providers,omitempty"`
	ImageProviders     []ProviderRef `yaml:"image_providers,omitempty" json:"image_providers,omitempty"`

	// Voices binds voice IDs to loaded TTS provider IDs. Personas reference
	// voice IDs (not provider IDs) so the same persona can run against a
	// real Cartesia voice in recording mode and a mock TTS provider in CI
	// just by editing this list.
	Voices []config.VoiceBinding `yaml:"voices,omitempty" json:"voices,omitempty"`

	// Loaded resources (populated by LoadConfig, included in JSON for adapter consumption)
	LoadedPromptConfigs      map[string]*PromptConfigData `yaml:"-" json:"loaded_prompt_configs,omitempty"`
	LoadedProviders          map[string]*config.Provider  `yaml:"-" json:"loaded_providers,omitempty"`
	LoadedTTSProviders       map[string]*config.Provider  `yaml:"-" json:"loaded_tts_providers,omitempty"`
	LoadedSTTProviders       map[string]*config.Provider  `yaml:"-" json:"loaded_stt_providers,omitempty"`
	LoadedEmbeddingProviders map[string]*config.Provider  `yaml:"-" json:"loaded_embedding_providers,omitempty"`
	LoadedImageProviders     map[string]*config.Provider  `yaml:"-" json:"loaded_image_providers,omitempty"`
	LoadedInferenceProviders map[string]*config.Provider  `yaml:"-" json:"loaded_inference_providers,omitempty"`
	LoadedJudges             map[string]*JudgeTarget      `yaml:"-" json:"loaded_judges,omitempty"`
	LoadedScenarios          map[string]*Scenario         `yaml:"-" json:"loaded_scenarios,omitempty"`
	LoadedEvals              map[string]*Eval             `yaml:"-" json:"loaded_evals,omitempty"`
	LoadedTools              []config.ToolData            `yaml:"-" json:"loaded_tools,omitempty"`
	LoadedSkillSources       []prompt.SkillSourceConfig   `yaml:"-" json:"loaded_skill_sources,omitempty"`
	LoadedPersonas           map[string]*UserPersonaPack  `yaml:"-" json:"loaded_personas,omitempty"`
	LoadedPack               *prompt.Pack                 `yaml:"-" json:"loaded_pack,omitempty"`

	// Pack eval settings (set by CLI flags, not serialized)
	SkipPackEvals  bool     `yaml:"-" json:"-"` // Disable pack eval execution
	EvalTypeFilter []string `yaml:"-" json:"-"` // Filter to specific eval types

	// ValidationWarnings holds non-fatal warnings from config validation.
	// Populated by LoadConfig; callers may inspect or log these.
	ValidationWarnings []string `yaml:"-" json:"-"`

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
func (c *Config) GetStateStoreConfig() *config.StateStoreConfig {
	if c.StateStore == nil {
		return &config.StateStoreConfig{
			Type: defaultStateStoreType,
		}
	}
	// Set default type if not specified
	if c.StateStore.Type == "" {
		c.StateStore.Type = defaultStateStoreType
	}
	return c.StateStore
}

// A2AAgentConfig defines an A2A agent for Arena testing.
// When URL is set the agent is treated as a remote A2A server;
// otherwise a mock server is started from Card + Responses.
type A2AAgentConfig struct {
	Name      string            `yaml:"name" json:"name"`
	Card      A2ACardConfig     `yaml:"card,omitempty" json:"card,omitempty"`
	Responses []A2AResponseRule `yaml:"responses,omitempty" json:"responses,omitempty"`

	// URL of a remote A2A server. When set, Card and Responses are ignored
	// and the agent is discovered from the remote /.well-known/agent.json.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`

	// Auth configures authentication for remote A2A servers.
	Auth *A2AAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`

	// Headers are static headers sent on every request to the remote server.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// HeadersFromEnv injects headers from environment variables.
	// Format: "Header-Name=ENV_VAR_NAME".
	HeadersFromEnv []string `yaml:"headers_from_env,omitempty" json:"headers_from_env,omitempty"`

	// TimeoutMs sets the per-request timeout in milliseconds.
	TimeoutMs int `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`

	// SkillFilter controls which skills from this agent are exposed.
	SkillFilter *A2ASkillFilter `yaml:"skill_filter,omitempty" json:"skill_filter,omitempty"`
}

// A2AAuthConfig configures authentication for a remote A2A agent.
type A2AAuthConfig struct {
	// Scheme is the auth scheme (e.g. "Bearer").
	Scheme string `yaml:"scheme" json:"scheme"`
	// Token is the literal token value.
	Token string `yaml:"token,omitempty" json:"token,omitempty"`
	// TokenEnv is the name of an environment variable containing the token.
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
}

// A2ASkillFilter controls which skills from an A2A agent are exposed.
type A2ASkillFilter struct {
	Allowlist []string `yaml:"allowlist,omitempty" json:"allowlist,omitempty"`
	Blocklist []string `yaml:"blocklist,omitempty" json:"blocklist,omitempty"`
}

// A2ACardConfig defines the agent card for a mock A2A agent.
type A2ACardConfig struct {
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description" json:"description"`
	Skills      []A2ASkillConfig `yaml:"skills" json:"skills"`
}

// A2ASkillConfig defines a single skill in a mock A2A agent card.
type A2ASkillConfig struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// A2AResponseRule defines a response rule for a mock A2A agent.
type A2AResponseRule struct {
	Skill    string             `yaml:"skill" json:"skill"`
	Match    *A2AMatchConfig    `yaml:"match,omitempty" json:"match,omitempty"`
	Response *A2AResponseConfig `yaml:"response,omitempty" json:"response,omitempty"`
	Error    string             `yaml:"error,omitempty" json:"error,omitempty"`
}

// A2AMatchConfig defines how to match incoming messages for a mock A2A agent.
type A2AMatchConfig struct {
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	Regex    string `yaml:"regex,omitempty" json:"regex,omitempty"`
}

// A2AResponseConfig defines the response parts for a mock A2A agent.
type A2AResponseConfig struct {
	Parts []A2APartConfig `yaml:"parts" json:"parts"`
}

// A2APartConfig defines a single response part for a mock A2A agent.
type A2APartConfig struct {
	Text string `yaml:"text" json:"text"`
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
	FilePath string            `json:"file_path,omitempty"` // relative to ConfigDir
	Config   interface{}       `json:"config,omitempty"`    // parsed prompt configuration (*prompt.Config at runtime)
	TaskType string            `json:"task_type,omitempty"` // extracted from Config.Spec.TaskType
	Vars     map[string]string `json:"vars,omitempty"`      // Variable overrides from arena.yaml
}

// ProviderRef references a provider configuration file
type ProviderRef struct {
	File  string `yaml:"file" json:"file"`
	Group string `yaml:"group,omitempty" json:"group,omitempty"`
}

// JudgeRef references a judge configuration mapped to a provider.
// Mirrors self-play role/provider mapping to allow multiple judge targets.
// The judge inherits its model from the referenced provider.
type JudgeRef struct {
	Name     string `yaml:"name" json:"name"`         // Judge identifier used in assertions
	Provider string `yaml:"provider" json:"provider"` // Provider ID reference (must exist in spec.providers)
}

// JudgeSpec defines an inline judge configuration. The judge inherits its
// model from the referenced provider.
type JudgeSpec struct {
	Provider string `yaml:"provider" json:"provider"` // Provider ID reference
}

// JudgeTarget is a resolved judge reference with its provider config.
// The effective model is the provider's model (Provider.Model).
type JudgeTarget struct {
	Name     string           `json:"name"`               // Judge identifier
	Provider *config.Provider `json:"provider,omitempty"` // Resolved provider config (carries the model)
}

// ScenarioRef references a scenario file
type ScenarioRef struct {
	File string `yaml:"file" json:"file"`
}

// EvalRef references an eval configuration file
type EvalRef struct {
	File string `yaml:"file" json:"file"`
}

// ToolRef references a tool configuration file
type ToolRef struct {
	File string `yaml:"file" json:"file"`
}

// SelfPlayConfig configures self-play functionality.
// Self-play is enabled by default when this section is present.
type SelfPlayConfig struct {
	// Deprecated: Enabled is redundant — self-play is enabled whenever this section
	// is present. The field is retained for backward compatibility and will be removed
	// in a future release. To disable self-play, remove the self_play section entirely.
	Enabled      *bool                       `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Personas     []PersonaRef                `yaml:"personas" json:"personas"`
	PersonaSpecs map[string]*UserPersonaPack `yaml:"persona_specs,omitempty" json:"persona_specs,omitempty"`
	Roles        []SelfPlayRoleGroup         `yaml:"roles" json:"roles"`
}

// IsEnabled returns whether self-play is enabled. Defaults to true when the
// Enabled field is not set (nil).
func (c *SelfPlayConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// PersonaRef references a persona file
type PersonaRef struct {
	File string `yaml:"file" json:"file"`
}

// SelfPlayRoleGroup defines user LLM configuration for self-play
type SelfPlayRoleGroup struct {
	ID       string `yaml:"id" json:"id"`
	Provider string `yaml:"provider" json:"provider"` // Provider ID reference (must exist in spec.providers)
}

// Defaults contains default configuration values
type Defaults struct {
	Temperature float32 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	MaxTokens   int     `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Seed        int     `yaml:"seed,omitempty" json:"seed,omitempty"`
	Concurrency int     `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	// RunTimeout is the per-run timeout duration (e.g. "5m", "30s"). Defaults to 5m.
	RunTimeout string       `yaml:"run_timeout,omitempty" json:"run_timeout,omitempty"`
	Output     OutputConfig `yaml:"output,omitempty" json:"output,omitempty"`
	// ConfigDir is the base directory for all config files (prompts, providers, scenarios, tools).
	// If not set, defaults to the directory containing the main config file.
	// If the main config file path is not known, defaults to current working directory.
	ConfigDir string   `yaml:"config_dir,omitempty" json:"config_dir,omitempty"`
	FailOn    []string `yaml:"fail_on,omitempty" json:"fail_on,omitempty"`
	Verbose   bool     `yaml:"verbose,omitempty" json:"verbose,omitempty"`

	// Monitor groups runtime-monitor configuration (audio, future surfaces).
	Monitor *MonitorSection `yaml:"monitor,omitempty" json:"monitor,omitempty"`

	// Inference selects which configured inference client (under
	// spec.inference) serves each task when an assertion or handler doesn't
	// pin one explicitly. IDs reference entries in cfg.Inference.
	Inference *config.InferenceDefaults `yaml:"inference,omitempty" json:"inference,omitempty"`

	// Deprecated fields for backward compatibility (will be removed)
	HTMLReport     string          `yaml:"html_report,omitempty" json:"html_report,omitempty"`
	OutDir         string          `yaml:"out_dir,omitempty" json:"out_dir,omitempty"`
	OutputFormats  []string        `yaml:"output_formats,omitempty" json:"output_formats,omitempty"`
	MarkdownConfig *MarkdownConfig `yaml:"markdown_config,omitempty" json:"markdown_config,omitempty"`
}

// MonitorSection groups runtime-monitor configuration. Today only audio is
// supported; future surfaces (transcript stream, video tap) can be added here.
type MonitorSection struct {
	Audio *AudioMonitorSection `yaml:"audio,omitempty" json:"audio,omitempty"`
}

// AudioMonitorSection configures real-time audio monitoring for duplex
// scenarios. Values map to tools/arena/audio.Options at engine startup.
type AudioMonitorSection struct {
	// Enabled controls when monitoring is active. One of "auto", "on", "off".
	// Defaults to "auto" when unset.
	Enabled string `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Rate is the canonical sample rate inside the router. Must be one of
	// 16000, 24000, or 48000. Defaults to 24000 when unset.
	Rate int `yaml:"rate,omitempty" json:"rate,omitempty"`
}

// JudgeDefaults configures default judge prompt selection.
type JudgeDefaults struct {
	Prompt         string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	PromptRegistry string `yaml:"prompt_registry,omitempty" json:"prompt_registry,omitempty"`
}

// OutputConfig contains configuration for all output formats
type OutputConfig struct {
	Dir       string                `yaml:"dir" json:"dir,omitempty"`
	Formats   []string              `yaml:"formats" json:"formats,omitempty"`
	JSON      *JSONOutputConfig     `yaml:"json,omitempty" json:"json,omitempty"`
	HTML      *HTMLOutputConfig     `yaml:"html,omitempty" json:"html,omitempty"`
	Markdown  *MarkdownOutputConfig `yaml:"markdown,omitempty" json:"markdown,omitempty"`
	JUnit     *JUnitOutputConfig    `yaml:"junit,omitempty" json:"junit,omitempty"`
	Recording *RecordingConfig      `yaml:"recording,omitempty" json:"recording,omitempty"`
}

// JSONOutputConfig contains configuration options for JSON output
type JSONOutputConfig struct {
	// Future: could add options like pretty printing, compression, etc.
}

// HTMLOutputConfig contains configuration options for HTML output
type HTMLOutputConfig struct {
	File string `yaml:"file,omitempty" json:"file,omitempty"` // Custom HTML output file name
	// Future: could add theme, template, styling options, etc.
}

// MarkdownOutputConfig contains configuration options for markdown output formatting
type MarkdownOutputConfig struct {
	File              string `yaml:"file,omitempty" json:"file,omitempty"`           // Custom markdown output file name
	IncludeDetails    bool   `yaml:"include_details" json:"include_details"`         // Include detailed test information
	ShowOverview      bool   `yaml:"show_overview" json:"show_overview"`             // Show executive overview section
	ShowResultsMatrix bool   `yaml:"show_results_matrix" json:"show_results_matrix"` // Show results matrix table
	ShowFailedTests   bool   `yaml:"show_failed_tests" json:"show_failed_tests"`     // Show failed tests section
	ShowCostSummary   bool   `yaml:"show_cost_summary" json:"show_cost_summary"`     // Show cost analysis section
}

// JUnitOutputConfig contains configuration options for JUnit XML output
type JUnitOutputConfig struct {
	File string `yaml:"file,omitempty" json:"file,omitempty"` // Custom JUnit output file name
	// Future: could add options like test suite naming, etc.
}

// RecordingConfig contains configuration for session recording.
// Session recordings capture the complete event stream for replay and analysis.
type RecordingConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`             // Enable session recording
	Dir     string `yaml:"dir,omitempty" json:"dir,omitempty"` // Subdirectory within output.dir (default: "recordings")
}

// Deprecated: Use MarkdownOutputConfig instead
type MarkdownConfig struct {
	IncludeDetails    bool `yaml:"include_details" json:"include_details"`         // Include detailed test information
	ShowOverview      bool `yaml:"show_overview" json:"show_overview"`             // Show executive overview section
	ShowResultsMatrix bool `yaml:"show_results_matrix" json:"show_results_matrix"` // Show results matrix table
	ShowFailedTests   bool `yaml:"show_failed_tests" json:"show_failed_tests"`     // Show failed tests section
	ShowCostSummary   bool `yaml:"show_cost_summary" json:"show_cost_summary"`     // Show cost analysis section
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
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Scenario          `yaml:"spec" json:"spec"`
}

// ScenarioConfigK8s is the K8s-compatible version for unmarshaling
type ScenarioConfigK8s struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Scenario          `yaml:"spec" json:"spec"`
}

// Scenario describes user turns, context, and validation constraints.
// Workflow scenarios use the same turns format — the workflow state machine
// (defined in config.arena.yaml) dynamically selects the prompt per turn.
type Scenario struct {
	ID          string `json:"id,omitempty" yaml:"id,omitempty"`
	TaskType    string `json:"task_type,omitempty" yaml:"task_type,omitempty"`
	Mode        string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Description string `json:"description" yaml:"description"`
	// Labels are key/value tags copied from the scenario manifest's metadata.labels
	// (K8s-style). Used for stratified reporting in arena. Populated by LoadScenario;
	// not read from the spec body itself.
	Labels          map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	ContextMetadata *ContextMetadata       `json:"context_metadata,omitempty" yaml:"context_metadata,omitempty"`
	Turns           []TurnDefinition       `json:"turns,omitempty" yaml:"turns,omitempty"`
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
	ConversationAssertions []AssertionConfig `json:"conversation_assertions,omitempty" yaml:"conversation_assertions,omitempty"` //nolint:lll
	// Duplex enables bidirectional streaming mode for voice/audio scenarios.
	Duplex *DuplexConfig `json:"duplex,omitempty" yaml:"duplex,omitempty"`

	// Voice references an arena-level voice id (see Config.Voices) used to
	// synthesize this scenario's scripted-text user turns. The duplex executor
	// resolves the voice via Config.ResolveVoice.
	//
	// For selfplay scenarios (turns with role: selfplay-user) the persona owns
	// voice choice; Scenario.Voice is ignored.
	Voice string `yaml:"voice,omitempty" json:"voice,omitempty"`

	// Variables are injected into the pack's template variables.
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty" jsonschema:"description=Template variables to inject into the pack"` //nolint:lll
	// Trials is the number of times to execute this scenario for statistical evaluation.
	// When > 1, assertion results are aggregated across trials using pass rates and flakiness scores.
	Trials int `json:"trials,omitempty" yaml:"trials,omitempty" jsonschema:"description=Number of times to run this scenario for statistical evaluation"` //nolint:lll
	// SeedMemories pre-populates the memory store before the first turn.
	// Uses the same fields as memory__remember: content (required), type, confidence, metadata.
	SeedMemories []SeedMemoryEntry `json:"seed_memories,omitempty" yaml:"seed_memories,omitempty"`
}

// SeedMemoryEntry defines a memory to pre-populate before scenario execution.
// Fields match the memory__remember tool arguments.
type SeedMemoryEntry struct {
	Content    string         `json:"content" yaml:"content"`
	Type       string         `json:"type,omitempty" yaml:"type,omitempty"`
	Confidence float64        `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
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
	const querySourceCustom = "custom"
	validQuerySources := map[string]bool{"": true, "last_user": true, "last_n": true, querySourceCustom: true}
	if !validQuerySources[r.QuerySource] {
		return fmt.Errorf("query_source must be 'last_user', 'last_n', or 'custom', got %q", r.QuerySource)
	}

	// Validate last_n_count when query_source is "last_n"
	if r.QuerySource == "last_n" && r.LastNCount <= 0 {
		return fmt.Errorf("last_n_count must be positive when query_source is 'last_n'")
	}

	// Validate custom_query when query_source is "custom"
	if r.QuerySource == querySourceCustom && r.CustomQuery == "" {
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
	// MaxRounds is the maximum number of tool-call rounds per turn.
	// 0/unset means use the Arena default (50). Never unlimited.
	MaxRounds int `json:"max_rounds,omitempty" yaml:"max_rounds,omitempty"`
	// MaxCostUSD is the maximum cost in USD allowed per turn.
	// 0/unset means use the Arena default ($2.00). Never unlimited.
	MaxCostUSD float64 `json:"max_cost_usd,omitempty" yaml:"max_cost_usd,omitempty"`
	// MaxIdenticalToolCalls is the maximum number of times the same tool may be
	// called with identical arguments before the loop is aborted.
	// 0/unset means use the Arena default (3). Never unlimited.
	MaxIdenticalToolCalls int `json:"max_identical_tool_calls,omitempty" yaml:"max_identical_tool_calls,omitempty"`
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

// TurnDefinition represents a single conversation turn definition
type TurnDefinition struct {
	Role    string `json:"role" yaml:"role"` // "user", "assistant", or provider selector like "claude-user" (only for self-play turns)
	Content string `json:"content,omitempty" yaml:"content,omitempty"`

	// Multimodal content parts (text, images, audio, video)
	// If Parts is non-empty, it takes precedence over Content.
	Parts []TurnContentPart `json:"parts,omitempty" yaml:"parts,omitempty"`

	// Self-play specific fields (when role is a provider selector like "claude-user")
	Persona string `json:"persona,omitempty" yaml:"persona,omitempty"` // Persona ID for self-play
	// Turns is the number of self-play exchanges. Exact count if MaxTurns absent.
	Turns int `json:"turns,omitempty" yaml:"turns,omitempty"`
	// MaxTurns is the upper bound; enables natural termination when > Turns.
	MaxTurns      int     `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`
	AssistantTemp float32 `json:"assistant_temp,omitempty" yaml:"assistant_temp,omitempty"` // Override assistant temperature
	UserTemp      float32 `json:"user_temp,omitempty" yaml:"user_temp,omitempty"`           // Override user temperature
	Seed          int     `json:"seed,omitempty" yaml:"seed,omitempty"`                     // Override seed

	// Streaming control - if nil, uses scenario-level streaming setting
	Streaming *bool `json:"streaming,omitempty" yaml:"streaming,omitempty"` // Override streaming for this turn

	// ConsentOverrides controls consent simulation for client-side tools.
	// Keys are tool names, values are "grant", "deny", or "timeout".
	ConsentOverrides map[string]string `json:"consent_overrides,omitempty" yaml:"consent_overrides,omitempty"`

	// Perturbations define variable substitutions for behavioral testing.
	// Each key is a placeholder name (e.g., "city") and the value is a list of variants.
	// The engine expands each variant into a separate trial, substituting {key} in Content.
	Perturbations map[string][]string `json:"perturbations,omitempty" yaml:"perturbations,omitempty"`

	// Chaos configures fault injection for resilience testing.
	Chaos *ChaosConfig `json:"chaos,omitempty" yaml:"chaos,omitempty"`

	// Turn-level assertions (for testing only)
	Assertions []AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty"`
}

// ChaosConfig defines fault injection rules for a turn.
type ChaosConfig struct {
	// ToolFailures injects failures into specific tool calls.
	ToolFailures []ChaosToolFailure `json:"tool_failures,omitempty" yaml:"tool_failures,omitempty"`
}

// ChaosToolFailure defines a failure injection rule for a specific tool.
type ChaosToolFailure struct {
	// Tool is the name of the tool to inject failures for.
	Tool string `json:"tool" yaml:"tool"`
	// Mode is the type of failure: "error", "timeout", or "slow".
	Mode string `json:"mode" yaml:"mode"`
	// Probability is the chance of failure (0.0 to 1.0, default 1.0).
	Probability float64 `json:"probability,omitempty" yaml:"probability,omitempty"`
	// Message is an optional custom error message.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
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

// PromptConfigSchema represents a PromptConfig in K8s-style manifest format for schema generation
type PromptConfigSchema struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       prompt.Spec       `yaml:"spec" json:"spec"`
}

// ToolConfigSchema represents a Tool configuration for schema generation with simplified ObjectMeta
type ToolConfigSchema struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       config.ToolSpec   `yaml:"spec" json:"spec"`
}

// PersonaConfigSchema represents a Persona configuration for schema generation with simplified ObjectMeta
type PersonaConfigSchema struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       UserPersonaPack   `yaml:"spec" json:"spec"`
}

// EvalConfig represents an Eval (saved conversation evaluation) configuration
// in K8s-style manifest format
type EvalConfig struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   config.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Eval              `yaml:"spec" json:"spec"`
}

// EvalConfigK8s represents the Eval configuration using full K8s ObjectMeta for unmarshaling
type EvalConfigK8s struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Eval              `yaml:"spec" json:"spec"`
}

// Eval describes an evaluation configuration for replaying and validating saved conversations
type Eval struct {
	// ID uniquely identifies this evaluation configuration
	ID string `json:"id,omitempty" yaml:"id,omitempty" jsonschema:"description=Unique identifier for this evaluation"`
	// Description provides a human-readable explanation of what this eval tests
	Description string `json:"description" yaml:"description" jsonschema:"description=Human-readable description"`
	// Recording specifies the path to the saved conversation to evaluate
	Recording RecordingSource `json:"recording" yaml:"recording" jsonschema:"required,description=Recording source"`
	// Turns configures turn-level assertion evaluation (array format matching Scenario structure)
	Turns []EvalTurnConfig `json:"turns,omitempty" yaml:"turns,omitempty" jsonschema:"description=Turn-level assertions"`
	// ConversationAssertions are evaluated on the entire conversation after replay
	ConversationAssertions []AssertionConfig `json:"conversation_assertions,omitempty" yaml:"conversation_assertions,omitempty" jsonschema:"description=Conversation-level assertions"` //nolint:lll
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
	Assertions []AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty" jsonschema:"description=Assertions to apply to each assistant message"` //nolint:lll
}

// RecordingSource specifies where to load the saved conversation from
type RecordingSource struct {
	// Path to the recording file (relative to eval config file or absolute)
	Path string `json:"path" yaml:"path" jsonschema:"required,description=Path to recording file"`
	// Type hints the recording format: "session", "arena_output", "transcript", or "generic"
	// If omitted, format is auto-detected from file extension
	Type string `json:"type,omitempty" yaml:"type,omitempty" jsonschema:"enum=session,enum=arena_output,enum=transcript,enum=generic,description=Recording format type hint (auto-detected if omitted)"` //nolint:lll
}
