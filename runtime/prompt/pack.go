package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// PromptPackSchemaURL is the JSON Schema URL for validating PromptPack files
const PromptPackSchemaURL = "https://promptpack.org/schema/latest/promptpack.schema.json"

// Loader interface abstracts the registry for testing
type Loader interface {
	LoadConfig(taskType string) (*Config, error)
	ListTaskTypes() []string
}

// TimeProvider allows injecting time for deterministic tests
type TimeProvider interface {
	Now() time.Time
}

type realTimeProvider struct{}

// Now returns the current time.
func (r realTimeProvider) Now() time.Time {
	return time.Now()
}

// FileWriter abstracts file writing for testing
type FileWriter interface {
	WriteFile(path string, data []byte, perm os.FileMode) error
}

type realFileWriter struct{}

// WriteFile writes data to a file at the given path.
func (w realFileWriter) WriteFile(path string, data []byte, perm os.FileMode) error {
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
	// Schema reference for validation
	Schema string `json:"$schema,omitempty"` // JSON Schema URL for validation

	// Identity
	ID          string `json:"id"`          // Pack ID (e.g., "customer-support")
	Name        string `json:"name"`        // Human-readable name
	Version     string `json:"version"`     // Pack version
	Description string `json:"description"` // Pack description

	// Template Engine (shared across all prompts in pack)
	TemplateEngine *TemplateEngineInfo `json:"template_engine"`

	// Prompts - Map of task_type -> PackPrompt
	Prompts map[string]*PackPrompt `json:"prompts"`

	// Tools - Map of tool_name -> PackTool (per PromptPack spec Section 9)
	// Tools are defined at pack level and referenced by name in prompts
	Tools map[string]*PackTool `json:"tools,omitempty"`

	// Shared fragments (can be referenced by any prompt)
	Fragments map[string]string `json:"fragments,omitempty"` // Resolved fragments: name -> content

	// Metadata
	Metadata    *Metadata        `json:"metadata,omitempty"`
	Compilation *CompilationInfo `json:"compilation,omitempty"`

	// Evals - Pack-level eval definitions (applied to all prompts unless overridden)
	Evals []evals.EvalDef `json:"evals,omitempty"`

	// Workflow - State-machine workflow over the pack's prompts
	Workflow *workflow.Spec `json:"workflow,omitempty"`

	// Agents - Agent configuration mapping prompts to A2A-compatible agent definitions
	Agents *AgentsConfig `json:"agents,omitempty"`

	// Skills - Skill sources for dynamic capability loading
	Skills []SkillSourceConfig `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// SkillSourceConfig represents a skill source in the pack YAML.
type SkillSourceConfig struct {
	// Directory path for filesystem-based skills
	Dir string `json:"dir,omitempty" yaml:"dir,omitempty"`

	// Inline skill fields
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	Instructions string `json:"instructions,omitempty" yaml:"instructions,omitempty"`

	// Options
	Preload bool `json:"preload,omitempty" yaml:"preload,omitempty"`
}

// PackTool represents a tool definition in the pack (per PromptPack spec Section 9)
// Tools are defined at pack level and referenced by prompts via the tools array
type PackTool struct {
	Name        string      `json:"name"`        // Tool function name (required)
	Description string      `json:"description"` // Tool description (required)
	Parameters  interface{} `json:"parameters"`  // JSON Schema for input parameters (required)
}

// WorkflowConfig is an alias for workflow.Spec for backward compatibility.
type WorkflowConfig = workflow.Spec

// WorkflowState is an alias for workflow.State for backward compatibility.
type WorkflowState = workflow.State

// AgentsConfig maps prompts to A2A-compatible agent definitions.
type AgentsConfig struct {
	Entry   string               `json:"entry"`
	Members map[string]*AgentDef `json:"members"`
}

// AgentDef provides A2A Agent Card metadata for a single prompt.
type AgentDef struct {
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"input_modes,omitempty"`
	OutputModes []string `json:"output_modes,omitempty"`
}

// CompileOption configures optional fields for CompileFromRegistryWithOptions.
type CompileOption func(*compileOptions)

type compileOptions struct {
	workflow *workflow.Spec
	agents   *AgentsConfig
}

// WithWorkflow sets the workflow config on the compiled pack.
func WithWorkflow(w *workflow.Spec) CompileOption {
	return func(o *compileOptions) { o.workflow = w }
}

// WithAgents sets the agents config on the compiled pack.
func WithAgents(a *AgentsConfig) CompileOption {
	return func(o *compileOptions) { o.agents = a }
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

	// Evals - Prompt-level eval definitions (override pack-level evals by ID)
	Evals []evals.EvalDef `json:"evals,omitempty"`

	// Model Testing
	TestedModels []ModelTestResultRef `json:"tested_models,omitempty"`

	// Model Overrides
	ModelOverrides map[string]ModelOverride `json:"model_overrides,omitempty"`
}

// ToolPolicyPack represents tool policy in pack format
type ToolPolicyPack struct {
	ToolChoice          string   `json:"tool_choice,omitempty" yaml:"tool_choice,omitempty"`
	MaxRounds           int      `json:"max_rounds,omitempty" yaml:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty" yaml:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

// ParametersPack represents model parameters in pack format
type ParametersPack struct {
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty" yaml:"top_k,omitempty"`
}

// PackCompiler compiles Config to Pack format
type PackCompiler struct {
	loader       Loader
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
func NewPackCompilerWithDeps(loader Loader, timeProvider TimeProvider, fileWriter FileWriter) *PackCompiler {
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
	taskTypes := pc.loader.ListTaskTypes()
	if len(taskTypes) == 0 {
		return nil, fmt.Errorf("no prompts found in registry (registry may need to be pre-loaded)")
	}

	pack := pc.createEmptyPack(packID, len(taskTypes))

	for _, taskType := range taskTypes {
		if err := pc.addPromptToPack(pack, taskType); err != nil {
			return nil, err
		}
	}

	pack.Compilation = &CompilationInfo{
		CompiledWith: compilerVersion,
		CreatedAt:    pc.getCurrentTimestamp(),
		Schema:       "v1",
	}

	return pack, nil
}

// ToolData holds raw tool configuration data for compilation
type ToolData struct {
	FilePath string
	Data     []byte
}

// ParsedTool holds pre-parsed tool information for compilation
// Use this when YAML parsing happens in the calling package
type ParsedTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// CompileFromRegistryWithTools compiles ALL prompts from the registry into a single Pack
// and includes tool definitions from the provided tool data.
// This method satisfies PromptPack spec Section 9 which requires tools to be defined
// at pack level with name, description, and parameters.
func (pc *PackCompiler) CompileFromRegistryWithTools(
	packID, compilerVersion string,
	toolData []ToolData,
) (*Pack, error) {
	// First compile prompts
	pack, err := pc.CompileFromRegistry(packID, compilerVersion)
	if err != nil {
		return nil, err
	}

	// Parse and add tools to the pack
	if len(toolData) > 0 {
		pack.Tools = make(map[string]*PackTool)
		for _, td := range toolData {
			tool, err := parseToolData(td.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tool from %s: %w", td.FilePath, err)
			}
			if tool != nil {
				pack.Tools[tool.Name] = tool
			}
		}
	}

	return pack, nil
}

// CompileFromRegistryWithParsedTools compiles ALL prompts from the registry into a single Pack
// and includes pre-parsed tool definitions. Use this when YAML parsing happens externally.
func (pc *PackCompiler) CompileFromRegistryWithParsedTools(
	packID, compilerVersion string,
	parsedTools []ParsedTool,
) (*Pack, error) {
	return pc.CompileFromRegistryWithOptions(packID, compilerVersion, parsedTools, nil)
}

// CompileFromRegistryWithOptions compiles ALL prompts from the registry into a single Pack
// with pre-parsed tool definitions, pack-level eval definitions, and optional workflow/agents config.
func (pc *PackCompiler) CompileFromRegistryWithOptions(
	packID, compilerVersion string,
	parsedTools []ParsedTool,
	packEvals []evals.EvalDef,
	opts ...CompileOption,
) (*Pack, error) {
	// First compile prompts
	pack, err := pc.CompileFromRegistry(packID, compilerVersion)
	if err != nil {
		return nil, err
	}

	// Add pre-parsed tools to the pack
	if len(parsedTools) > 0 {
		pack.Tools = make(map[string]*PackTool)
		for _, pt := range parsedTools {
			pack.Tools[pt.Name] = ConvertToolToPackTool(pt.Name, pt.Description, pt.InputSchema)
		}
	}

	// Add pack-level evals
	if len(packEvals) > 0 {
		pack.Evals = packEvals
	}

	// Apply compile options (workflow, agents, etc.)
	var copts compileOptions
	for _, o := range opts {
		o(&copts)
	}
	if copts.workflow != nil {
		pack.Workflow = copts.workflow
	}
	if copts.agents != nil {
		pack.Agents = copts.agents
	}

	return pack, nil
}

// parseToolData parses raw YAML tool data into a PackTool
func parseToolData(data []byte) (*PackTool, error) {
	// Parse as a generic K8s-style config first
	var raw struct {
		APIVersion string `yaml:"apiVersion" json:"apiVersion"`
		Kind       string `yaml:"kind" json:"kind"`
		Spec       struct {
			Name        string          `yaml:"name" json:"name"`
			Description string          `yaml:"description" json:"description"`
			InputSchema json.RawMessage `yaml:"input_schema" json:"input_schema"`
		} `yaml:"spec" json:"spec"`
	}

	if err := parseYAMLConfig(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tool YAML: %w", err)
	}

	if raw.Kind != "Tool" {
		return nil, nil // Not a tool, skip
	}

	// Convert input_schema to parameters
	var params interface{}
	if len(raw.Spec.InputSchema) > 0 {
		if err := json.Unmarshal(raw.Spec.InputSchema, &params); err != nil {
			return nil, fmt.Errorf("failed to parse input_schema as JSON: %w", err)
		}
	}

	return &PackTool{
		Name:        raw.Spec.Name,
		Description: raw.Spec.Description,
		Parameters:  params,
	}, nil
}

// parseYAMLConfig is a helper to parse YAML without importing yaml package directly
// This uses JSON as intermediate format since yaml struct tags include json fallback
func parseYAMLConfig(data []byte, v interface{}) error {
	// Try to parse as JSON first (some configs may be JSON)
	if err := json.Unmarshal(data, v); err == nil {
		return nil
	}

	// For YAML, we need to convert via the standard library approach
	// Since we can't import yaml here without circular deps, we use a workaround
	// The actual yaml parsing happens in the caller (packc main)
	return fmt.Errorf("YAML parsing not available in this package, use ConvertToolToPackTool instead")
}

// ConvertToolToPackTool converts a tool descriptor to a PackTool
// This is the preferred method when tool parsing happens externally
func ConvertToolToPackTool(name, description string, inputSchema json.RawMessage) *PackTool {
	var params interface{}
	if len(inputSchema) > 0 {
		_ = json.Unmarshal(inputSchema, &params)
	}
	return &PackTool{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}

// createEmptyPack creates a new empty pack structure
func (pc *PackCompiler) createEmptyPack(packID string, promptCount int) *Pack {
	return &Pack{
		Schema:      PromptPackSchemaURL,
		ID:          packID,
		Name:        packID,
		Version:     "v1.0.0",
		Description: fmt.Sprintf("Pack containing %d prompts", promptCount),
		Prompts:     make(map[string]*PackPrompt),
		Tools:       make(map[string]*PackTool),
		Fragments:   make(map[string]string),
	}
}

// addPromptToPack loads a prompt config and adds it to the pack
func (pc *PackCompiler) addPromptToPack(pack *Pack, taskType string) error {
	config, err := pc.loader.LoadConfig(taskType)
	if err != nil {
		return fmt.Errorf("failed to load config for %s: %w", taskType, err)
	}

	if pack.TemplateEngine == nil && config.Spec.TemplateEngine != nil {
		pack.TemplateEngine = config.Spec.TemplateEngine
	}

	pack.Prompts[taskType] = pc.createPackPrompt(config)
	pc.collectFragments(pack, config)

	return nil
}

// createPackPrompt creates a PackPrompt from a Config
func (pc *PackCompiler) createPackPrompt(config *Config) *PackPrompt {
	// Ensure all variables have a type (default to "string" per PromptPack spec)
	variables := make([]VariableMetadata, len(config.Spec.Variables))
	for i, v := range config.Spec.Variables {
		variables[i] = v
		if variables[i].Type == "" {
			variables[i].Type = "string"
		}
	}

	return &PackPrompt{
		ID:             config.Spec.TaskType,
		Name:           config.Metadata.Name,
		Description:    config.Spec.Description,
		Version:        config.Spec.Version,
		SystemTemplate: config.Spec.SystemTemplate,
		Variables:      variables,
		Tools:          config.Spec.AllowedTools,
		ToolPolicy:     config.Spec.ToolPolicy,
		Parameters:     config.Spec.Parameters,
		Evals:          config.Spec.Evals,
		Validators:     config.Spec.Validators,
		MediaConfig:    config.Spec.MediaConfig,
		TestedModels:   config.Spec.TestedModels,
		ModelOverrides: config.Spec.ModelOverrides,
		Pipeline:       GetDefaultPipelineConfig(),
	}
}

// collectFragments collects fragment references from config into pack
func (pc *PackCompiler) collectFragments(pack *Pack, config *Config) {
	for _, fragRef := range config.Spec.Fragments {
		if _, exists := pack.Fragments[fragRef.Name]; !exists {
			pack.Fragments[fragRef.Name] = fmt.Sprintf("{{%s}}", fragRef.Name)
		}
	}
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
	warnings = append(warnings, p.validatePackFields()...)
	warnings = append(warnings, p.validateTemplateEngine()...)
	warnings = append(warnings, p.validatePrompts()...)
	warnings = append(warnings, p.validateCompilation()...)
	if p.Workflow != nil {
		result := p.ValidateWorkflow()
		warnings = append(warnings, result.Errors...)
		warnings = append(warnings, result.Warnings...)
	}
	if p.Agents != nil {
		errs, agentWarnings := p.ValidateAgents()
		warnings = append(warnings, errs...)
		warnings = append(warnings, agentWarnings...)
	}
	return warnings
}

// ValidateWorkflow validates the workflow section and returns a detailed result
// with separate errors and warnings. Returns an empty result if no workflow is present.
func (p *Pack) ValidateWorkflow() *workflow.ValidationResult {
	if p.Workflow == nil {
		return &workflow.ValidationResult{}
	}
	promptKeys := make([]string, 0, len(p.Prompts))
	for k := range p.Prompts {
		promptKeys = append(promptKeys, k)
	}
	return workflow.Validate(p.Workflow, promptKeys)
}

// validatePackFields validates pack-level required fields
func (p *Pack) validatePackFields() []string {
	warnings := []string{}
	if p.ID == "" {
		warnings = append(warnings, "missing required field: id")
	}
	if p.Version == "" {
		warnings = append(warnings, "missing required field: version")
	} else if err := validateSemanticVersion(p.Version); err != nil {
		warnings = append(warnings, fmt.Sprintf("invalid pack version '%s': %v", p.Version, err))
	}
	if len(p.Prompts) == 0 {
		warnings = append(warnings, "no prompts defined in pack")
	}
	return warnings
}

// validateTemplateEngine validates template engine configuration
func (p *Pack) validateTemplateEngine() []string {
	warnings := []string{}
	if p.TemplateEngine == nil {
		warnings = append(warnings, "missing template_engine configuration")
		return warnings
	}
	if p.TemplateEngine.Version == "" {
		warnings = append(warnings, "template_engine.version not set")
	}
	if p.TemplateEngine.Syntax == "" {
		warnings = append(warnings, "template_engine.syntax not set")
	}
	return warnings
}

// validatePrompts validates each prompt in the pack
func (p *Pack) validatePrompts() []string {
	warnings := []string{}
	for taskType, prompt := range p.Prompts {
		warnings = append(warnings, validatePrompt(taskType, prompt)...)
	}
	return warnings
}

// validatePrompt validates a single prompt
func validatePrompt(taskType string, prompt *PackPrompt) []string {
	warnings := []string{}
	if prompt.SystemTemplate == "" {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': missing system_template", taskType))
	}
	if len(prompt.Variables) == 0 {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': no variables defined", taskType))
	}
	if prompt.Version == "" {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': missing version", taskType))
	} else if err := validateSemanticVersion(prompt.Version); err != nil {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': invalid version '%s': %v", taskType, prompt.Version, err))
	}
	return warnings
}

// validateCompilation validates compilation metadata
func (p *Pack) validateCompilation() []string {
	if p.Compilation == nil {
		return []string{"missing compilation metadata"}
	}
	return []string{}
}

// ValidateAgents validates the agents section of the pack.
// Returns errors (which block compilation) and warnings (informational).
func (p *Pack) ValidateAgents() (errors, warnings []string) {
	if p.Agents == nil {
		return nil, nil
	}

	// Members must be non-empty
	if len(p.Agents.Members) == 0 {
		errors = append(errors, "agents: members must not be empty")
		return errors, warnings
	}

	// Entry must reference a key in Members
	if _, ok := p.Agents.Members[p.Agents.Entry]; !ok {
		errors = append(errors, fmt.Sprintf("agents: entry %q does not reference a valid member", p.Agents.Entry))
	}

	// All member keys must reference valid keys in Pack.Prompts
	for key := range p.Agents.Members {
		if _, ok := p.Prompts[key]; !ok {
			errors = append(errors, fmt.Sprintf("agents: member %q does not reference a valid prompt", key))
		}
	}

	// Validate individual agent definitions
	for key, agent := range p.Agents.Members {
		warnings = append(warnings, validateAgentDef(key, agent)...)
	}

	return errors, warnings
}

// validateAgentDef validates modes and tags for a single agent member definition.
func validateAgentDef(key string, agent *AgentDef) (warnings []string) {
	for _, mode := range agent.InputModes {
		if !strings.Contains(mode, "/") {
			warnings = append(warnings, fmt.Sprintf("agents: member %q input_mode %q is not a valid MIME type", key, mode))
		}
	}
	for _, mode := range agent.OutputModes {
		if !strings.Contains(mode, "/") {
			warnings = append(warnings, fmt.Sprintf("agents: member %q output_mode %q is not a valid MIME type", key, mode))
		}
	}
	for i, tag := range agent.Tags {
		if strings.TrimSpace(tag) == "" {
			warnings = append(warnings, fmt.Sprintf("agents: member %q tag[%d] is empty", key, i))
		}
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
		if !v.Required && v.Default != nil {
			// Convert default value to string
			if defaultStr, ok := v.Default.(string); ok && defaultStr != "" {
				vars[v.Name] = defaultStr
			}
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
