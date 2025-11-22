// Package prompt provides template-based prompt management and assembly.
//
// This package implements a registry system for loading, caching, and assembling
// prompt templates via repository interfaces:
//   - Fragment-based prompt composition
//   - Variable substitution with required/optional vars
//   - Model-specific overrides (template modifications only)
//   - Tool allowlist integration
//   - Version tracking and content hashing
//
// The Registry uses the repository pattern to load prompt configs, avoiding direct
// file I/O. It resolves fragment references, performs template variable substitution,
// and generates AssembledPrompt objects ready for LLM execution.
//
// # Architecture
//
// For system architecture and design patterns, see:
//   - Architecture overview: https://github.com/AltairaAI/promptkit-wip/blob/main/docs/architecture.md
//   - Prompt assembly pipeline: https://github.com/AltairaAI/promptkit-wip/blob/main/docs/prompt-assembly.md
//   - Repository pattern: https://github.com/AltairaAI/promptkit-wip/blob/main/docs/persistence-layer-proposal.md
//
// # Usage
//
// Create a registry with a repository (config-first pattern):
//
//	repo := memory.NewPromptRepository()
//	registry := prompt.NewRegistryWithRepository(repo)
//	assembled := registry.LoadWithVars("task_type", vars, "gpt-4")
//
// See package github.com/AltairaLabs/PromptKit/sdk for higher-level APIs.
package prompt

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/template"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AssembledPrompt represents a complete prompt ready for LLM execution.
type AssembledPrompt struct {
	TaskType     string            `json:"task_type"`
	SystemPrompt string            `json:"system_prompt"`
	AllowedTools []string          `json:"allowed_tools,omitempty"` // Tools this prompt can use
	Validators   []ValidatorConfig `json:"validators,omitempty"`    // Validators to apply at runtime
}

// UsesTools returns true if this prompt has tools configured
func (ap *AssembledPrompt) UsesTools() bool {
	return len(ap.AllowedTools) > 0
}

// PromptConfig represents a YAML prompt configuration file in K8s-style manifest format
type PromptConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       PromptSpec        `yaml:"spec"`
}

// PromptSpec contains the actual prompt configuration
type PromptSpec struct {
	TaskType       string                   `yaml:"task_type"`
	Version        string                   `yaml:"version"`
	Description    string                   `yaml:"description"`
	TemplateEngine *TemplateEngineInfo      `yaml:"template_engine,omitempty"` // Template engine configuration
	Fragments      []FragmentRef            `yaml:"fragments,omitempty"`       // New: fragment assembly
	SystemTemplate string                   `yaml:"system_template"`
	RequiredVars   []string                 `yaml:"required_vars"`
	OptionalVars   map[string]string        `yaml:"optional_vars"`
	Variables      []VariableMetadata       `yaml:"variables,omitempty"` // Enhanced variable metadata
	ModelOverrides map[string]ModelOverride `yaml:"model_overrides"`
	AllowedTools   []string                 `yaml:"allowed_tools,omitempty"` // Tools this prompt can use
	MediaConfig    *MediaConfig             `yaml:"media,omitempty"`         // Multimodal media configuration
	Validators     []ValidatorConfig        `yaml:"validators,omitempty"`    // Validators/Guardrails for production runtime
	TestedModels   []ModelTestResultRef     `yaml:"tested_models,omitempty"` // Model testing metadata
	Metadata       *PromptMetadata          `yaml:"metadata,omitempty"`      // Additional metadata for pack format
	Compilation    *CompilationInfo         `yaml:"compilation,omitempty"`   // Compilation information
}

// ModelTestResultRef is a simplified reference to model test results
// The full ModelTestResult type is in pkg/engine for tracking test execution
type ModelTestResultRef struct {
	Provider     string  `yaml:"provider"`
	Model        string  `yaml:"model"`
	Date         string  `yaml:"date"`
	SuccessRate  float64 `yaml:"success_rate"`
	AvgTokens    int     `yaml:"avg_tokens,omitempty"`
	AvgCost      float64 `yaml:"avg_cost,omitempty"`
	AvgLatencyMs int     `yaml:"avg_latency_ms,omitempty"`
}

// MediaConfig defines multimodal media support configuration for a prompt
type MediaConfig struct {
	// Enable multimodal support for this prompt
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Supported content types: "image", "audio", "video"
	SupportedTypes []string `yaml:"supported_types,omitempty" json:"supported_types,omitempty"`
	// Image-specific configuration
	Image *ImageConfig `yaml:"image,omitempty" json:"image,omitempty"`
	// Audio-specific configuration
	Audio *AudioConfig `yaml:"audio,omitempty" json:"audio,omitempty"`
	// Video-specific configuration
	Video *VideoConfig `yaml:"video,omitempty" json:"video,omitempty"`
	// Example multimodal messages
	Examples []MultimodalExample `yaml:"examples,omitempty" json:"examples,omitempty"`
}

// ImageConfig contains image-specific configuration
type ImageConfig struct {
	// Maximum image size in MB (0 = unlimited)
	MaxSizeMB int `yaml:"max_size_mb,omitempty" json:"max_size_mb,omitempty"`
	// Allowed formats: ["jpeg", "png", "webp", "gif"]
	AllowedFormats []string `yaml:"allowed_formats,omitempty" json:"allowed_formats,omitempty"`
	// Default detail level: "low", "high", "auto"
	DefaultDetail string `yaml:"default_detail,omitempty" json:"default_detail,omitempty"`
	// Whether captions are required
	RequireCaption bool `yaml:"require_caption,omitempty" json:"require_caption,omitempty"`
	// Max images per message (0 = unlimited)
	MaxImagesPerMsg int `yaml:"max_images_per_msg,omitempty" json:"max_images_per_msg,omitempty"`
}

// AudioConfig contains audio-specific configuration
type AudioConfig struct {
	// Maximum audio size in MB (0 = unlimited)
	MaxSizeMB int `yaml:"max_size_mb,omitempty" json:"max_size_mb,omitempty"`
	// Allowed formats: ["mp3", "wav", "ogg", "webm"]
	AllowedFormats []string `yaml:"allowed_formats,omitempty" json:"allowed_formats,omitempty"`
	// Max duration in seconds (0 = unlimited)
	MaxDurationSec int `yaml:"max_duration_sec,omitempty" json:"max_duration_sec,omitempty"`
	// Whether metadata (duration, bitrate) is required
	RequireMetadata bool `yaml:"require_metadata,omitempty" json:"require_metadata,omitempty"`
}

// VideoConfig contains video-specific configuration
type VideoConfig struct {
	// Maximum video size in MB (0 = unlimited)
	MaxSizeMB int `yaml:"max_size_mb,omitempty" json:"max_size_mb,omitempty"`
	// Allowed formats: ["mp4", "webm", "ogg"]
	AllowedFormats []string `yaml:"allowed_formats,omitempty" json:"allowed_formats,omitempty"`
	// Max duration in seconds (0 = unlimited)
	MaxDurationSec int `yaml:"max_duration_sec,omitempty" json:"max_duration_sec,omitempty"`
	// Whether metadata (resolution, fps) is required
	RequireMetadata bool `yaml:"require_metadata,omitempty" json:"require_metadata,omitempty"`
}

// MultimodalExample represents an example multimodal message for testing/documentation
type MultimodalExample struct {
	// Example name/identifier
	Name string `yaml:"name" json:"name"`
	// Human-readable description
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Message role: "user", "assistant"
	Role string `yaml:"role" json:"role"`
	// Content parts for this example
	Parts []ExampleContentPart `yaml:"parts" json:"parts"`
}

// ExampleContentPart represents a content part in an example (simplified for YAML)
type ExampleContentPart struct {
	// Content type: "text", "image", "audio", "video"
	Type string `yaml:"type" json:"type"`
	// Text content (for type=text)
	Text string `yaml:"text,omitempty" json:"text,omitempty"`
	// For media content
	Media *ExampleMedia `yaml:"media,omitempty" json:"media,omitempty"`
}

// ExampleMedia represents media references in examples
type ExampleMedia struct {
	// Relative path to media file
	FilePath string `yaml:"file_path,omitempty" json:"file_path,omitempty"`
	// External URL
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// MIME type
	MIMEType string `yaml:"mime_type" json:"mime_type"`
	// Detail level for images
	Detail string `yaml:"detail,omitempty" json:"detail,omitempty"`
	// Optional caption
	Caption string `yaml:"caption,omitempty" json:"caption,omitempty"`
}

// ValidatorConfig extends validators.ValidatorConfig with prompt-pack specific fields
type ValidatorConfig struct {
	// Embed base config (Type, Params)
	validators.ValidatorConfig `yaml:",inline"`
	// Enable/disable validator (default: true)
	Enabled *bool `yaml:"enabled,omitempty"`
	// Fail execution on violation (default: true)
	FailOnViolation *bool `yaml:"fail_on_violation,omitempty"`
}

// TemplateEngineInfo describes the template engine used for variable substitution
type TemplateEngineInfo struct {
	Version  string   `yaml:"version"`            // Template engine version (e.g., "v1")
	Syntax   string   `yaml:"syntax"`             // Template syntax (e.g., "{{variable}}")
	Features []string `yaml:"features,omitempty"` // Supported features (e.g., "conditionals", "loops")
}

// VariableMetadata contains enhanced metadata for a variable
type VariableMetadata struct {
	Name        string              `yaml:"name"`                  // Variable name
	Type        string              `yaml:"type,omitempty"`        // Variable type (string, int, etc.)
	Required    bool                `yaml:"required"`              // Whether variable is required
	Default     string              `yaml:"default,omitempty"`     // Default value if optional
	Description string              `yaml:"description,omitempty"` // Human-readable description
	Example     string              `yaml:"example,omitempty"`     // Example value
	Enum        []string            `yaml:"enum,omitempty"`        // Allowed values
	Validation  *VariableValidation `yaml:"validation,omitempty"`  // Validation rules
}

// VariableValidation contains validation rules for a variable
type VariableValidation struct {
	Pattern   string `yaml:"pattern,omitempty"`    // Regex pattern
	MinLength int    `yaml:"min_length,omitempty"` // Minimum length
	MaxLength int    `yaml:"max_length,omitempty"` // Maximum length
}

// PromptMetadata contains additional metadata for the pack format
type PromptMetadata struct {
	Domain       string              `yaml:"domain,omitempty"`        // Domain/category (e.g., "customer-support")
	Language     string              `yaml:"language,omitempty"`      // Primary language (e.g., "en")
	Tags         []string            `yaml:"tags,omitempty"`          // Tags for categorization
	CostEstimate *CostEstimate       `yaml:"cost_estimate,omitempty"` // Estimated cost per execution
	Performance  *PerformanceMetrics `yaml:"performance,omitempty"`   // Performance benchmarks
	Changelog    []ChangelogEntry    `yaml:"changelog,omitempty"`     // Version history
}

// CostEstimate provides estimated costs for prompt execution
type CostEstimate struct {
	MinCostUSD float64 `yaml:"min_cost_usd"` // Minimum cost per execution
	MaxCostUSD float64 `yaml:"max_cost_usd"` // Maximum cost per execution
	AvgCostUSD float64 `yaml:"avg_cost_usd"` // Average cost per execution
}

// PerformanceMetrics provides performance benchmarks
type PerformanceMetrics struct {
	AvgLatencyMs int     `yaml:"avg_latency_ms"` // Average latency in milliseconds
	P95LatencyMs int     `yaml:"p95_latency_ms"` // 95th percentile latency
	AvgTokens    int     `yaml:"avg_tokens"`     // Average tokens used
	SuccessRate  float64 `yaml:"success_rate"`   // Success rate (0.0-1.0)
}

// ChangelogEntry records a change in the prompt configuration
type ChangelogEntry struct {
	Version     string `yaml:"version"`          // Version number
	Date        string `yaml:"date"`             // Date of change (YYYY-MM-DD)
	Author      string `yaml:"author,omitempty"` // Author of change
	Description string `yaml:"description"`      // Description of change
}

// CompilationInfo contains information about prompt compilation
type CompilationInfo struct {
	CompiledWith string `yaml:"compiled_with"`    // Compiler version
	CreatedAt    string `yaml:"created_at"`       // Timestamp (RFC3339)
	Schema       string `yaml:"schema,omitempty"` // Pack schema version (e.g., "v1")
}

// FragmentRef references a prompt fragment for assembly
type FragmentRef struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path,omitempty"` // Optional: relative path to fragment file
	Required bool   `yaml:"required"`
}

// Fragment represents a reusable prompt fragment
type Fragment struct {
	Type              string `yaml:"fragment_type"`
	Version           string `yaml:"version"`
	Description       string `yaml:"description"`
	Content           string `yaml:"content"`
	SourceFile        string `yaml:"source_file,omitempty"`         // Source file path (for pack compilation)
	ResolvedAtCompile bool   `yaml:"resolved_at_compile,omitempty"` // Whether resolved at compile time
}

// ModelOverride contains model-specific template modifications.
// Note: Temperature and MaxTokens should be configured at the scenario or provider level,
// not in the prompt configuration.
type ModelOverride struct {
	SystemTemplate       string `yaml:"system_template,omitempty"`
	SystemTemplateSuffix string `yaml:"system_template_suffix,omitempty"`
}

// PromptRepository interface defines methods for loading prompts (to avoid import cycles)
// This should match persistence.PromptRepository interface
type PromptRepository interface {
	LoadPrompt(taskType string) (*PromptConfig, error)
	LoadFragment(name string, relativePath string, baseDir string) (*Fragment, error)
	ListPrompts() ([]string, error)
	SavePrompt(config *PromptConfig) error
}

// Registry manages prompt templates, versions, and variable substitution.
type Registry struct {
	repository       PromptRepository // Required repository for loading prompts
	promptCache      map[string]*PromptConfig
	fragmentCache    map[string]*Fragment
	fragmentResolver *FragmentResolver
	templateRenderer *template.Renderer
	mu               sync.RWMutex
}

// NewRegistryWithRepository creates a registry with a repository (new preferred method).
// This constructor uses the repository pattern for loading prompts, avoiding direct file I/O.
func NewRegistryWithRepository(repository PromptRepository) *Registry {
	return &Registry{
		repository:       repository,
		promptCache:      make(map[string]*PromptConfig),
		fragmentCache:    make(map[string]*Fragment),
		fragmentResolver: NewFragmentResolverWithRepository(repository),
		templateRenderer: template.NewRenderer(),
	}
}

// Load returns an assembled prompt for the specified activity with variable substitution.
func (r *Registry) Load(activity string) *AssembledPrompt {
	return r.LoadWithVars(activity, make(map[string]string), "")
}

// LoadWithVars loads a prompt with variable substitution and optional model override.
func (r *Registry) LoadWithVars(activity string, vars map[string]string, model string) *AssembledPrompt {
	config, err := r.loadConfig(activity)
	if err != nil {
		logger.Error("Failed to load prompt config for activity '%s': %v", activity, err)
		return nil
	}

	// Validate and merge variables
	finalVars, err := r.prepareVariables(config, vars, activity)
	if err != nil {
		return nil
	}

	// Get system template with model overrides applied
	systemTemplate := r.applyModelOverrides(config, model)

	// Render template and create assembled prompt
	return r.renderAndAssemble(config, systemTemplate, finalVars, activity)
}

// prepareVariables validates required vars, merges with defaults, and assembles fragments
func (r *Registry) prepareVariables(config *PromptConfig, vars map[string]string, activity string) (map[string]string, error) {
	// Validate required variables
	if err := r.validateRequiredVars(config, vars); err != nil {
		logger.Error("Prompt missing required vars for activity '%s': %v", activity, err)
		return nil, err
	}

	// Merge optional variables with defaults
	finalVars := r.mergeVars(config, vars)

	// Assemble fragments if configured
	if len(config.Spec.Fragments) > 0 {
		fragmentVars, err := r.assembleFragmentVars(config, finalVars, activity)
		if err != nil {
			return nil, err
		}
		// Merge fragment variables into final vars
		for key, val := range fragmentVars {
			finalVars[key] = val
		}
	}

	return finalVars, nil
}

// assembleFragmentVars assembles fragment variables
func (r *Registry) assembleFragmentVars(config *PromptConfig, finalVars map[string]string, activity string) (map[string]string, error) {
	fragmentVars, err := r.fragmentResolver.AssembleFragments(config.Spec.Fragments, finalVars, "")
	if err != nil {
		logger.Error("Fragment assembly failed for activity '%s': %v", activity, err)
		return nil, err
	}
	return fragmentVars, nil
}

// applyModelOverrides applies model-specific template overrides
func (r *Registry) applyModelOverrides(config *PromptConfig, model string) string {
	systemTemplate := config.Spec.SystemTemplate

	if model == "" {
		return systemTemplate
	}

	override, exists := config.Spec.ModelOverrides[model]
	if !exists {
		return systemTemplate
	}

	if override.SystemTemplate != "" {
		systemTemplate = override.SystemTemplate
	}
	if override.SystemTemplateSuffix != "" {
		systemTemplate += override.SystemTemplateSuffix
	}

	return systemTemplate
}

// renderAndAssemble renders the template and creates the final AssembledPrompt
func (r *Registry) renderAndAssemble(config *PromptConfig, systemTemplate string, finalVars map[string]string, activity string) *AssembledPrompt {
	// Render template with variables
	assembledText, err := r.templateRenderer.Render(systemTemplate, finalVars)
	if err != nil {
		logger.Error("Template rendering failed for activity '%s': %v", activity, err)
		return nil
	}

	// Generate hash for logging/debugging
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(assembledText)))

	result := AssembledPrompt{
		TaskType:     config.Spec.TaskType,
		SystemPrompt: assembledText,
		AllowedTools: config.Spec.AllowedTools,
		Validators:   config.Spec.Validators,
	}

	// Debug logging (controlled by global log level via -v flag)
	logger.Debug("🔧 Assembled prompt",
		"task_type", config.Spec.TaskType,
		"hash", hash[:8],
		"tools", len(config.Spec.AllowedTools),
		"validators", len(config.Spec.Validators))

	return &result
}

// ParsePromptConfig parses a prompt config from YAML data.
// This is a package-level utility function for parsing prompt configs in the config layer.
// The config layer should read files using os.ReadFile and pass the data to this function.
// Returns the parsed PromptConfig or an error if parsing/validation fails.
func ParsePromptConfig(data []byte) (*PromptConfig, error) {
	var config PromptConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate manifest format
	if config.APIVersion == "" {
		return nil, fmt.Errorf("missing required field: apiVersion")
	}
	if config.Kind != "PromptConfig" {
		return nil, fmt.Errorf("invalid kind: expected 'PromptConfig', got '%s'", config.Kind)
	}
	if config.Metadata.Name == "" {
		return nil, fmt.Errorf("missing required field: metadata.name")
	}
	if config.Spec.TaskType == "" {
		return nil, fmt.Errorf("missing required field: spec.task_type")
	}

	return &config, nil
}

// loadConfig loads a prompt configuration from the repository with caching
func (r *Registry) loadConfig(activity string) (*PromptConfig, error) {
	if r.repository == nil {
		return nil, fmt.Errorf("registry requires repository")
	}

	// Check cache first
	r.mu.RLock()
	if cached, ok := r.promptCache[activity]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	// Load from repository
	config, err := r.repository.LoadPrompt(activity)
	if err != nil {
		return nil, fmt.Errorf("failed to load prompt from repository: %w", err)
	}

	// Populate default values
	r.populateDefaults(config)

	// Cache the config
	r.mu.Lock()
	r.promptCache[activity] = config
	r.mu.Unlock()

	return config, nil
}

// validateRequiredVars ensures all required variables are provided
func (r *Registry) validateRequiredVars(config *PromptConfig, vars map[string]string) error {
	return r.templateRenderer.ValidateRequiredVars(config.Spec.RequiredVars, vars)
}

// mergeVars combines provided vars with optional defaults
func (r *Registry) mergeVars(config *PromptConfig, vars map[string]string) map[string]string {
	// Pre-allocate with known capacity for better performance
	result := make(map[string]string, len(config.Spec.OptionalVars)+len(vars))

	// Start with optional defaults
	for key, defaultVal := range config.Spec.OptionalVars {
		result[key] = defaultVal
	}

	// Override with provided vars
	for key, val := range vars {
		result[key] = val
	}

	return result
}

// GetAvailableRegions returns a list of all available regions from prompt fragments
func (r *Registry) GetAvailableRegions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	regionsMap := make(map[string]bool)

	// Check fragment cache for region-specific fragments
	for fragmentName := range r.fragmentCache {
		// Look for patterns like "persona_support_us", "persona_assistant_uk", etc.
		if strings.Contains(fragmentName, "_us") {
			regionsMap["us"] = true
		} else if strings.Contains(fragmentName, "_uk") {
			regionsMap["uk"] = true
		} else if strings.Contains(fragmentName, "_au") {
			regionsMap["au"] = true
		}
	}

	// Convert map to slice
	regions := make([]string, 0, len(regionsMap))
	for region := range regionsMap {
		regions = append(regions, region)
	}

	// If no regions found, return default set
	if len(regions) == 0 {
		return []string{}
	}

	return regions
}

// GetCachedPrompts returns a list of currently cached prompt task types.
// For a complete list including uncached prompts, use ListTaskTypes instead.
func (r *Registry) GetCachedPrompts() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return extractKeys(r.promptCache)
}

// GetCachedFragments returns a list of currently cached fragment keys.
func (r *Registry) GetCachedFragments() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return extractKeys(r.fragmentCache)
}

// ClearCache clears all cached prompts and fragments
func (r *Registry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.promptCache = make(map[string]*PromptConfig)
	r.fragmentCache = make(map[string]*Fragment)
}

// GetPromptInfo returns detailed information about a prompt configuration
func (r *Registry) GetPromptInfo(taskType string) (*PromptInfo, error) {
	config, err := r.loadConfig(taskType)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt info: %w", err)
	}

	return &PromptInfo{
		TaskType:       config.Spec.TaskType,
		Version:        config.Spec.Version,
		Description:    config.Spec.Description,
		FragmentCount:  len(config.Spec.Fragments),
		RequiredVars:   config.Spec.RequiredVars,
		OptionalVars:   extractKeys(config.Spec.OptionalVars),
		ToolAllowlist:  config.Spec.AllowedTools,
		ModelOverrides: extractKeys(config.Spec.ModelOverrides),
	}, nil
}

// PromptInfo provides summary information about a prompt configuration
type PromptInfo struct {
	TaskType       string
	Version        string
	Description    string
	FragmentCount  int
	RequiredVars   []string
	OptionalVars   []string
	ToolAllowlist  []string
	ModelOverrides []string
}

// extractKeys is a generic helper to extract keys from any map with string keys
func extractKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// populateDefaults fills in default values for optional fields in the config
func (r *Registry) populateDefaults(config *PromptConfig) {
	// Set default template engine info if not specified
	if config.Spec.TemplateEngine == nil {
		config.Spec.TemplateEngine = &TemplateEngineInfo{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		}
	}

	// Auto-generate Variables metadata from RequiredVars and OptionalVars if not specified
	if len(config.Spec.Variables) == 0 && (len(config.Spec.RequiredVars) > 0 || len(config.Spec.OptionalVars) > 0) {
		config.Spec.Variables = make([]VariableMetadata, 0)

		// Add required variables
		for _, varName := range config.Spec.RequiredVars {
			config.Spec.Variables = append(config.Spec.Variables, VariableMetadata{
				Name:     varName,
				Type:     "string",
				Required: true,
			})
		}

		// Add optional variables
		for varName, defaultVal := range config.Spec.OptionalVars {
			config.Spec.Variables = append(config.Spec.Variables, VariableMetadata{
				Name:     varName,
				Type:     "string",
				Required: false,
				Default:  defaultVal,
			})
		}
	}

	// Set default validator flags if not specified
	trueVal := true
	for i := range config.Spec.Validators {
		if config.Spec.Validators[i].Enabled == nil {
			config.Spec.Validators[i].Enabled = &trueVal
		}
		if config.Spec.Validators[i].FailOnViolation == nil {
			config.Spec.Validators[i].FailOnViolation = &trueVal
		}
	}
}

// ListTaskTypes returns all available task types from the repository.
// Falls back to cached task types if repository is unavailable or returns empty.
func (r *Registry) ListTaskTypes() []string {
	// Try repository first for complete list
	if r.repository != nil {
		if taskTypes, err := r.repository.ListPrompts(); err == nil && len(taskTypes) > 0 {
			return taskTypes
		}
	}

	// Fallback: return cached task types
	r.mu.RLock()
	defer r.mu.RUnlock()
	return extractKeys(r.promptCache)
}

// RegisterConfig registers a PromptConfig directly into the registry.
// This allows programmatic registration of prompts without requiring disk files.
// Useful for loading prompts from compiled packs or other in-memory sources.
// If a repository is configured, the config is persisted there as well.
func (r *Registry) RegisterConfig(taskType string, config *PromptConfig) error {
	if taskType == "" {
		return fmt.Errorf("task_type cannot be empty")
	}
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Ensure task_type is set in config
	if config.Spec.TaskType == "" {
		config.Spec.TaskType = taskType
	}

	// Populate defaults before saving
	r.populateDefaults(config)

	// Persist to repository if available
	if r.repository != nil {
		if err := r.repository.SavePrompt(config); err != nil {
			return fmt.Errorf("failed to save prompt to repository: %w", err)
		}
	}

	// Cache the config
	r.mu.Lock()
	r.promptCache[taskType] = config
	r.mu.Unlock()

	return nil
}

// Backward compatibility aliases - deprecated, use the new names instead

// GetAvailableTaskTypes is deprecated: use ListTaskTypes instead
func (r *Registry) GetAvailableTaskTypes() []string {
	return r.ListTaskTypes()
}

// GetLoadedPrompts is deprecated: use GetCachedPrompts instead
func (r *Registry) GetLoadedPrompts() []string {
	return r.GetCachedPrompts()
}

// GetLoadedFragments is deprecated: use GetCachedFragments instead
func (r *Registry) GetLoadedFragments() []string {
	return r.GetCachedFragments()
}

// LoadConfig is deprecated: use loadConfig directly (internal use) or use Load/LoadWithVars
func (r *Registry) LoadConfig(activity string) (*PromptConfig, error) {
	return r.loadConfig(activity)
}
