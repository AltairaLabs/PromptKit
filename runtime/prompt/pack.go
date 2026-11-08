package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
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

// Pack represents a compiled PromptPack.
//
// EMBEDS the generated type rather than aliasing it. Go has no partial classes,
// and a type alias cannot carry methods — but an embedded type can, and Pack has
// eighteen (Validate, ValidateWorkflow, ValidateAgents, GetPrompt, ...). Every
// spec property is promoted from packspec.Pack, so the properties cannot drift
// even though the methods live here.
//
// This is the general answer for a pack type with behavior attached. An alias is
// still preferable where the methods are few enough to become free functions,
// because an alias gives true identity with the generated type; embedding needs
// .Pack to extract it. Types with a method set that IS their API embed instead.
//
// FilePath is the only field of our own, and it is the reason a bare alias would
// not do even without the methods.
type Pack struct {
	packspec.Pack `yaml:",inline"`

	// FilePath is the on-disk path this pack was loaded from, if any. It is
	// never serialized; loaders set it so schema/fragment resolution can
	// resolve paths relative to the pack file.
	FilePath string `json:"-" yaml:"-"`
}

// SkillSourceConfig declares a skill source for the pack.
//
// Generated. $defs/SkillSource is a oneOf over a bare string, a SkillPathSource
// and an InlineSkill; the generator flattens that into one struct AND emits the
// Marshal/Unmarshal pair that keeps the scalar form in Shorthand, so the union
// needed no hand-written code at all.
//
// The legacy `dir` alias is GONE. It was promptkit-only, and because
// $defs/SkillPathSource is additionalProperties:false and requires `path`, a
// pack emitted from `dir` failed validation three ways at once. No shipped pack
// used it. Author `path` instead, or the bare-string shorthand.
type SkillSourceConfig = packspec.SkillSource

// SkillPath returns the path a skill source points at, resolving the
// bare-string shorthand. A free function because SkillSourceConfig is the
// generated type and an alias cannot carry methods.
func SkillPath(s *SkillSourceConfig) string {
	if s == nil {
		return ""
	}
	if s.Path != "" {
		return s.Path
	}
	return s.Shorthand
}

// PackTool represents a tool definition in the pack (per PromptPack spec Section 9).
// Tools are defined at pack level and referenced by prompts via the tools array.
//
// Generated from the schema: an ALIAS for packspec.Tool.
//
// Parameters is *ToolParameters rather than the interface{} the hand-written
// type used. The spec defines a shape here (type/properties/required) and
// interface{} ignored it; the generated type keeps that shape while remaining
// extensible, since Tool.parameters omits additionalProperties and JSON Schema
// defaults it to true. So a tool schema using $defs, oneOf or any other keyword
// still round-trips, through ToolParameters.Extra.
type PackTool = packspec.Tool

// WorkflowConfig is an alias for workflow.Spec for backward compatibility.
type WorkflowConfig = workflow.Spec

// WorkflowState is an alias for workflow.State for backward compatibility.
type WorkflowState = workflow.State

// AgentsConfig maps prompts to A2A-compatible agent definitions.
//
// Generated from the schema: an ALIAS for packspec.AgentsConfig.
type AgentsConfig = packspec.AgentsConfig

// AgentDef provides A2A Agent Card metadata for a single prompt.
//
// Generated from the schema: this is an ALIAS for packspec.AgentDef, not a copy.
// An alias keeps every call site, tag and behavior identical while making the
// schema the single source of the shape — a defined type would not, and the
// two would be free to drift again.
//
// The hand-written struct it replaced was already field-for-field identical to
// $defs/AgentDef, which is why it is first: the switch is provably a no-op.
type AgentDef = packspec.AgentDef

// CompileOption configures optional fields for CompileFromRegistryWithOptions.
type CompileOption func(*compileOptions)

type compileOptions struct {
	workflow     *workflow.Spec
	agents       *AgentsConfig
	skills       []SkillSourceConfig
	compositions map[string]*composition.Composition
	metadata     *Metadata
}

// WithWorkflow sets the workflow config on the compiled pack.
func WithWorkflow(w *workflow.Spec) CompileOption {
	return func(o *compileOptions) { o.workflow = w }
}

// WithAgents sets the agents config on the compiled pack.
func WithAgents(a *AgentsConfig) CompileOption {
	return func(o *compileOptions) { o.agents = a }
}

// WithSkills sets the skills config on the compiled pack.
func WithSkills(s []SkillSourceConfig) CompileOption {
	return func(o *compileOptions) { o.skills = s }
}

// WithCompositions sets the compositions map on the compiled pack.
func WithCompositions(c map[string]*composition.Composition) CompileOption {
	return func(o *compileOptions) { o.compositions = c }
}

// WithMetadata sets the pack-level metadata block — domain, language, tags,
// cost_estimate and governance (RFC 0013).
//
// Governance had no authoring route before this: metadata.governance could be
// carried in a compiled pack and never written by an author, which is the same
// as not existing.
func WithMetadata(m *Metadata) CompileOption {
	return func(o *compileOptions) { o.metadata = m }
}

// PackPrompt is a single prompt definition within a pack.
//
// Generated. It is the hub every prompt hangs off, so adopting it was left
// until everything reachable from it was generated first — validators, evals,
// variables, tested_models, model_overrides, media and parameters are all
// packspec types now, and the generated Prompt holds them as slices and maps of
// POINTERS, which is the shape the schema implies for optional objects.
//
// ToPromptConfig() was a method. A type alias cannot carry methods, so it is
// the free function ToConfig below.
type PackPrompt = packspec.Prompt

// ToolPolicyPack represents tool policy in pack format
// Generated from the schema: an ALIAS for packspec.ToolPolicy.
// max_rounds, max_tool_calls_per_turn and tool_choice are pointers for the same reason.
type ToolPolicyPack = packspec.ToolPolicy

// ParametersPack represents model parameters in pack format
// Generated from the schema: an ALIAS for packspec.Parameters.
// Gains frequency_penalty and presence_penalty, which the spec defines and the hand-written type omitted.
type ParametersPack = packspec.Parameters

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

// packFilePerm is the file mode of a written pack: owner read/write only.
const packFilePerm = 0o600

// WritePack writes a pack to a file
func (pc *PackCompiler) WritePack(pack *Pack, outputPath string) error {
	data, err := pc.MarshalPack(pack)
	if err != nil {
		return fmt.Errorf("failed to marshal pack: %w", err)
	}

	if err := pc.fileWriter.WriteFile(outputPath, data, packFilePerm); err != nil {
		return fmt.Errorf("failed to write pack file: %w", err)
	}

	return nil
}

// MarshalPack marshals pack to JSON (testable without I/O)
func (pc *PackCompiler) MarshalPack(pack *Pack) ([]byte, error) {
	return json.MarshalIndent(pack, "", "  ")
}

// packIDFromTaskType derives a pack id from a task type. The two follow
// different patterns in the PromptPack schema: a prompt id allows underscores
// and a pack id does not, so `technical_support` becomes `technical-support`.
// The prompt itself keeps the task type as its id and key.
func packIDFromTaskType(taskType string) string {
	return strings.ReplaceAll(taskType, "_", "-")
}

// Compile compiles a single prompt config to Pack format (for backward compatibility).
// The pack id is the task type with underscores replaced by hyphens.
func (pc *PackCompiler) Compile(taskType, compilerVersion string) (*Pack, error) {
	// Load the config (this will auto-populate defaults)
	config, err := pc.loader.LoadConfig(taskType)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create a pack with a single prompt
	pack := &Pack{Pack: packspec.Pack{
		Schema:         packspec.Ptr(PromptPackSchemaURL),
		ID:             packIDFromTaskType(config.Spec.TaskType),
		Name:           config.Metadata.Name,
		Version:        config.Spec.Version,
		Description:    config.Spec.Description,
		TemplateEngine: config.Spec.TemplateEngine,
		Prompts:        make(map[string]*PackPrompt),
		Fragments:      make(map[string]string),
	}}

	// Add the prompt to the pack. The single-prompt path builds it exactly as
	// CompileFromRegistry does, so a field the compiler learns to carry
	// (Parameters, Evals, RFC 0016 extensions) reaches both outputs.
	pack.Prompts[config.Spec.TaskType] = pc.createPackPrompt(config)
	if err := pc.collectFragments(pack, taskType, config); err != nil {
		return nil, err
	}

	// Generate compilation info
	builder := NewMetadataBuilder(&config.Spec)
	pack.Compilation = builder.BuildCompilationInfo(compilerVersion)
	pack.Metadata = config.Spec.Metadata

	return pack, nil
}

// CompileFromRegistry compiles ALL prompts from the registry into a single Pack.
//
// Prompts are compiled in sorted task-type order, so the output never depends
// on map order. A pack has one template_engine: it is the engine the prompts
// declare, and prompts that declare different engines are an error. A prompt
// whose engine equals the registry default counts as declaring none, because
// the registry fills that default in on every prompt that omits the field.
func (pc *PackCompiler) CompileFromRegistry(packID, compilerVersion string) (*Pack, error) {
	taskTypes := slices.Clone(pc.loader.ListTaskTypes())
	if len(taskTypes) == 0 {
		return nil, fmt.Errorf("no prompts found in registry (registry may need to be pre-loaded)")
	}
	sort.Strings(taskTypes)

	pack := pc.createEmptyPack(packID, len(taskTypes))

	var engines engineChoice
	for _, taskType := range taskTypes {
		config, err := pc.addPromptToPack(pack, taskType)
		if err != nil {
			return nil, err
		}
		engines.add(taskType, config.Spec.TemplateEngine)
	}
	engine, err := engines.resolve()
	if err != nil {
		return nil, err
	}
	pack.TemplateEngine = engine

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
		pack.Evals = ptrSlice(packEvals)
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
	if copts.skills != nil {
		pack.Skills = ptrSlice(copts.skills)
	}
	if copts.metadata != nil {
		pack.Metadata = copts.metadata
	}
	if copts.compositions != nil {
		pack.Compositions = copts.compositions
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
			// RFC 0013. Carried, never interpreted — see config.ToolSpec.
			ActionScope *packspec.ActionScope `yaml:"action_scope" json:"action_scope"`
			Extensions  map[string]any        `yaml:"extensions" json:"extensions"`
		} `yaml:"spec" json:"spec"`
	}

	if err := parseYAMLConfig(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tool YAML: %w", err)
	}

	if raw.Kind != "Tool" {
		return nil, nil // Not a tool, skip
	}

	// Convert input_schema to parameters. Keywords the spec does not name are
	// preserved by ToolParameters.Extra, so an author's full JSON Schema
	// survives the conversion.
	var params *packspec.ToolParameters
	if len(raw.Spec.InputSchema) > 0 {
		var parsed packspec.ToolParameters
		if err := json.Unmarshal(raw.Spec.InputSchema, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse input_schema as JSON: %w", err)
		}
		params = &parsed
	}

	return &PackTool{
		Name:        raw.Spec.Name,
		Description: raw.Spec.Description,
		Parameters:  params,
		ActionScope: raw.Spec.ActionScope,
		Extensions:  raw.Spec.Extensions,
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

// ConvertToolToPackTool converts a tool descriptor to a PackTool.
// This is the preferred method when tool parsing happens externally.
//
// It does not take action_scope or extensions: PackTool is the generated type,
// so a caller sets those fields on the result directly rather than growing this
// signature every time the spec adds a tool property.
func ConvertToolToPackTool(name, description string, inputSchema json.RawMessage) *PackTool {
	var params *packspec.ToolParameters
	if len(inputSchema) > 0 {
		var parsed packspec.ToolParameters
		// A tool's input schema is author-supplied JSON Schema. Anything the
		// spec does not name is preserved by ToolParameters.Extra, so a parse
		// failure here means the document is not an object at all — keep the
		// prior behavior of tolerating it rather than failing the compile.
		if err := json.Unmarshal(inputSchema, &parsed); err == nil {
			params = &parsed
		}
	}
	return &PackTool{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}

// createEmptyPack creates a new empty pack structure
func (pc *PackCompiler) createEmptyPack(packID string, promptCount int) *Pack {
	return &Pack{Pack: packspec.Pack{
		Schema:      packspec.Ptr(PromptPackSchemaURL),
		ID:          packID,
		Name:        packID,
		Version:     "v1.0.0",
		Description: fmt.Sprintf("Pack containing %d prompts", promptCount),
		Prompts:     make(map[string]*PackPrompt),
		Tools:       make(map[string]*PackTool),
		Fragments:   make(map[string]string),
	}}
}

// addPromptToPack loads a prompt config, adds it to the pack and returns it.
func (pc *PackCompiler) addPromptToPack(pack *Pack, taskType string) (*Config, error) {
	config, err := pc.loader.LoadConfig(taskType)
	if err != nil {
		return nil, fmt.Errorf("failed to load config for %s: %w", taskType, err)
	}

	pack.Prompts[taskType] = pc.createPackPrompt(config)
	if err := pc.collectFragments(pack, taskType, config); err != nil {
		return nil, err
	}

	return config, nil
}

// engineChoice settles a pack's single template_engine from its prompts'.
type engineChoice struct {
	first     *TemplateEngineInfo
	firstTask string
	differing []string
	defaulted *TemplateEngineInfo
}

// add records one prompt's engine. Callers add prompts in sorted order.
func (e *engineChoice) add(taskType string, engine *TemplateEngineInfo) {
	if engine == nil {
		return
	}
	if sameEngine(engine, defaultTemplateEngine()) {
		if e.defaulted == nil {
			e.defaulted = engine
		}
		return
	}
	if e.first == nil {
		e.first, e.firstTask = engine, taskType
		return
	}
	if !sameEngine(engine, e.first) {
		e.differing = append(e.differing,
			fmt.Sprintf("%q (%s %s)", taskType, engine.Version, engine.Syntax))
	}
}

// sameEngine compares two engines by value; an empty and an absent feature
// list are the same.
func sameEngine(a, b *TemplateEngineInfo) bool {
	return a.Version == b.Version && a.Syntax == b.Syntax && slices.Equal(a.Features, b.Features)
}

// resolve returns the declared engine, the default when none was declared, or
// an error when declared engines disagree.
func (e *engineChoice) resolve() (*TemplateEngineInfo, error) {
	if len(e.differing) > 0 {
		return nil, fmt.Errorf("prompts declare different template engines: %q uses %s %s, but %s; "+
			"a pack has one template_engine, so every prompt that sets one must set the same",
			e.firstTask, e.first.Version, e.first.Syntax, strings.Join(e.differing, ", "))
	}
	if e.first != nil {
		return e.first, nil
	}
	return e.defaulted, nil
}

// createPackPrompt creates a PackPrompt from a Config
func (pc *PackCompiler) createPackPrompt(config *Config) *PackPrompt {
	// Compile variables to their spec-exact form (defaults type to "string",
	// drops the runtime-only Binding).
	variables := ptrSlice(compileVariables(config.Spec.Variables))

	validators := ptrSlice(foldValidatorMessages(config.Spec.Validators))

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
		Evals:          ptrSlice(config.Spec.Evals),
		Validators:     validators,
		Media:          config.Spec.MediaConfig,
		TestedModels:   ptrSlice(config.Spec.TestedModels),
		ModelOverrides: ptrMap(config.Spec.ModelOverrides),
		Pipeline:       GetDefaultPipelineConfig(),
		Extensions:     config.Spec.Extensions,
	}
}

// foldValidatorMessages converts authoring-side ValidatorConfigs into the
// spec-exact compiled Validators that appear in a PromptPack. A top-level
// Message is folded into Params["message"] (the schema has no top-level message
// field on Validator); existing Params["message"] values are preserved. A nil
// Enabled defaults to true (the runtime always enforces a declared validator).
func foldValidatorMessages(validators []ValidatorConfig) []Validator {
	if len(validators) == 0 {
		return nil
	}
	out := make([]Validator, len(validators))
	for i, vc := range validators {
		params := vc.Params
		if vc.Message != "" {
			if _, exists := params["message"]; !exists {
				// Copy before folding: Params is the authoring config's map,
				// shared with the registry, and must not gain a key from
				// compiling it.
				params = maps.Clone(params)
				if params == nil {
					params = make(map[string]interface{})
				}
				params["message"] = vc.Message
			}
		}
		// enabled defaults to true when the authoring config omits it; the
		// generated type keeps it a *bool, so the resolved default is written
		// back as an explicit value rather than collapsing to the zero.
		//
		// FailOnViolation decides nothing (RFC 0015 deprecated it and
		// validators always enforce), but it is still carried rather than
		// dropped here: this is the authored pack on its way to being emitted
		// again, and silently discarding a field the author wrote — one that
		// stays schema-valid until v2.0.0 — is data loss, not tidying. It goes
		// when the spec removes it.
		out[i] = Validator{
			Type:            vc.Type,
			Enabled:         packspec.Ptr(vc.Enabled == nil || *vc.Enabled),
			FailOnViolation: vc.FailOnViolation,
			Params:          params,
			ID:              vc.ID,
			Extensions:      vc.Extensions,
		}
	}
	return out
}

// FragmentLoader is implemented by a Loader that can load a prompt's fragment
// references at compile time. *Registry implements it, through its repository.
// The pack compiler resolves fragments through it. A Loader that does not
// implement it cannot load fragments, so the compiler writes each reference as
// its "{{name}}" placeholder, as it did before it resolved fragments.
type FragmentLoader interface {
	LoadPromptFragment(taskType string, ref FragmentRef) (*Fragment, error)
}

// collectFragments resolves a prompt's fragment references into the pack's
// fragments, which the PromptPack spec defines as content resolved at compile
// time. A pack holds one fragment per name, so two prompts whose fragments of
// one name resolve to different content are an error.
//
// A required fragment that cannot be loaded is an error. An optional one that
// does not exist is written as "", as the registry renders it; one that exists
// but cannot be loaded is an error. A reference whose name or path contains a {{variable}} is chosen at
// runtime, so it cannot be resolved here: it is written as the "{{name}}"
// reference it has always been. So is every reference when the compiler's
// Loader does not implement FragmentLoader.
func (pc *PackCompiler) collectFragments(pack *Pack, taskType string, config *Config) error {
	loader, canLoad := pc.loader.(FragmentLoader)
	if !canLoad && len(config.Spec.Fragments) > 0 {
		logger.Warn("Pack compiler's loader cannot load fragments; writing them as {{name}} references",
			"prompt", taskType, "fragments", len(config.Spec.Fragments))
	}
	for _, ref := range config.Spec.Fragments {
		if !canLoad {
			addFragmentReference(pack, ref.Name)
			continue
		}
		if isDynamicFragmentRef(ref) {
			if err := addDynamicFragment(pack, loader, taskType, config, ref); err != nil {
				return err
			}
			continue
		}
		if err := addResolvedFragment(pack, loader, taskType, ref); err != nil {
			return err
		}
	}
	return nil
}

// addFragmentReference writes a fragment the compiler cannot resolve as its
// "{{name}}" reference, unless the pack already holds that name.
func addFragmentReference(pack *Pack, name string) {
	if _, exists := pack.Fragments[name]; !exists {
		pack.Fragments[name] = fmt.Sprintf("{{%s}}", name)
	}
}

// addResolvedFragment loads one static fragment reference into the pack.
//
// An optional fragment that does not exist is written as "", which is what the
// registry renders for it, so a pack consumer renders the prompt the same way
// the source does. Another prompt's fragment of that name, with content, takes
// its place: the pack holds one fragment per name.
// An optional fragment that exists but cannot be loaded fails the compile, as
// it fails the render.
func addResolvedFragment(pack *Pack, loader FragmentLoader, taskType string, ref FragmentRef) error {
	content, err := loadFragmentContent(loader, taskType, ref)
	if err != nil {
		if ref.Required {
			return fmt.Errorf("prompt %q: required fragment %q: %w", taskType, ref.Name, err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prompt %q: optional fragment %q exists but cannot be loaded: %w",
				taskType, ref.Name, err)
		}
		if _, exists := pack.Fragments[ref.Name]; !exists {
			pack.Fragments[ref.Name] = ""
		}
		return nil
	}
	if existing, exists := pack.Fragments[ref.Name]; exists && existing != "" && existing != content {
		return fmt.Errorf("prompt %q: fragment %q resolves to different content than the fragment of that "+
			"name the pack already holds (from this prompt or another); a pack holds one fragment per name",
			taskType, ref.Name)
	}
	pack.Fragments[ref.Name] = content
	return nil
}

// fragmentNameVarPattern matches a {{variable}} in a fragment reference's name
// or path.
// It matches exactly what the runtime substitutes in a fragment name or path
// (FragmentResolver.resolveVariables): "{{name}}", with no spaces.
var fragmentNameVarPattern = regexp.MustCompile(`\{\{([A-Za-z0-9_.]+)\}\}`)

// maxDynamicFragmentCandidates bounds how many candidates one variable-chosen
// fragment compiles to, so a few large enums cannot blow up the pack.
const maxDynamicFragmentCandidates = 256

// addDynamicFragment writes a fragment reference a variable chooses. When every
// variable in its name and path declares the values it takes
// (validation.enum), each candidate is loaded and written under its resolved
// name, so the pack carries every fragment the reference can choose and a
// render resolves it from the variable, whichever source sets it. Otherwise
// the values are unknown at compile time and the pack, whose fragments are
// static content, cannot hold the candidates: the reference is written as its
// "{{name}}" placeholder, as before, and a render fails on it unless the
// caller supplies the chosen fragment's content.
func addDynamicFragment(pack *Pack, loader FragmentLoader, taskType string, config *Config, ref FragmentRef) error {
	// A pack holds one fragment per name, so only a name that varies with the
	// variable can carry a candidate per value. A fixed name over a variable
	// path stays its reference: writing every candidate under the one name
	// would conflict, or, with one candidate present, serve it for every value.
	if !nameCarriesPathVars(ref) {
		logger.Warn("Fragment whose path a variable chooses that its name does not; the pack holds only its "+
			"reference. Put every path variable in the fragment's name to compile every candidate.",
			"prompt", taskType, "fragment", ref.Name, "path", ref.Path)
		addFragmentReference(pack, ref.Name)
		return nil
	}
	combos, ok := enumCombinations(config, ref)
	if !ok {
		logger.Warn("Fragment chosen by a variable without declared values (or with more than "+
			strconv.Itoa(maxDynamicFragmentCandidates)+" combinations); the pack holds only its reference. "+
			"Declare validation.enum on the variable to compile every candidate.",
			"prompt", taskType, "fragment", ref.Name)
		addFragmentReference(pack, ref.Name)
		return nil
	}
	for _, vars := range combos {
		resolved := FragmentRef{
			Name:     substituteFragmentVars(ref.Name, vars),
			Path:     substituteFragmentVars(ref.Path, vars),
			Required: ref.Required,
		}
		if err := addResolvedFragment(pack, loader, taskType, resolved); err != nil {
			return err
		}
	}
	return nil
}

// nameCarriesPathVars reports whether ref's name holds a variable and every
// variable of its path, so each candidate has a name of its own.
func nameCarriesPathVars(ref FragmentRef) bool {
	nameVars := map[string]bool{}
	for _, m := range fragmentNameVarPattern.FindAllStringSubmatch(ref.Name, -1) {
		nameVars[m[1]] = true
	}
	if len(nameVars) == 0 {
		return false
	}
	for _, m := range fragmentNameVarPattern.FindAllStringSubmatch(ref.Path, -1) {
		if !nameVars[m[1]] {
			return false
		}
	}
	return true
}

// enumCombinations returns every assignment of the variables in ref's name
// and path to their declared enum values, or false when one declares none.
func enumCombinations(config *Config, ref FragmentRef) ([]map[string]string, bool) {
	var names []string
	seen := map[string]bool{}
	for _, m := range fragmentNameVarPattern.FindAllStringSubmatch(ref.Name+" "+ref.Path, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	combos := []map[string]string{{}}
	for _, name := range names {
		values := variableEnum(config, name)
		if len(values) == 0 || slices.ContainsFunc(values, func(v string) bool { return !IsPlainFragmentValue(v) }) {
			// No values, or one the runtime would refuse to put in a path.
			return nil, false
		}
		if len(combos)*len(values) > maxDynamicFragmentCandidates {
			return nil, false
		}
		var next []map[string]string
		for _, c := range combos {
			for _, v := range values {
				m := maps.Clone(c)
				m[name] = v
				next = append(next, m)
			}
		}
		combos = next
	}
	return combos, true
}

// variableEnum returns the declared enum values of the prompt variable name.
func variableEnum(config *Config, name string) []string {
	for _, v := range config.Spec.Variables {
		if v.Name != name || v.Validation == nil {
			continue
		}
		raw, ok := v.Validation["enum"].([]interface{})
		if !ok {
			return nil
		}
		out := make([]string, 0, len(raw))
		for _, e := range raw {
			out = append(out, enumValueString(e))
		}
		return out
	}
	return nil
}

// enumValueString formats an enum value as a variable carrying it renders: a
// JSON number decodes as float64, and %v would write 1000000 as 1e+06.
func enumValueString(v interface{}) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

// substituteFragmentVars replaces each {{variable}} in s with its value.
func substituteFragmentVars(s string, vars map[string]string) string {
	return fragmentNameVarPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := fragmentNameVarPattern.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// isDynamicFragmentRef reports whether a fragment reference is chosen by a
// template variable, and so cannot be resolved at compile time.
func isDynamicFragmentRef(ref FragmentRef) bool {
	return strings.Contains(ref.Name, "{{") || strings.Contains(ref.Path, "{{")
}

func loadFragmentContent(loader FragmentLoader, taskType string, ref FragmentRef) (string, error) {
	frag, err := loader.LoadPromptFragment(taskType, ref)
	if err != nil {
		return "", err
	}
	if frag == nil {
		return "", fmt.Errorf("fragment not found: %w", fs.ErrNotExist)
	}
	return frag.Content, nil
}

// getCurrentTimestamp returns the current timestamp in RFC3339 format
func (pc *PackCompiler) getCurrentTimestamp() string {
	return pc.timeProvider.Now().Format(time.RFC3339)
}

// LoadPack loads a pack from a JSON file
func LoadPack(filePath string) (*Pack, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // G304: the caller names the pack file to load
	if err != nil {
		return nil, fmt.Errorf("failed to read pack file: %w", err)
	}

	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("failed to parse pack JSON: %w", err)
	}

	return &pack, nil
}

// Validate validates a pack format and returns its findings in one list,
// whatever their severity: ValidateDetailed's errors followed by its warnings.
// Among the errors are the eval definitions evals.ValidateEvals rejects, at
// pack level and in each prompt.
//
// It leaves out two kinds of warning ValidateDetailed reports: undeclared
// prompt tools, and eval triggers this runtime does not know. Callers have
// treated every Validate finding as blocking, and both are legitimate in a
// valid pack: a host may supply the tool, and the schema leaves trigger open.
// Use ValidateDetailed to see them and to tell errors from warnings.
func (p *Pack) Validate() []string {
	r := p.validateDetailed(false)
	return append(r.Errors, r.Warnings...)
}

// PackValidationResult is ValidateDetailed's result. Errors are findings that
// make the pack unusable or schema-invalid; Warnings are findings a valid pack
// may still have.
type PackValidationResult struct {
	Errors   []string
	Warnings []string
}

// ValidateDetailed runs Validate's checks, plus a check of each prompt's tools
// against the pack's tools, and splits the findings by severity. Three are
// warnings rather than errors:
//   - a missing compilation block, which the PromptPack schema does not require;
//   - a prompt tool the pack's tools do not define. The schema asks for every
//     prompt tool to be defined there, but a host may supply tools a pack does
//     not carry (MCP servers, workflow and memory tools, skills);
//   - an eval trigger this runtime does not know. The schema lists trigger
//     values as examples, not an enum, but the SDK skips such an eval.
//
// Workflow, agents and governance findings keep the severity their own
// validators give them.
func (p *Pack) ValidateDetailed() *PackValidationResult {
	return p.validateDetailed(true)
}

func (p *Pack) validateDetailed(detailed bool) *PackValidationResult {
	r := &PackValidationResult{Errors: []string{}, Warnings: []string{}}
	r.Errors = append(r.Errors, p.validatePackFields()...)
	r.Errors = append(r.Errors, p.validateTemplateEngine()...)
	r.Errors = append(r.Errors, p.validatePrompts()...)
	if detailed {
		r.Warnings = append(r.Warnings, p.validatePromptTools()...)
	}
	r.Warnings = append(r.Warnings, p.validateCompilation()...)
	evalErrs, unknownTriggers := p.validateEvals()
	r.Errors = append(r.Errors, evalErrs...)
	if detailed {
		r.Warnings = append(r.Warnings, unknownTriggers...)
	}
	if p.Workflow != nil {
		wf := p.ValidateWorkflow()
		r.Errors = append(r.Errors, wf.Errors...)
		r.Warnings = append(r.Warnings, wf.Warnings...)
	}
	if p.Agents != nil {
		errs, warns := p.ValidateAgents()
		r.Errors = append(r.Errors, errs...)
		r.Warnings = append(r.Warnings, warns...)
	}
	gov := p.ValidateGovernance()
	r.Errors = append(r.Errors, gov.Errors...)
	r.Warnings = append(r.Warnings, gov.Warnings...)
	return r
}

// validatePromptTools reports each tool a prompt names that the pack's tools
// do not define, in sorted prompt order.
func (p *Pack) validatePromptTools() []string {
	var warnings []string
	for _, taskType := range p.ListPrompts() {
		prompt := p.Prompts[taskType]
		if prompt == nil {
			continue
		}
		for _, tool := range prompt.Tools {
			if _, ok := p.Tools[tool]; !ok {
				warnings = append(warnings, fmt.Sprintf(
					"prompt '%s': tool '%s' is not defined in the pack's tools", taskType, tool))
			}
		}
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

// ValidateCompositions validates the pack's compositions (RFC 0010): each
// composition's internal rules + reference resolution against the pack's
// prompts/tools/evals, plus pack-level cross-checks against the workflow.
func (p *Pack) ValidateCompositions() *composition.ValidationResult {
	avail := composition.Available{
		Prompts: p.ListPrompts(),
		Tools:   p.toolKeys(),
		Evals:   p.evalKeys(),
	}
	res := composition.ValidateAll(p.Compositions, avail)

	referenced := map[string]bool{}
	if p.Workflow != nil {
		for name, st := range p.Workflow.States {
			if workflow.OrchestrationOf(st) != workflow.OrchestrationComposition {
				continue
			}
			referenced[st.Composition] = true
			if _, ok := p.Compositions[st.Composition]; !ok {
				res.Errors = append(res.Errors, fmt.Sprintf(
					"workflow state %q references unknown composition %q", name, st.Composition))
			}
		}
	}
	if len(p.Compositions) > 0 && p.Workflow == nil {
		// No workflow at all: one banner covers every composition; skip the
		// redundant per-composition "unreferenced" warnings below.
		res.Warnings = append(res.Warnings, "compositions defined but no workflow references them")
		return res
	}
	for name := range p.Compositions {
		if !referenced[name] {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("composition %q is not referenced by any workflow state", name))
		}
	}
	return res
}

// toolKeys returns the pack's tool names.
func (p *Pack) toolKeys() []string {
	keys := make([]string, 0, len(p.Tools))
	for k := range p.Tools {
		keys = append(keys, k)
	}
	return keys
}

// evalKeys returns the pack's eval IDs.
func (p *Pack) evalKeys() []string {
	keys := make([]string, 0, len(p.Evals))
	for i := range p.Evals {
		keys = append(keys, p.Evals[i].ID)
	}
	return keys
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
	for _, taskType := range p.ListPrompts() {
		warnings = append(warnings, validatePrompt(taskType, p.Prompts[taskType], p.Fragments)...)
	}
	return warnings
}

// validatePrompt validates a single prompt. fragments are the pack's
// fragments: a placeholder that names one is filled from it, not from a
// variable, so it does not need a declared variable.
func validatePrompt(taskType string, prompt *PackPrompt, fragments map[string]string) []string {
	warnings := []string{}
	if prompt.SystemTemplate == "" {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': missing system_template", taskType))
	}
	unfilled := expandFragmentPlaceholders(prompt.SystemTemplate, fragments)
	if len(prompt.Variables) == 0 && strings.Contains(unfilled, "{{") {
		msg := "prompt '%s': system_template references variables, but none are declared"
		warnings = append(warnings, fmt.Sprintf(msg, taskType))
	}
	if prompt.Version == "" {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': missing version", taskType))
	} else if err := validateSemanticVersion(prompt.Version); err != nil {
		warnings = append(warnings, fmt.Sprintf("prompt '%s': invalid version '%s': %v", taskType, prompt.Version, err))
	}
	return warnings
}

// expandFragmentPlaceholders returns text with the "{{name}}" placeholder of
// each of the pack's fragments replaced by the fragment's content, so a
// variable inside a fragment still counts as one the template references. A
// fragment whose name holds a variable ("persona_{{region}}") is left in:
// choosing it needs that variable. Expansion stops after one pass per
// fragment, so fragments that reference each other cannot loop.
func expandFragmentPlaceholders(text string, fragments map[string]string) string {
	names := slices.Sorted(maps.Keys(fragments))
	for range len(names) + 1 {
		before := text
		for _, name := range names {
			if !strings.Contains(name, "{{") && fragments[name] != "{{"+name+"}}" {
				text = strings.ReplaceAll(text, "{{"+name+"}}", fragments[name])
			}
		}
		if text == before {
			break
		}
	}
	return text
}

// validateEvals runs evals.ValidateEvals over the pack-level evals (scope
// "pack") and then over each prompt's evals (scope "prompt:<task type>"), in
// task-type order. Each scope is checked on its own: a prompt eval that shares
// a pack eval's id overrides it rather than duplicating it.
//
// A trigger this runtime does not know comes back on its own, in
// unknownTriggers: the PromptPack schema lists trigger values as examples, not
// an enum, so such a pack is spec-valid.
func (p *Pack) validateEvals() (errs, unknownTriggers []string) {
	errs, unknownTriggers = splitEvalFindings(evals.Values(p.Evals), "pack")
	for _, name := range slices.Sorted(maps.Keys(p.Prompts)) {
		if pp := p.Prompts[name]; pp != nil {
			e, u := splitEvalFindings(evals.Values(pp.Evals), "prompt:"+name)
			errs = append(errs, e...)
			unknownTriggers = append(unknownTriggers, u...)
		}
	}
	return errs, unknownTriggers
}

// splitEvalFindings is evals.ValidateEvals with the warnings (a trigger this
// runtime does not know) split out from the errors, by each finding's severity.
func splitEvalFindings(defs []evals.EvalDef, scope string) (errs, unknownTriggers []string) {
	for _, f := range evals.ValidateEvalsDetailed(defs, scope) {
		if f.Severity == evals.EvalFindingWarning {
			unknownTriggers = append(unknownTriggers, f.Message)
		} else {
			errs = append(errs, f.Message)
		}
	}
	return errs, unknownTriggers
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
func (p *Pack) ValidateAgents() (errs, warnings []string) {
	if p.Agents == nil {
		return nil, nil
	}

	// Members must be non-empty
	if len(p.Agents.Members) == 0 {
		errs = append(errs, "agents: members must not be empty")
		return errs, warnings
	}

	// Entry must reference a key in Members
	if _, ok := p.Agents.Members[p.Agents.Entry]; !ok {
		errs = append(errs, fmt.Sprintf("agents: entry %q does not reference a valid member", p.Agents.Entry))
	}

	// All member keys must reference valid keys in Pack.Prompts
	for key := range p.Agents.Members {
		if _, ok := p.Prompts[key]; !ok {
			errs = append(errs, fmt.Sprintf("agents: member %q does not reference a valid prompt", key))
		}
	}

	// RFC 0011: state-backed agents must reference a valid workflow state and
	// require a top-level workflow.
	for key, agent := range p.Agents.Members {
		errs = append(errs, validateAgentState(p, key, agent)...)
	}

	// Validate individual agent definitions
	for key, agent := range p.Agents.Members {
		warnings = append(warnings, validateAgentDef(key, agent)...)
	}

	return errs, warnings
}

// validateAgentState checks RFC 0011 rules for a state-backed agent member:
// the referenced state must exist in the pack's workflow, which must be present.
func validateAgentState(p *Pack, key string, agent *AgentDef) []string {
	if agent.State == "" {
		return nil
	}
	if p.Workflow == nil {
		return []string{fmt.Sprintf(
			"agents: member %q sets state %q but requires a top-level workflow", key, agent.State)}
	}
	if _, ok := p.Workflow.States[agent.State]; !ok {
		return []string{fmt.Sprintf(
			"agents: member %q state %q does not reference a valid workflow state", key, agent.State)}
	}
	return nil
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

// ListPrompts returns all prompt task types in the pack, sorted.
func (p *Pack) ListPrompts() []string {
	prompts := make([]string, 0, len(p.Prompts))
	for taskType := range p.Prompts {
		prompts = append(prompts, taskType)
	}
	sort.Strings(prompts)
	return prompts
}

// GetTool returns a specific tool by name, or nil if not found.
func (p *Pack) GetTool(name string) *PackTool {
	if p.Tools == nil {
		return nil
	}
	return p.Tools[name]
}

// ListTools returns all tool names defined in the pack, sorted.
func (p *Pack) ListTools() []string {
	if p.Tools == nil {
		return nil
	}
	names := make([]string, 0, len(p.Tools))
	for name := range p.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ToConfig converts a pack prompt into a prompt.Config suitable for
// registration in a prompt.Registry. It carries the fields the prompt-assembly
// pipeline needs; tools and validators are wired separately by the caller.
// Extensions are deep-copied, so a pack → config → pack round trip keeps them
// and editing the Config never reaches the pack.
func ToConfig(pr *PackPrompt, taskType string) *Config {
	var vars []VariableMetadata
	if len(pr.Variables) > 0 {
		vars = make([]VariableMetadata, len(pr.Variables))
		for i, v := range pr.Variables {
			vars[i] = VariableToMetadata(*v)
		}
	}
	return &Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: Spec{
			TaskType:       taskType,
			Version:        pr.Version,
			Description:    pr.Description,
			SystemTemplate: pr.SystemTemplate,
			AllowedTools:   pr.Tools,
			ToolPolicy:     pr.ToolPolicy,
			Variables:      vars,
			ModelOverrides: valueMap(pr.ModelOverrides),
			Parameters:     packspec.Clone(pr.Parameters),
			Extensions:     copyAnyMap(pr.Extensions),
		},
	}
}

// PromptConfig converts the pack's prompt for taskType into a Config for a
// prompt.Registry, as ToConfig does, and gives it a reference to each of the
// pack's fragments. The PromptPack spec defines a pack's fragments as
// content any of its prompts can reference, so the registry renders a
// prompt's "{{name}}" placeholder from the fragment of that name; register
// the fragments with the registry's repository by name. It returns nil when
// the pack has no prompt for taskType.
//
// A fragment whose name holds a variable (persona_{{region}}) gets no
// reference: a render resolves the name to one the pack does not hold, so its
// placeholder is left for the caller's variables to fill, and fails the render
// when nothing does. Neither does a fragment named like a variable the prompt
// declares: the variable wins.
func (p *Pack) PromptConfig(taskType string) *Config {
	pr := p.GetPrompt(taskType)
	if pr == nil {
		return nil
	}
	config := ToConfig(pr, taskType)
	config.Spec.Fragments = p.fragmentRefs(pr)
	return config
}

// fragmentRefs returns a required, name-only reference to each of the pack's
// fragments, in name order, except:
//   - a fragment whose name holds a variable: the name a render resolves it
//     to is not one the pack holds;
//   - a fragment the compiler wrote as its own "{{name}}" reference;
//   - a fragment named like one of the prompt's declared variables, so the
//     variable the prompt declares keeps its default and its caller's value.
func (p *Pack) fragmentRefs(pr *PackPrompt) []FragmentRef {
	declared := make(map[string]bool, len(pr.Variables))
	for _, v := range pr.Variables {
		declared[v.Name] = true
	}
	var refs []FragmentRef
	for _, name := range slices.Sorted(maps.Keys(p.Fragments)) {
		if strings.Contains(name, "{{") || p.Fragments[name] == "{{"+name+"}}" || declared[name] {
			continue
		}
		refs = append(refs, FragmentRef{Name: name, Required: true})
	}
	return refs
}

// compileVariables converts authoring variables into their spec-exact compiled
// form: an empty type defaults to "string", and Binding is dropped (variable
// binding is a runtime concern, not part of the portable pack).
func compileVariables(vars []VariableMetadata) []Variable {
	if len(vars) == 0 {
		return nil
	}
	out := make([]Variable, len(vars))
	for i, v := range vars {
		t := v.Type
		if t == "" {
			t = "string"
		}
		out[i] = Variable{
			Name:        v.Name,
			Type:        t,
			Required:    v.Required,
			Default:     v.Default,
			Description: v.Description,
			Example:     v.Example,
			Validation:  validationFromMap(v.Validation),
		}
	}
	return out
}

// ptrSlice converts a value slice to the pointer slice the generated types use
// for optional object arrays.
func ptrSlice[T any](in []T) []*T {
	if in == nil {
		return nil
	}
	out := make([]*T, len(in))
	for i := range in {
		v := in[i]
		out[i] = &v
	}
	return out
}

// ptrMap is ptrSlice for string-keyed maps.
// valueMap is the inverse of ptrMap; nil entries are dropped.
func valueMap[T any](in map[string]*T) map[string]T {
	if in == nil {
		return nil
	}
	out := make(map[string]T, len(in))
	for k, v := range in {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

func ptrMap[T any](in map[string]T) map[string]*T {
	if in == nil {
		return nil
	}
	out := make(map[string]*T, len(in))
	for k := range in {
		v := in[k]
		out[k] = &v
	}
	return out
}

// VariableToMetadata converts a compiled variable back to the authoring
// metadata form. Binding is always nil — it is not carried in the pack.
//
// A free function rather than a method: Variable is the generated type now, and
// a type alias cannot carry methods.
func VariableToMetadata(v Variable) VariableMetadata {
	return VariableMetadata{
		Name:        v.Name,
		Type:        v.Type,
		Required:    v.Required,
		Default:     v.Default,
		Description: v.Description,
		Example:     v.Example,
		Validation:  validationToMap(v.Validation),
	}
}

// validationToMap renders the generated validation struct back into the
// authoring map form. The authoring type predates the typed shape and is
// consumed as a map by the template layer.
func validationToMap(v *VariableValidation) map[string]any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validationFromMap is the inverse, for compiling an authoring variable.
func validationFromMap(m map[string]any) *VariableValidation {
	if len(m) == 0 {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var out VariableValidation
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
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
