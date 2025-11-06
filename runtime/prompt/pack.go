package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// PromptLoader interface abstracts the registry for testing
type PromptLoader interface {
	LoadConfig(taskType string) (*PromptConfig, error)
	ListTaskTypes() []string
}

// TimeProvider allows injecting time for deterministic tests
type TimeProvider interface {
	Now() time.Time
}

type realTimeProvider struct{}

func (r realTimeProvider) Now() time.Time {
	return time.Now()
}

// FileWriter abstracts file writing for testing
type FileWriter interface {
	WriteFile(path string, data []byte, perm os.FileMode) error
}

type realFileWriter struct{}

func (r realFileWriter) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// Pack represents the complete JSON pack format containing MULTIPLE prompts for different task types.
//
// DESIGN DECISION: Why separate Pack types in runtime vs sdk?
//
// This runtime Pack is optimized for COMPILATION:
//   - Created by PackCompiler from prompt registry
//   - Includes Compilation and Metadata for tracking provenance
//   - Returns validation warnings ([]string) for compiler feedback
//   - No thread-safety needed (single-threaded compilation)
//   - Simple types (VariableMetadata, ValidatorConfig) for JSON serialization
//
// The sdk.Pack is optimized for LOADING & EXECUTION:
//   - Loaded from .pack.json files for application use
//   - Includes Tools map and filePath for execution context
//   - Thread-safe with sync.RWMutex for concurrent access
//   - Returns validation errors for application error handling
//   - Rich types (*Variable, *Validator) with additional methods
//   - Has CreateRegistry() to convert back to runtime.Registry for pipeline
//
// Both serialize to/from the SAME JSON format (.pack.json files), ensuring
// full interoperability. The type duplication is intentional and prevents
// circular dependencies while allowing each module to evolve independently.
//
// See sdk/pack.go for the corresponding SDK-side documentation.
type Pack struct {
	// Identity
	ID          string `json:"id"`          // Pack ID (e.g., "customer-support")
	Name        string `json:"name"`        // Human-readable name
	Version     string `json:"version"`     // Pack version
	Description string `json:"description"` // Pack description

	// Template Engine (shared across all prompts in pack)
	TemplateEngine *TemplateEngineInfo `json:"template_engine"`

	// Prompts - Map of task_type -> PackPrompt
	Prompts map[string]*PackPrompt `json:"prompts"`

	// Shared fragments (can be referenced by any prompt)
	Fragments map[string]string `json:"fragments,omitempty"` // Resolved fragments: name -> content

	// Metadata
	Metadata    *PromptMetadata  `json:"metadata,omitempty"`
	Compilation *CompilationInfo `json:"compilation,omitempty"`
}

// PackPrompt represents a single prompt configuration within a pack
type PackPrompt struct {
	// Identity
	ID          string `json:"id"`          // Prompt ID (task_type)
	Name        string `json:"name"`        // Human-readable name
	Description string `json:"description"` // Prompt description
	Version     string `json:"version"`     // Prompt version

	// Prompt
	SystemTemplate string `json:"system_template"`

	// Variables
	Variables []VariableMetadata `json:"variables,omitempty"`

	// Tools
	Tools      []string        `json:"tools,omitempty"`       // Allowed tool names
	ToolPolicy *ToolPolicyPack `json:"tool_policy,omitempty"` // Tool usage policy

	// Multimodal media configuration
	MediaConfig *MediaConfig `json:"media,omitempty"`

	// Pipeline
	Pipeline map[string]interface{} `json:"pipeline,omitempty"` // Pipeline configuration

	// Parameters
	Parameters *ParametersPack `json:"parameters,omitempty"` // Model-specific parameters

	// Validators
	Validators []ValidatorConfig `json:"validators,omitempty"`

	// Model Testing
	TestedModels []ModelTestResultRef `json:"tested_models,omitempty"`

	// Model Overrides
	ModelOverrides map[string]ModelOverride `json:"model_overrides,omitempty"`
}

// ToolPolicyPack represents tool policy in pack format
type ToolPolicyPack struct {
	ToolChoice          string   `json:"tool_choice,omitempty"`
	MaxRounds           int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// ParametersPack represents model parameters in pack format
type ParametersPack struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

// PackCompiler compiles PromptConfig to Pack format
type PackCompiler struct {
	loader       PromptLoader
	timeProvider TimeProvider
	fileWriter   FileWriter
}

// NewPackCompiler creates a new pack compiler with default dependencies
func NewPackCompiler(registry *Registry) *PackCompiler {
	return &PackCompiler{
		loader:       registry,
		timeProvider: realTimeProvider{},
		fileWriter:   realFileWriter{},
	}
}

// NewPackCompilerWithDeps creates a pack compiler with injected dependencies (for testing)
func NewPackCompilerWithDeps(loader PromptLoader, timeProvider TimeProvider, fileWriter FileWriter) *PackCompiler {
	return &PackCompiler{
		loader:       loader,
		timeProvider: timeProvider,
		fileWriter:   fileWriter,
	}
}

// CompileToFile compiles a prompt config to a JSON pack file
func (pc *PackCompiler) CompileToFile(taskType, outputPath, compilerVersion string) error {
	pack, err := pc.Compile(taskType, compilerVersion)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	return pc.WritePack(pack, outputPath)
}

// WritePack writes a pack to a file
func (pc *PackCompiler) WritePack(pack *Pack, outputPath string) error {
	data, err := pc.MarshalPack(pack)
	if err != nil {
		return fmt.Errorf("failed to marshal pack: %w", err)
	}

	if err := pc.fileWriter.WriteFile(outputPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write pack file: %w", err)
	}

	return nil
}

// MarshalPack marshals pack to JSON (testable without I/O)
func (pc *PackCompiler) MarshalPack(pack *Pack) ([]byte, error) {
	return json.MarshalIndent(pack, "", "  ")
}

// Compile compiles a single prompt config to Pack format (for backward compatibility)
func (pc *PackCompiler) Compile(taskType, compilerVersion string) (*Pack, error) {
	// Load the config (this will auto-populate defaults)
	config, err := pc.loader.LoadConfig(taskType)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create a pack with a single prompt
	pack := &Pack{
		ID:             config.Spec.TaskType,
		Name:           config.Metadata.Name,
		Version:        config.Spec.Version,
		Description:    config.Spec.Description,
		TemplateEngine: config.Spec.TemplateEngine,
		Prompts:        make(map[string]*PackPrompt),
		Fragments:      make(map[string]string),
	}

	// Add the prompt to the pack
	packPrompt := &PackPrompt{
		ID:             config.Spec.TaskType,
		Name:           config.Metadata.Name,
		Description:    config.Spec.Description,
		Version:        config.Spec.Version,
		SystemTemplate: config.Spec.SystemTemplate,
		Variables:      config.Spec.Variables,
		Tools:          config.Spec.AllowedTools,
		Validators:     config.Spec.Validators,
		MediaConfig:    config.Spec.MediaConfig,
		TestedModels:   config.Spec.TestedModels,
		ModelOverrides: config.Spec.ModelOverrides,
		Pipeline:       GetDefaultPipelineConfig(),
	}

	pack.Prompts[config.Spec.TaskType] = packPrompt

	// Resolve fragments into the pack
	if len(config.Spec.Fragments) > 0 {
		// Note: This is a simplified version. Full fragment resolution would need
		// to handle variables and paths properly
		for _, fragRef := range config.Spec.Fragments {
			// For now, just record the fragment names
			// Full implementation would load and resolve the fragment content
			pack.Fragments[fragRef.Name] = fmt.Sprintf("{{%s}}", fragRef.Name)
		}
	}

	// Generate compilation info
	builder := NewMetadataBuilder(&config.Spec)
	pack.Compilation = builder.BuildCompilationInfo(compilerVersion)
	pack.Metadata = config.Spec.Metadata

	return pack, nil
}

// CompileFromRegistry compiles ALL prompts from the registry into a single Pack
func (pc *PackCompiler) CompileFromRegistry(packID, compilerVersion string) (*Pack, error) {
	// Get all prompt configs from registry
	taskTypes := pc.loader.ListTaskTypes()

	// If registry is empty, try to load all available configs
	if len(taskTypes) == 0 {
		// Registry might not have loaded configs yet - this is expected
		// Return empty pack with message
		return nil, fmt.Errorf("no prompts found in registry (registry may need to be pre-loaded)")
	}

	// Create pack structure
	pack := &Pack{
		ID:          packID,
		Name:        packID,
		Version:     "v1.0.0", // Default version
		Description: fmt.Sprintf("Pack containing %d prompts", len(taskTypes)),
		Prompts:     make(map[string]*PackPrompt),
		Fragments:   make(map[string]string),
	}

	// Add each prompt to the pack
	for _, taskType := range taskTypes {
		config, err := pc.loader.LoadConfig(taskType)
		if err != nil {
			return nil, fmt.Errorf("failed to load config for %s: %w", taskType, err)
		}

		// Set pack-level fields from first prompt (template engine)
		if pack.TemplateEngine == nil && config.Spec.TemplateEngine != nil {
			pack.TemplateEngine = config.Spec.TemplateEngine
		}

		// Create PackPrompt
		packPrompt := &PackPrompt{
			ID:             config.Spec.TaskType,
			Name:           config.Metadata.Name,
			Description:    config.Spec.Description,
			Version:        config.Spec.Version,
			SystemTemplate: config.Spec.SystemTemplate,
			Variables:      config.Spec.Variables,
			Tools:          config.Spec.AllowedTools,
			Validators:     config.Spec.Validators,
			MediaConfig:    config.Spec.MediaConfig,
			TestedModels:   config.Spec.TestedModels,
			ModelOverrides: config.Spec.ModelOverrides,
			Pipeline:       GetDefaultPipelineConfig(),
		}

		pack.Prompts[taskType] = packPrompt

		// Collect fragments
		if len(config.Spec.Fragments) > 0 {
			for _, fragRef := range config.Spec.Fragments {
				// Avoid duplicates
				if _, exists := pack.Fragments[fragRef.Name]; !exists {
					pack.Fragments[fragRef.Name] = fmt.Sprintf("{{%s}}", fragRef.Name)
				}
			}
		}
	}

	// Generate compilation info
	pack.Compilation = &CompilationInfo{
		CompiledWith: compilerVersion,
		CreatedAt:    pc.getCurrentTimestamp(),
		Schema:       "v1",
	}

	return pack, nil
}

// getCurrentTimestamp returns the current timestamp in RFC3339 format
func (pc *PackCompiler) getCurrentTimestamp() string {
	return pc.timeProvider.Now().Format(time.RFC3339)
}

// LoadPack loads a pack from a JSON file
func LoadPack(filePath string) (*Pack, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read pack file: %w", err)
	}

	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("failed to parse pack JSON: %w", err)
	}

	return &pack, nil
}

// Validate validates a pack format
func (p *Pack) Validate() []string {
	warnings := []string{}

	// Check required fields
	if p.ID == "" {
		warnings = append(warnings, "missing required field: id")
	}
	if p.Version == "" {
		warnings = append(warnings, "missing required field: version")
	} else {
		// Validate pack version format
		if err := validateSemanticVersion(p.Version); err != nil {
			warnings = append(warnings, fmt.Sprintf("invalid pack version '%s': %v", p.Version, err))
		}
	}
	if len(p.Prompts) == 0 {
		warnings = append(warnings, "no prompts defined in pack")
	}

	// Check template engine
	if p.TemplateEngine == nil {
		warnings = append(warnings, "missing template_engine configuration")
	} else {
		if p.TemplateEngine.Version == "" {
			warnings = append(warnings, "template_engine.version not set")
		}
		if p.TemplateEngine.Syntax == "" {
			warnings = append(warnings, "template_engine.syntax not set")
		}
	}

	// Validate each prompt
	for taskType, prompt := range p.Prompts {
		if prompt.SystemTemplate == "" {
			warnings = append(warnings, fmt.Sprintf("prompt '%s': missing system_template", taskType))
		}
		if len(prompt.Variables) == 0 {
			warnings = append(warnings, fmt.Sprintf("prompt '%s': no variables defined", taskType))
		}

		// Validate prompt version
		if prompt.Version == "" {
			warnings = append(warnings, fmt.Sprintf("prompt '%s': missing version", taskType))
		} else {
			if err := validateSemanticVersion(prompt.Version); err != nil {
				warnings = append(warnings, fmt.Sprintf("prompt '%s': invalid version '%s': %v", taskType, prompt.Version, err))
			}
		}
	}

	// Check compilation info
	if p.Compilation == nil {
		warnings = append(warnings, "missing compilation metadata")
	}

	return warnings
}

// GetPrompt returns a specific prompt by task type
func (p *Pack) GetPrompt(taskType string) *PackPrompt {
	return p.Prompts[taskType]
}

// ListPrompts returns all prompt task types in the pack
func (p *Pack) ListPrompts() []string {
	prompts := make([]string, 0, len(p.Prompts))
	for taskType := range p.Prompts {
		prompts = append(prompts, taskType)
	}
	return prompts
}

// GetRequiredVariables returns all required variable names for a specific prompt
func (p *Pack) GetRequiredVariables(taskType string) []string {
	prompt := p.Prompts[taskType]
	if prompt == nil {
		return []string{}
	}

	vars := []string{}
	for _, v := range prompt.Variables {
		if v.Required {
			vars = append(vars, v.Name)
		}
	}
	return vars
}

// GetOptionalVariables returns all optional variable names with defaults for a specific prompt
func (p *Pack) GetOptionalVariables(taskType string) map[string]string {
	prompt := p.Prompts[taskType]
	if prompt == nil {
		return make(map[string]string)
	}

	vars := make(map[string]string)
	for _, v := range prompt.Variables {
		if !v.Required && v.Default != "" {
			vars[v.Name] = v.Default
		}
	}
	return vars
}

// GetToolNames returns the list of allowed tool names for a specific prompt
func (p *Pack) GetToolNames(taskType string) []string {
	prompt := p.Prompts[taskType]
	if prompt == nil {
		return []string{}
	}
	return prompt.Tools
}

// Summary returns a brief summary of the pack
func (p *Pack) Summary() string {
	return fmt.Sprintf("Pack: %s v%s (%d prompts)",
		p.Name, p.Version, len(p.Prompts))
}
