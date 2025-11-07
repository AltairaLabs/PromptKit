// Package sdk provides a high-level SDK for building LLM applications with PromptKit.
//
// The SDK is built around PromptPacks - compiled JSON files containing prompts,
// variables, tools, and validators. This PromptPack-first approach ensures you
// get the full benefits of PromptKit's pipeline architecture including:
//   - Prompt assembly with variable interpolation
//   - Template rendering with fragments
//   - Tool orchestration and governance
//   - Response validation and guardrails
//   - State persistence across conversations
//
// Two API Levels:
//
// High-Level API (ConversationManager):
//   - Simple interface for common use cases
//   - Automatic pipeline construction
//   - Load pack, create conversation, send messages
//
// Low-Level API (PipelineBuilder):
//   - Custom middleware injection
//   - Full pipeline control
//   - Advanced use cases (custom context builders, observability)
package sdk

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Pack represents a loaded PromptPack containing multiple prompts for related task types.
// A pack is a portable, JSON-based bundle created by the packc compiler.
//
// DESIGN DECISION: Why separate Pack types in sdk vs runtime?
//
// This SDK Pack is optimized for LOADING & EXECUTION:
//   - Loaded from .pack.json files for application use
//   - Includes Tools map for runtime tool access
//   - Includes filePath to track source file location
//   - Thread-safe with sync.RWMutex for concurrent access
//   - Returns validation errors for application error handling
//   - Rich types (*Variable, *Validator, *Tool) with full functionality
//   - Has CreateRegistry() to convert to runtime.Registry for pipeline execution
//   - Has convertToRuntimeConfig() to bridge SDK ↔ runtime formats
//
// The runtime.prompt.Pack is optimized for COMPILATION:
//   - Created by PackCompiler during prompt compilation
//   - Includes Compilation and Metadata fields for provenance tracking
//   - Returns validation warnings ([]string) for compiler feedback
//   - No thread-safety (single-threaded compilation process)
//   - Simple types for clean JSON serialization
//   - No conversion methods (produces, doesn't consume)
//
// Both types serialize to/from the SAME JSON format (.pack.json files),
// ensuring full interoperability between compilation and execution phases.
// The duplication is intentional and provides:
//  1. Clear separation of concerns (compile vs execute)
//  2. No circular dependencies (sdk imports runtime, not vice versa)
//  3. Independent evolution of each module
//  4. Type-specific optimizations (thread-safety, validation behavior)
//
// Design: A pack contains MULTIPLE prompts (task_types) that share common configuration
// like template engine and fragments, but each prompt has its own template, variables,
// tools, and validators.
//
// See runtime/prompt/pack.go for the corresponding runtime-side documentation.
type Pack struct {
	// Pack identity
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`

	// Shared configuration across all prompts
	TemplateEngine TemplateEngine `json:"template_engine"`

	// Map of task_type -> prompt configuration
	Prompts map[string]*Prompt `json:"prompts"`

	// Shared fragments used by all prompts
	Fragments map[string]string `json:"fragments,omitempty"`

	// Tool definitions (referenced by prompts)
	Tools map[string]*Tool `json:"tools,omitempty"`

	// File path this pack was loaded from (not in JSON)
	filePath string `json:"-"`

	// Lock for thread-safe access
	mu sync.RWMutex `json:"-"`
}

// TemplateEngine describes the template engine configuration shared across prompts
type TemplateEngine struct {
	Version  string   `json:"version"`
	Syntax   string   `json:"syntax"`
	Features []string `json:"features"`
}

// Prompt represents a single prompt configuration within a pack
type Prompt struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`

	// Template
	SystemTemplate string `json:"system_template"`

	// Variables for this prompt
	Variables []*Variable `json:"variables"`

	// Tool references (names that map to pack.Tools)
	ToolNames []string `json:"tools,omitempty"`

	// Tool policy
	ToolPolicy *ToolPolicy `json:"tool_policy,omitempty"`

	// Multimodal media configuration
	MediaConfig *prompt.MediaConfig `json:"media,omitempty"`

	// Pipeline configuration
	Pipeline *PipelineConfig `json:"pipeline,omitempty"`

	// LLM parameters
	Parameters *Parameters `json:"parameters,omitempty"`

	// Validators
	Validators []*Validator `json:"validators,omitempty"`

	// Model testing results
	TestedModels []*TestedModel `json:"tested_models,omitempty"`

	// Model-specific overrides
	ModelOverrides map[string]*ModelOverride `json:"model_overrides,omitempty"`
}

// Variable defines a template variable with validation rules
type Variable struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // "string", "number", "boolean", "object", "array"
	Required    bool                   `json:"required"`
	Default     interface{}            `json:"default,omitempty"`
	Description string                 `json:"description"`
	Example     interface{}            `json:"example,omitempty"`
	Validation  map[string]interface{} `json:"validation,omitempty"`
}

// ToolPolicy defines tool usage constraints
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice"` // "auto", "required", "none"
	MaxRounds           int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// PipelineConfig defines pipeline middleware configuration
type PipelineConfig struct {
	Stages     []string            `json:"stages"`
	Middleware []*MiddlewareConfig `json:"middleware"`
}

// MiddlewareConfig defines a single middleware configuration
type MiddlewareConfig struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// Parameters defines LLM generation parameters
type Parameters struct {
	Temperature float32 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	TopP        float32 `json:"top_p,omitempty"`
	TopK        *int    `json:"top_k,omitempty"`
}

// Validator defines a validation rule
type Validator struct {
	Type            string                 `json:"type"`
	Enabled         bool                   `json:"enabled"`
	FailOnViolation bool                   `json:"fail_on_violation"`
	Params          map[string]interface{} `json:"params"`
}

// TestedModel contains testing results for a specific model
type TestedModel struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Date         string  `json:"date"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTokens    int     `json:"avg_tokens"`
	AvgCost      float64 `json:"avg_cost"`
	AvgLatencyMs int     `json:"avg_latency_ms"`
}

// ModelOverride defines model-specific template overrides
type ModelOverride struct {
	SystemTemplateSuffix string `json:"system_template_suffix,omitempty"`
}

// Tool defines a tool that can be called by the LLM
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// PackManager manages loading and caching of PromptPacks
type PackManager struct {
	packs map[string]*Pack // packID -> Pack
	mu    sync.RWMutex
}

// NewPackManager creates a new PackManager
func NewPackManager() *PackManager {
	return &PackManager{
		packs: make(map[string]*Pack),
	}
}

// LoadPack loads a PromptPack from a .pack.json file
func (pm *PackManager) LoadPack(packPath string) (*Pack, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check cache first
	if pack, exists := pm.packs[packPath]; exists {
		return pack, nil
	}

	// Read pack file
	data, err := os.ReadFile(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read pack file %s: %w", packPath, err)
	}

	// Parse JSON
	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("failed to parse pack file %s: %w", packPath, err)
	}

	// Validate pack
	if err := pm.validatePack(&pack); err != nil {
		return nil, fmt.Errorf("invalid pack %s: %w", packPath, err)
	}

	// Store file path
	pack.filePath = packPath

	// Cache pack
	pm.packs[packPath] = &pack

	return &pack, nil
}

// validatePack validates a pack structure
func (pm *PackManager) validatePack(pack *Pack) error {
	if pack.ID == "" {
		return fmt.Errorf("pack missing required field: id")
	}
	if pack.Name == "" {
		return fmt.Errorf("pack missing required field: name")
	}
	if pack.Version == "" {
		return fmt.Errorf("pack missing required field: version")
	}

	// Validate pack version follows semantic versioning
	if err := validateSemanticVersion(pack.Version); err != nil {
		return fmt.Errorf("pack version '%s' is invalid: %w", pack.Version, err)
	}

	if len(pack.Prompts) == 0 {
		return fmt.Errorf("pack contains no prompts")
	}

	// Validate each prompt
	for taskType, prompt := range pack.Prompts {
		if err := pm.validatePrompt(prompt, pack); err != nil {
			return fmt.Errorf("invalid prompt %s: %w", taskType, err)
		}
	}

	return nil
}

// validatePrompt validates a single prompt configuration
func (pm *PackManager) validatePrompt(prompt *Prompt, pack *Pack) error {
	if prompt.ID == "" {
		return fmt.Errorf("prompt missing required field: id")
	}
	if prompt.SystemTemplate == "" {
		return fmt.Errorf("prompt missing required field: system_template")
	}

	// Validate prompt version if present
	if prompt.Version != "" {
		if err := validateSemanticVersion(prompt.Version); err != nil {
			return fmt.Errorf("prompt '%s' version '%s' is invalid: %w", prompt.Name, prompt.Version, err)
		}
	}

	// Validate tool references
	for _, toolName := range prompt.ToolNames {
		if _, exists := pack.Tools[toolName]; !exists {
			return fmt.Errorf("prompt references undefined tool: %s", toolName)
		}
	}

	// Validate required variables have no default
	for _, variable := range prompt.Variables {
		if variable.Required && variable.Default != nil {
			return fmt.Errorf("required variable %s cannot have a default value", variable.Name)
		}
	}

	return nil
}

// GetPack retrieves a cached pack by path
func (pm *PackManager) GetPack(packPath string) (*Pack, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pack, exists := pm.packs[packPath]
	return pack, exists
}

// GetPrompt retrieves a specific prompt from a pack
func (p *Pack) GetPrompt(taskType string) (*Prompt, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prompt, exists := p.Prompts[taskType]
	if !exists {
		return nil, fmt.Errorf("pack %s does not contain prompt for task_type: %s", p.ID, taskType)
	}

	return prompt, nil
}

// ListPrompts returns all available task types in the pack
func (p *Pack) ListPrompts() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	taskTypes := make([]string, 0, len(p.Prompts))
	for taskType := range p.Prompts {
		taskTypes = append(taskTypes, taskType)
	}
	return taskTypes
}

// GetTools retrieves tools used by a specific prompt
func (p *Pack) GetTools(taskType string) ([]*Tool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prompt, exists := p.Prompts[taskType]
	if !exists {
		return nil, fmt.Errorf("pack %s does not contain prompt for task_type: %s", p.ID, taskType)
	}

	tools := make([]*Tool, 0, len(prompt.ToolNames))
	for _, toolName := range prompt.ToolNames {
		if tool, exists := p.Tools[toolName]; exists {
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// CreateRegistry creates a runtime prompt.Registry from this pack.
// The registry allows the runtime pipeline to access prompts using the standard
// prompt assembly middleware. Each prompt in the pack is registered by its task_type.
//
// This bridges the SDK's .pack.json format with the runtime's prompt.Registry format.
func (p *Pack) CreateRegistry() (*prompt.Registry, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Prompts) == 0 {
		return nil, fmt.Errorf("pack %s contains no prompts", p.ID)
	}

	// Create memory repository for in-memory prompt storage
	memRepo := memory.NewMemoryPromptRepository()

	// Convert SDK prompts to runtime PromptConfigs and register them
	for taskType, sdkPrompt := range p.Prompts {
		// Convert SDK Prompt to runtime PromptConfig
		runtimeConfig := p.convertToRuntimeConfig(taskType, sdkPrompt)

		// Register in memory repository
		memRepo.RegisterPrompt(taskType, runtimeConfig)
	}

	// Create registry with memory repository (new persistence layer)
	registry := prompt.NewRegistryWithRepository(memRepo)

	return registry, nil
}

// convertToRuntimeConfig converts SDK Prompt format to runtime PromptConfig format
func (p *Pack) convertToRuntimeConfig(taskType string, sdkPrompt *Prompt) *prompt.PromptConfig {
	// Build variable metadata from SDK variables
	varMetadata := make([]prompt.VariableMetadata, 0, len(sdkPrompt.Variables))
	requiredVars := []string{}
	optionalVars := make(map[string]string)

	for _, v := range sdkPrompt.Variables {
		varMetadata = append(varMetadata, prompt.VariableMetadata{
			Name:        v.Name,
			Type:        v.Type,
			Required:    v.Required,
			Default:     formatDefault(v.Default),
			Description: v.Description,
			Example:     formatDefault(v.Example),
		})

		if v.Required {
			requiredVars = append(requiredVars, v.Name)
		} else if v.Default != nil {
			optionalVars[v.Name] = formatDefault(v.Default)
		}
	}

	// Convert validators
	validatorConfigs := make([]prompt.ValidatorConfig, 0, len(sdkPrompt.Validators))
	for _, v := range sdkPrompt.Validators {
		enabled := v.Enabled
		failOnViolation := v.FailOnViolation

		validatorConfigs = append(validatorConfigs, prompt.ValidatorConfig{
			ValidatorConfig: validators.ValidatorConfig{
				Type:   v.Type,
				Params: v.Params,
			},
			Enabled:         &enabled,
			FailOnViolation: &failOnViolation,
		})
	}

	// Convert model overrides
	modelOverrides := make(map[string]prompt.ModelOverride)
	if sdkPrompt.ModelOverrides != nil {
		for model, override := range sdkPrompt.ModelOverrides {
			modelOverrides[model] = prompt.ModelOverride{
				SystemTemplateSuffix: override.SystemTemplateSuffix,
			}
		}
	}

	// Convert tested models
	testedModels := make([]prompt.ModelTestResultRef, 0, len(sdkPrompt.TestedModels))
	for _, tm := range sdkPrompt.TestedModels {
		testedModels = append(testedModels, prompt.ModelTestResultRef{
			Provider:     tm.Provider,
			Model:        tm.Model,
			Date:         tm.Date,
			SuccessRate:  tm.SuccessRate,
			AvgTokens:    tm.AvgTokens,
			AvgCost:      tm.AvgCost,
			AvgLatencyMs: tm.AvgLatencyMs,
		})
	}

	// Build PromptConfig
	return &prompt.PromptConfig{
		APIVersion: "promptkit.altairalabs.ai/v1",
		Kind:       "Prompt",
		Spec: prompt.PromptSpec{
			TaskType:       taskType,
			Version:        sdkPrompt.Version,
			Description:    sdkPrompt.Description,
			SystemTemplate: sdkPrompt.SystemTemplate,
			RequiredVars:   requiredVars,
			OptionalVars:   optionalVars,
			Variables:      varMetadata,
			ModelOverrides: modelOverrides,
			AllowedTools:   sdkPrompt.ToolNames,
			MediaConfig:    sdkPrompt.MediaConfig, // Multimodal support
			Validators:     validatorConfigs,
			TestedModels:   testedModels,
			TemplateEngine: &prompt.TemplateEngineInfo{
				Version:  p.TemplateEngine.Version,
				Syntax:   p.TemplateEngine.Syntax,
				Features: p.TemplateEngine.Features,
			},
		},
	}
}

// formatDefault converts an interface{} value to a string for use as a default value
func formatDefault(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
