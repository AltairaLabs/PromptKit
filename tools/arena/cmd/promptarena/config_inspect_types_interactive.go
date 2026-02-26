package main

// InspectionData contains all collected configuration inspection data
type InspectionData struct {
	ConfigFile               string                `json:"config_file"`
	ArenaName                string                `json:"arena_name,omitempty"`
	PromptConfigs            []PromptInspectData   `json:"prompt_configs"`
	Providers                []ProviderInspectData `json:"providers"`
	Scenarios                []ScenarioInspectData `json:"scenarios"`
	Tools                    []ToolInspectData     `json:"tools,omitempty"`
	Personas                 []PersonaInspectData  `json:"personas,omitempty"`
	Judges                   []JudgeInspectData    `json:"judges,omitempty"`
	SelfPlayRoles            []SelfPlayRoleData    `json:"self_play_roles,omitempty"`
	SelfPlayEnabled          bool                  `json:"self_play_enabled"`
	AvailableTaskTypes       []string              `json:"available_task_types"`
	Defaults                 *DefaultsInspectData  `json:"defaults,omitempty"`
	ValidationPassed         bool                  `json:"validation_passed"`
	ValidationError          string                `json:"validation_error,omitempty"`
	ValidationErrors         []string              `json:"validation_errors,omitempty"`
	ValidationWarnings       int                   `json:"validation_warnings"`
	ValidationWarningDetails []string              `json:"validation_warning_details,omitempty"`
	ValidationChecks         []ValidationCheckData `json:"validation_checks,omitempty"`
	CacheStats               *CacheStatsData       `json:"cache_stats,omitempty"`
}

// ValidationCheckData represents a single validation check result for display
type ValidationCheckData struct {
	Name    string   `json:"name"`
	Passed  bool     `json:"passed"`
	Warning bool     `json:"warning,omitempty"`
	Issues  []string `json:"issues,omitempty"`
}

// PromptInspectData contains detailed prompt configuration info
type PromptInspectData struct {
	ID             string            `json:"id"`
	File           string            `json:"file"`
	TaskType       string            `json:"task_type,omitempty"`
	Version        string            `json:"version,omitempty"`
	Description    string            `json:"description,omitempty"`
	Variables      []string          `json:"variables,omitempty"`
	VariableValues map[string]string `json:"variable_values,omitempty"`
	AllowedTools   []string          `json:"allowed_tools,omitempty"`
	Validators     []string          `json:"validators,omitempty"`
	HasMedia       bool              `json:"has_media,omitempty"`
}

// ProviderInspectData contains detailed provider configuration info
type ProviderInspectData struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Type        string   `json:"type,omitempty"`
	Model       string   `json:"model,omitempty"`
	Group       string   `json:"group,omitempty"`
	Temperature float32  `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	UsedBy      []string `json:"used_by,omitempty"` // What uses this provider (e.g., "judge:quality", "role:user")
}

// ScenarioInspectData contains detailed scenario configuration info
type ScenarioInspectData struct {
	ID                 string   `json:"id"`
	File               string   `json:"file"`
	TaskType           string   `json:"task_type,omitempty"`
	Description        string   `json:"description,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	TurnCount          int      `json:"turn_count"`
	HasSelfPlay        bool     `json:"has_self_play,omitempty"`
	AssertionCount     int      `json:"assertion_count,omitempty"`
	ConvAssertionCount int      `json:"conv_assertion_count,omitempty"`
	Providers          []string `json:"providers,omitempty"`
	Streaming          bool     `json:"streaming,omitempty"`
}

// ToolInspectData contains detailed tool configuration info
type ToolInspectData struct {
	Name          string   `json:"name"`
	File          string   `json:"file"`
	Description   string   `json:"description,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	TimeoutMs     int      `json:"timeout_ms,omitempty"`
	InputParams   []string `json:"input_params,omitempty"`
	HasMockData   bool     `json:"has_mock_data,omitempty"`
	HasHTTPConfig bool     `json:"has_http_config,omitempty"`
}

// PersonaInspectData contains detailed persona configuration info
type PersonaInspectData struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Description string   `json:"description,omitempty"`
	Goals       []string `json:"goals,omitempty"`
	RoleID      string   `json:"role_id,omitempty"`  // Self-play role ID using this persona
	Provider    string   `json:"provider,omitempty"` // Provider assigned to this role
}

// JudgeInspectData contains detailed judge configuration info
type JudgeInspectData struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// SelfPlayRoleData contains detailed self-play role configuration info
type SelfPlayRoleData struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Persona  string `json:"persona,omitempty"` // Persona used by this role (from scenarios)
}

// DefaultsInspectData contains arena defaults
type DefaultsInspectData struct {
	Temperature   float32  `json:"temperature,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Seed          int      `json:"seed,omitempty"`
	Concurrency   int      `json:"concurrency,omitempty"`
	OutputDir     string   `json:"output_dir,omitempty"`
	OutputFormats []string `json:"output_formats,omitempty"`
}

// CacheStatsData contains cache statistics
type CacheStatsData struct {
	PromptCache   CacheInfo `json:"prompt_cache"`
	FragmentCache CacheInfo `json:"fragment_cache"`
	SelfPlayCache CacheInfo `json:"self_play_cache,omitempty"`
}

// CacheInfo contains information about a specific cache
type CacheInfo struct {
	Size    int      `json:"size"`
	Entries []string `json:"entries,omitempty"`
	Hits    int      `json:"hits,omitempty"`
	Misses  int      `json:"misses,omitempty"`
	HitRate float64  `json:"hit_rate,omitempty"`
}

// toolManifestMetadata represents the metadata section of a tool manifest
type toolManifestMetadata struct {
	Name string `yaml:"name"`
}

// toolInputSchema represents the input schema for a tool
type toolInputSchema struct {
	Properties map[string]interface{} `yaml:"properties"`
	Required   []string               `yaml:"required"`
}

// toolManifestSpec represents the spec section of a tool manifest
type toolManifestSpec struct {
	Name         string          `yaml:"name"`
	Description  string          `yaml:"description"`
	Mode         string          `yaml:"mode"`
	TimeoutMs    int             `yaml:"timeout_ms"`
	InputSchema  toolInputSchema `yaml:"input_schema"`
	MockResult   interface{}     `yaml:"mock_result"`
	MockTemplate string          `yaml:"mock_template"`
	HTTP         interface{}     `yaml:"http"`
}

// toolManifest represents the structure of a tool YAML file
type toolManifest struct {
	Metadata toolManifestMetadata `yaml:"metadata"`
	Spec     toolManifestSpec     `yaml:"spec"`
}

// sectionVisibility tracks which sections should be displayed
type sectionVisibility struct {
	all        bool
	prompts    bool
	providers  bool
	scenarios  bool
	tools      bool
	selfplay   bool
	judges     bool
	defaults   bool
	validation bool
}
