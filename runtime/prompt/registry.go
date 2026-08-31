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
//   - Runtime pipeline: https://promptkit.altairalabs.ai/architecture/runtime-pipeline/
//   - System overview: https://promptkit.altairalabs.ai/architecture/system-overview/
//
// # Usage
//
// Create a registry with a repository (config-first pattern):
//
//	repo := memory.NewRepository()
//	registry := prompt.NewRegistryWithRepository(repo)
//	assembled := registry.LoadWithVars("task_type", vars, "gpt-4")
//
// See package github.com/AltairaLabs/PromptKit/sdk for higher-level APIs.
package prompt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/template"
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

// Template holds an unrendered prompt template with all metadata required
// to render it later. This is the intermediate form produced by LoadTemplate,
// consumed by the TemplateStage to perform deferred variable substitution.
type Template struct {
	TaskType      string            `json:"task_type"`
	RawTemplate   string            `json:"raw_template"`
	DefaultVars   map[string]string `json:"default_vars,omitempty"`
	RequiredVars  []string          `json:"required_vars,omitempty"`
	FragmentVars  map[string]string `json:"fragment_vars,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Validators    []ValidatorConfig `json:"validators,omitempty"`
	ModelOverride string            `json:"model_override,omitempty"`
}

// UsesTools returns true if this prompt has tools configured
func (ap *AssembledPrompt) UsesTools() bool {
	return len(ap.AllowedTools) > 0
}

// Config represents a YAML prompt configuration file in K8s-style manifest format
type Config struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Spec       Spec              `yaml:"spec" json:"spec"`
}

// GetTaskType returns the task type from the prompt config
func (c *Config) GetTaskType() string {
	return c.Spec.TaskType
}

// GetAllowedTools returns the allowed tools from the prompt config
func (c *Config) GetAllowedTools() []string {
	return c.Spec.AllowedTools
}

// Spec contains the actual prompt configuration
type Spec struct {
	TaskType       string                   `yaml:"task_type" json:"task_type"`
	Version        string                   `yaml:"version" json:"version"`
	Description    string                   `yaml:"description" json:"description"`
	TemplateEngine *TemplateEngineInfo      `yaml:"template_engine,omitempty" json:"template_engine,omitempty"` // Template engine configuration
	Fragments      []FragmentRef            `yaml:"fragments,omitempty" json:"fragments,omitempty"`             // New: fragment assembly
	SystemTemplate string                   `yaml:"system_template" json:"system_template"`
	Variables      []VariableMetadata       `yaml:"variables,omitempty" json:"variables,omitempty"` // Variable definitions with rich metadata
	ModelOverrides map[string]ModelOverride `yaml:"model_overrides,omitempty" json:"model_overrides,omitempty"`
	AllowedTools   []string                 `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"` // Tools this prompt can use
	MediaConfig    *MediaConfig             `yaml:"media,omitempty" json:"media,omitempty"`                 // Multimodal media configuration
	Validators     []ValidatorConfig        `yaml:"validators,omitempty" json:"validators,omitempty"`       // Validators/Guardrails for production runtime
	TestedModels   []ModelTestResultRef     `yaml:"tested_models,omitempty" json:"tested_models,omitempty"` // Model testing metadata
	ToolPolicy     *ToolPolicyPack          `yaml:"tool_policy,omitempty" json:"tool_policy,omitempty"`
	Parameters     *ParametersPack          `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Evals          []evals.EvalDef          `yaml:"evals,omitempty" json:"evals,omitempty"`
	Metadata       *Metadata                `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Compilation    *CompilationInfo         `yaml:"compilation,omitempty" json:"compilation,omitempty"`
}

// ModelTestResultRef is a simplified reference to model test results
// The full ModelTestResult type is in pkg/engine for tracking test execution
// ModelTestResultRef is the Go form of the spec's $defs/TestedModel. It is
// pinned to that def by TestedModelStructMatchesPromptPackSpec.
//
// provider, model and date are required by the spec, so they carry no omitempty
// — a required field that vanishes on serialize produces a pack that fails its
// own validation.
// Generated from the schema: an ALIAS for packspec.TestedModel.
// avg_tokens/avg_latency_ms are float64: the spec types them as number, not integer.
type ModelTestResultRef = packspec.TestedModel

// MediaConfig configures multimodal support for a prompt.
//
// Generated. It was hand-written, and dropped $defs/MediaConfig's `document`
// property entirely — a prompt declaring document media round-tripped to
// nothing. Same failure as metadata.governance, same fix.
type MediaConfig = packspec.MediaConfig

// DocumentConfig configures document media (PDFs, CAD files, spreadsheets).
// Reachable now that MediaConfig is the generated type.
type DocumentConfig = packspec.DocumentConfig

// ImageConfig contains image-specific configuration
// Generated from the schema: an ALIAS for packspec.ImageConfig.
// default_detail is *string: the spec defaults it to "auto", so absent and empty differ.
type ImageConfig = packspec.ImageConfig

// AudioConfig contains audio-specific configuration
// AudioConfig is generated from the schema: an ALIAS for packspec.AudioConfig,
// not a copy. The hand-written struct was field-for-field identical to
// $defs/AudioConfig.
type AudioConfig = packspec.AudioConfig

// VideoConfig contains video-specific configuration
// Generated from the schema: an ALIAS for packspec.VideoConfig.
// Optional numeric and boolean fields are pointers: zero is a real setting.
type VideoConfig = packspec.VideoConfig

// MultimodalExample is a few-shot example carrying media.
//
// Generated, so that MediaConfig.Examples is the generated slice type.
type MultimodalExample = packspec.MultimodalExample

// ExampleContentPart is one content part of a multimodal example.
//
// Generated. This is $defs/ContentPart, the PACK authoring type — distinct from
// types.ContentPart, which is the runtime message type and a different graph.
type ExampleContentPart = packspec.ContentPart

// ExampleMedia is a media reference inside a multimodal example.
//
// Generated. The hand-written version was identical property-for-property
// except that it lacked `base64`, so a pack embedding media inline lost it.
//
// Note the Go field is MimeType, not MIMEType: the generator derives names from
// the schema, and renaming it by hand would put this type back outside the
// generated guarantee for the sake of two characters.
type ExampleMedia = packspec.MediaReference

// ValidatorConfig describes a validator/guardrail configuration from a prompt pack.
type ValidatorConfig struct {
	Type   string                 `yaml:"type" json:"type"`
	Params map[string]interface{} `yaml:"params" json:"params"`
	// Enable/disable validator (default: true)
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// FailOnViolation is part of the PromptPack spec but is ignored by
	// this runtime — guardrails always enforce. Authors wanting
	// observe-only behavior should declare an eval and assert on it
	// instead. Tracked upstream:
	// https://github.com/AltairaLabs/promptpack-spec/issues/46
	FailOnViolation *bool `yaml:"fail_on_violation,omitempty" json:"fail_on_violation,omitempty"`
	// User-facing message shown when content is blocked (default: DefaultBlockedMessage)
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
}

// Validator is a compiled pack validator.
//
// Generated. It carries the spec's `message`, which the COMPILED form does not
// use — foldValidatorMessages folds it into params at compile time, so nothing
// populates it and omitempty keeps it out of the emitted pack. Carrying an
// unused field is cheaper than maintaining a second definition of this type.
type Validator = packspec.Validator

// DefaultBlockedMessage is the user-facing message shown when a content guardrail blocks output.
const DefaultBlockedMessage = "Sorry, we can't provide this response as it would violate our content policy."

// TemplateEngineInfo is the pack's template engine configuration.
//
// Generated. template_engine is an inline object under the root's properties
// rather than a $def, but the generator emits types for those too — this is
// packspec.PackTemplateEngine, and it was field-for-field identical to the
// hand-written struct it replaced.
type TemplateEngineInfo = packspec.PackTemplateEngine

// VariableBindingKind defines the type of resource a variable binds to.
type VariableBindingKind string

const (
	// BindingKindProject binds to project metadata (name, description, tags).
	BindingKindProject VariableBindingKind = "project"
	// BindingKindProvider binds to provider/model selection.
	BindingKindProvider VariableBindingKind = "provider"
	// BindingKindWorkspace binds to current workspace (name, namespace).
	BindingKindWorkspace VariableBindingKind = "workspace"
	// BindingKindSecret binds to Kubernetes Secret resources.
	BindingKindSecret VariableBindingKind = "secret"
	// BindingKindConfigMap binds to Kubernetes ConfigMap resources.
	BindingKindConfigMap VariableBindingKind = "configmap"
)

// VariableBindingFilter specifies criteria for filtering bound resources.
type VariableBindingFilter struct {
	// Capability filters resources by capability (e.g., "chat", "embeddings").
	Capability string `yaml:"capability,omitempty" json:"capability,omitempty"`
	// Labels filters resources by label selectors.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// VariableBinding defines how a variable binds to system resources.
// This enables automatic population from system resources and type-safe UI selection.
type VariableBinding struct {
	// Kind specifies the type of resource to bind to.
	Kind VariableBindingKind `yaml:"kind" json:"kind"`
	// Field specifies which field of the resource to bind (e.g., "name", "model").
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	// AutoPopulate enables automatic population of this variable from the bound resource.
	// When true, the variable may be auto-filled and optionally hidden from the wizard.
	AutoPopulate bool `yaml:"autoPopulate,omitempty" json:"autoPopulate,omitempty"`
	// Filter specifies criteria for filtering bound resources.
	Filter *VariableBindingFilter `yaml:"filter,omitempty" json:"filter,omitempty"`
}

// VariableMetadata contains enhanced metadata for a variable
// VariableMetadata defines a template variable with validation rules
// This struct matches the SDK Variable type for PromptPack spec compliance
type VariableMetadata struct {
	Name        string                 `yaml:"name" json:"name"`
	Type        string                 `yaml:"type,omitempty" json:"type,omitempty"` // "string", "number", "boolean", "object", "array"
	Required    bool                   `yaml:"required" json:"required"`
	Default     interface{}            `yaml:"default,omitempty" json:"default,omitempty"`
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Example     interface{}            `yaml:"example,omitempty" json:"example,omitempty"`
	Validation  map[string]interface{} `yaml:"validation,omitempty" json:"validation,omitempty"`
	// Binding enables automatic population from system resources and type-safe UI selection.
	// This allows prompts to declare semantic meaning for variables beyond just their data type.
	Binding *VariableBinding `yaml:"binding,omitempty" json:"binding,omitempty"`
}

// Variable is a spec-exact compiled prompt template variable.
//
// Generated. It deliberately omits nothing of its own: Binding IS on the
// generated type, but compileVariables never populates it, because variable
// binding (auto-population from platform resources) is a runtime concern
// resolved at compile time and not part of the portable pack. The authoring
// type VariableMetadata carries it; omitempty keeps it out of the emitted pack.
//
// toMetadata() was a method. A type alias cannot carry methods, so it is
// VariableToMetadata below.
type Variable = packspec.Variable

// ValidatorValues dereferences a slice of validator pointers into values, at
// the boundary between the generated Prompt (which holds pointers) and the
// APIs that take values. A nil entry is skipped.
func ValidatorValues(in []*Validator) []Validator {
	if in == nil {
		return nil
	}
	out := make([]Validator, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}

// VariableValidation is the validation rule set on a Variable.
type VariableValidation = packspec.VariableValidation

// Metadata contains additional metadata for the pack format.
//
// This is the generated type: metadata is where the PromptPack spec puts the
// facts a pack declares ABOUT itself rather than about its execution, so it is
// where formalization lands as the spec grows — v1.6.0 added `governance`
// (RFC 0013: accountable owner, autonomy level, risk classification), and more
// of that shape is coming. A hand-written struct here dropped `governance`
// silently on load: the field validated, round-tripped clean, and vanished.
//
// Generated means new spec properties arrive by regeneration, and
// `make packspec-check` fails if they have not.
//
// Performance and Changelog are NOT spec properties. They live in Extra, which
// the spec permits (metadata is additionalProperties:true), and are reached
// through the accessors below rather than as struct fields.
type Metadata = packspec.PackMetadata

// MetadataPerformance returns the performance benchmarks a pack declares, or nil.
//
// performance is not a PromptPack property — it is a PromptKit extension carried
// in the metadata envelope the spec leaves open. Keeping the type assertion here
// rather than at each call site is what stops a silent nil creeping in; a value
// of the wrong shape yields nil rather than a half-populated struct.
func MetadataPerformance(m *Metadata) *PerformanceMetrics {
	if m == nil {
		return nil
	}
	return decodeExtra[PerformanceMetrics](m.Extra, "performance")
}

// SetMetadataPerformance stores performance benchmarks in the metadata envelope.
// A nil value removes the key rather than writing a null, so a pack does not
// gain a meaningless `"performance": null`.
func SetMetadataPerformance(m *Metadata, p *PerformanceMetrics) {
	if m == nil {
		return
	}
	m.Extra = setExtra(m.Extra, "performance", p)
}

// MetadataChangelog returns the version history a pack declares, or nil.
// See MetadataPerformance for why this is not a struct field.
func MetadataChangelog(m *Metadata) []ChangelogEntry {
	if m == nil {
		return nil
	}
	entries := decodeExtra[[]ChangelogEntry](m.Extra, "changelog")
	if entries == nil {
		return nil
	}
	return *entries
}

// SetMetadataChangelog stores the version history in the metadata envelope.
// An empty changelog removes the key.
func SetMetadataChangelog(m *Metadata, entries []ChangelogEntry) {
	if m == nil {
		return
	}
	if len(entries) == 0 {
		m.Extra = setExtra(m.Extra, "changelog", nil)
		return
	}
	m.Extra = setExtra(m.Extra, "changelog", entries)
}

// decodeExtra pulls a typed value out of an Extra bag.
//
// Extra holds whatever JSON or YAML decoding produced — map[string]any after a
// load, but the concrete Go value when something set it in memory this run. So
// this handles both: a direct type assertion first, then a JSON round-trip for
// the decoded-document case. Anything that fits neither yields nil.
func decodeExtra[T any](extra map[string]any, key string) *T {
	raw, present := extra[key]
	if !present || raw == nil {
		return nil
	}
	if typed, ok := raw.(*T); ok {
		return typed
	}
	if typed, ok := raw.(T); ok {
		return &typed
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

// setExtra returns extra with key set to value, allocating the map if needed.
// A nil value (including a typed nil pointer) deletes the key instead, so a
// pack does not gain a meaningless `"performance": null`.
func setExtra(extra map[string]any, key string, value any) map[string]any {
	if isNil(value) {
		delete(extra, key)
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra[key] = value
	return extra
}

// isNil reports whether value is nil, including a non-nil interface holding a
// nil pointer — which is what a caller passing a (*PerformanceMetrics)(nil)
// produces, and what a plain `value == nil` misses.
func isNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// CostEstimate provides estimated costs for prompt execution.
//
// Generated, for the same reason as Metadata. Note the fields are *float64:
// the spec makes all three optional, and a plain float64 cannot tell "no
// estimate" from "estimated at zero".
type CostEstimate = packspec.PackMetadataCostEstimate

// PerformanceMetrics provides performance benchmarks
type PerformanceMetrics struct {
	// Average latency in milliseconds
	AvgLatencyMs int `yaml:"avg_latency_ms" json:"avg_latency_ms"`
	// 95th percentile latency
	P95LatencyMs int `yaml:"p95_latency_ms" json:"p95_latency_ms"`
	// Average tokens used
	AvgTokens int `yaml:"avg_tokens" json:"avg_tokens"`
	// Success rate (0.0-1.0)
	SuccessRate float64 `yaml:"success_rate" json:"success_rate"`
}

// ChangelogEntry records a change in the prompt configuration
type ChangelogEntry struct {
	// Version number
	Version string `yaml:"version" json:"version"`
	// Date of change (YYYY-MM-DD)
	Date string `yaml:"date" json:"date"`
	// Author of change
	Author string `yaml:"author,omitempty" json:"author,omitempty"`
	// Description of change
	Description string `yaml:"description" json:"description"`
}

// CompilationInfo records when and how a pack was compiled.
//
// Generated. `compilation` is a spec property with a defined shape
// (compiled_with, created_at and schema are all required), not a promptkit
// extension — it was hand-written here on the false premise that it was one.
type CompilationInfo = packspec.PackCompilation

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
//
// The spec's $defs/ModelOverride also defines system_template_prefix and
// parameters. Neither is added here: nothing in the runtime assembles a prefix
// or applies per-model parameters, so declaring them would be vocabulary with a
// consumer and no producer. They are recorded as codegen candidates in
// docs/local-backlog/PACK_TYPES_FROM_SCHEMA_CODEGEN.md rather than added blind.
// Generated from the schema: an ALIAS for packspec.ModelOverride.
// Gains system_template_prefix and parameters from the spec.
type ModelOverride = packspec.ModelOverride

// Repository interface defines methods for loading prompts (to avoid import cycles)
// This should match persistence.Repository interface
type Repository interface {
	LoadPrompt(taskType string) (*Config, error)
	LoadFragment(name, relativePath, baseDir string) (*Fragment, error)
	ListPrompts() ([]string, error)
	SavePrompt(config *Config) error
}

// Registry manages prompt templates, versions, and variable substitution.
type Registry struct {
	repository       Repository // Required repository for loading prompts
	promptCache      map[string]*Config
	fragmentCache    map[string]*Fragment
	fragmentResolver *FragmentResolver
	templateRenderer *template.Renderer
	mu               sync.RWMutex
}

// NewRegistryWithRepository creates a registry with a repository (new preferred method).
// This constructor uses the repository pattern for loading prompts, avoiding direct file I/O.
func NewRegistryWithRepository(repository Repository) *Registry {
	return &Registry{
		repository:       repository,
		promptCache:      make(map[string]*Config),
		fragmentCache:    make(map[string]*Fragment),
		fragmentResolver: NewFragmentResolverWithRepository(repository),
		templateRenderer: template.NewRenderer(),
	}
}

// Load returns an assembled prompt for the specified activity with variable substitution.
func (r *Registry) Load(activity string) *AssembledPrompt {
	return r.LoadWithVars(activity, make(map[string]string), "")
}

// logPromptLoadFailure logs a prompt-config load failure. An empty activity is
// expected for workflow states with no prompt_task (composition /
// agent-orchestration states, RFC 0010) — benign, logged at Debug. ERROR is
// reserved for genuinely-missing named prompts.
func logPromptLoadFailure(activity string, err error) {
	if activity == "" {
		logger.Debug("No prompt config for empty activity (composition/orchestration state)", "error", err)
		return
	}
	logger.Error("Failed to load prompt config", "activity", activity, "error", err)
}

// LoadTemplate loads a prompt without rendering, returning the raw template and
// all metadata needed to render it later. This is the preferred path for pipeline
// stages that separate assembly from rendering (e.g. PromptAssemblyStage +
// TemplateStage). The returned Template is safe to cache across requests.
func (r *Registry) LoadTemplate(activity string, vars map[string]string, model string) (*Template, error) {
	config, err := r.loadConfig(activity)
	if err != nil {
		logPromptLoadFailure(activity, err)
		return nil, fmt.Errorf("failed to load prompt config for %q: %w", activity, err)
	}

	// Validate required vars before merging
	if err = r.validateRequiredVars(config, vars); err != nil {
		logger.Error("Prompt missing required vars", "activity", activity, "error", err)
		return nil, err
	}

	// Merge provided vars with defaults
	mergedVars := r.mergeVars(config, vars)

	// Assemble fragment vars if configured
	var fragmentVars map[string]string
	if len(config.Spec.Fragments) > 0 {
		fragmentVars, err = r.assembleFragmentVars(config, mergedVars, activity)
		if err != nil {
			return nil, err
		}
		// Merge fragment vars into merged vars (so caller has a complete picture)
		for k, v := range fragmentVars {
			mergedVars[k] = v
		}
	}

	// Apply model overrides to the raw template text
	rawTemplate := r.applyModelOverrides(config, model)

	// Collect required variable names
	var requiredVars []string
	for _, v := range config.Spec.Variables {
		if v.Required {
			requiredVars = append(requiredVars, v.Name)
		}
	}

	// Determine the model override key that was actually applied
	modelOverride := ""
	if model != "" {
		if _, exists := config.Spec.ModelOverrides[model]; exists {
			modelOverride = model
		}
	}

	return &Template{
		TaskType:      config.Spec.TaskType,
		RawTemplate:   rawTemplate,
		DefaultVars:   mergedVars,
		RequiredVars:  requiredVars,
		FragmentVars:  fragmentVars,
		AllowedTools:  config.Spec.AllowedTools,
		Validators:    config.Spec.Validators,
		ModelOverride: modelOverride,
	}, nil
}

// LoadWithVars loads a prompt with variable substitution and optional model override.
func (r *Registry) LoadWithVars(activity string, vars map[string]string, model string) *AssembledPrompt {
	tmpl, err := r.LoadTemplate(activity, vars, model)
	if err != nil {
		return nil
	}

	// Render the template using the merged vars (DefaultVars already contains
	// fragment vars and defaults merged in by LoadTemplate).
	return r.renderAndAssemble(tmpl, activity)
}

// prepareVariables validates required vars, merges with defaults, and assembles fragments
func (r *Registry) prepareVariables(config *Config, vars map[string]string, activity string) (map[string]string, error) {
	// Validate required variables
	if err := r.validateRequiredVars(config, vars); err != nil {
		logger.Error("Prompt missing required vars", "activity", activity, "error", err)
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
func (r *Registry) assembleFragmentVars(config *Config, finalVars map[string]string, activity string) (map[string]string, error) {
	fragmentVars, err := r.fragmentResolver.AssembleFragments(config.Spec.Fragments, finalVars, "")
	if err != nil {
		logger.Error("Fragment assembly failed", "activity", activity, "error", err)
		return nil, err
	}
	return fragmentVars, nil
}

// applyModelOverrides applies model-specific template overrides
func (r *Registry) applyModelOverrides(config *Config, model string) string {
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

// renderAndAssemble renders a Template and creates the final AssembledPrompt.
func (r *Registry) renderAndAssemble(tmpl *Template, activity string) *AssembledPrompt {
	// Render template with merged vars (DefaultVars already includes fragment vars)
	assembledText, err := r.templateRenderer.Render(tmpl.RawTemplate, tmpl.DefaultVars)
	if err != nil {
		logger.Error("Template rendering failed", "activity", activity, "error", err)
		return nil
	}

	// Generate hash for logging/debugging
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(assembledText)))

	result := AssembledPrompt{
		TaskType:     tmpl.TaskType,
		SystemPrompt: assembledText,
		AllowedTools: tmpl.AllowedTools,
		Validators:   tmpl.Validators,
	}

	// Debug logging (controlled by global log level via -v flag)
	logger.Debug("Assembled prompt",
		"task_type", tmpl.TaskType,
		"hash", hash[:8],
		"tools", len(tmpl.AllowedTools),
		"validators", len(tmpl.Validators))

	return &result
}

// ParseConfig parses a prompt config from YAML data.
// This is a package-level utility function for parsing prompt configs in the config layer.
// The config layer should read files using os.ReadFile and pass the data to this function.
// Returns the parsed Config or an error if parsing/validation fails.
func ParseConfig(data []byte) (*Config, error) {
	var config Config
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
func (r *Registry) loadConfig(activity string) (*Config, error) {
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
func (r *Registry) validateRequiredVars(config *Config, vars map[string]string) error {
	// Extract required variable names from Variables
	requiredVars := []string{}
	for _, v := range config.Spec.Variables {
		if v.Required {
			requiredVars = append(requiredVars, v.Name)
		}
	}
	return r.templateRenderer.ValidateRequiredVars(requiredVars, vars)
}

// mergeVars combines provided vars with optional defaults from Variables
func (r *Registry) mergeVars(config *Config, vars map[string]string) map[string]string {
	// Pre-allocate with known capacity for better performance
	result := make(map[string]string, len(config.Spec.Variables)+len(vars))

	// Start with variable defaults. An optional variable with an explicitly-set
	// default (including an empty string) registers so a {{var}} reference
	// renders to that value instead of the renderer hard-erroring on an
	// unresolved placeholder. A nil Default means "no default given" and is
	// skipped.
	for _, v := range config.Spec.Variables {
		if !v.Required && v.Default != nil {
			if defaultStr, ok := v.Default.(string); ok {
				result[v.Name] = defaultStr
			}
		}
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

	r.promptCache = make(map[string]*Config)
	r.fragmentCache = make(map[string]*Fragment)
}

// GetInfo returns detailed information about a prompt configuration
func (r *Registry) GetInfo(taskType string) (*Info, error) {
	config, err := r.loadConfig(taskType)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt info: %w", err)
	}

	// Extract required and optional variable names
	requiredVars := []string{}
	optionalVars := []string{}
	for _, v := range config.Spec.Variables {
		if v.Required {
			requiredVars = append(requiredVars, v.Name)
		} else {
			optionalVars = append(optionalVars, v.Name)
		}
	}

	return &Info{
		TaskType:       config.Spec.TaskType,
		Version:        config.Spec.Version,
		Description:    config.Spec.Description,
		FragmentCount:  len(config.Spec.Fragments),
		RequiredVars:   requiredVars,
		OptionalVars:   optionalVars,
		ToolAllowlist:  config.Spec.AllowedTools,
		ModelOverrides: extractKeys(config.Spec.ModelOverrides),
	}, nil
}

// Info provides summary information about a prompt configuration
type Info struct {
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
func (r *Registry) populateDefaults(config *Config) {
	// Set default template engine info if not specified
	if config.Spec.TemplateEngine == nil {
		config.Spec.TemplateEngine = &TemplateEngineInfo{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		}
	}

	// Variables are now required in the new format - no auto-migration

	// Enabled defaults to true. FailOnViolation defaults are not applied
	// here — the field is part of the PromptPack spec but ignored by this
	// runtime (guardrails always enforce). See ValidatorConfig.FailOnViolation
	// for the deviation rationale.
	trueVal := true
	for i := range config.Spec.Validators {
		if config.Spec.Validators[i].Enabled == nil {
			config.Spec.Validators[i].Enabled = &trueVal
		}
	}
}

// ListTaskTypes returns all available task types from the repository.
// Falls back to cached task types if repository is unavailable or returns empty.
func (r *Registry) ListTaskTypes() []string {
	// Try repository first for complete list
	if r.repository != nil {
		if taskTypes, _ := r.repository.ListPrompts(); len(taskTypes) > 0 {
			return taskTypes
		}
	}

	// Fallback: return cached task types
	r.mu.RLock()
	defer r.mu.RUnlock()
	return extractKeys(r.promptCache)
}

// RegisterConfig registers a Config directly into the registry.
// This allows programmatic registration of prompts without requiring disk files.
// Useful for loading prompts from compiled packs or other in-memory sources.
// If a repository is configured, the config is persisted there as well.
func (r *Registry) RegisterConfig(taskType string, config *Config) error {
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
func (r *Registry) LoadConfig(activity string) (*Config, error) {
	return r.loadConfig(activity)
}
