package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

const (
	defaultProviderGroup = "default"
	extYAML              = ".yaml"
	extYML               = ".yml"

	// errSchemaValidationFailed is the shared error-wrap format used by the
	// three schema-validation failure paths (loadSimpleK8sManifest,
	// loadPromptConfigs, loadTools).
	errSchemaValidationFailed = "schema validation failed for %s: %w"
)

// mergeSpecs is a generic helper that merges inline specs into a loaded resource map.
// It checks for duplicate IDs and calls setID on each spec before storing it.
// specKind and fileRefKind are used in error messages (e.g. "scenario_specs", "scenarios").
func mergeSpecs[T any](
	specs map[string]T,
	loaded map[string]T,
	setID func(T, string),
	specKind, fileRefKind string,
) error {
	for id, spec := range specs {
		if _, exists := loaded[id]; exists {
			return fmt.Errorf(
				"%s %q defined in both %s and %s file refs",
				fileRefKind, id, specKind, fileRefKind,
			)
		}
		setID(spec, id)
		loaded[id] = spec
	}
	return nil
}

// loadResources is a generic helper that loads resources from file refs.
// It resolves each ref's file path relative to configPath, calls the loader
// function, and stores the result in dest keyed by getID(resource).
func loadResources[T any](
	files []string,
	configPath string,
	loader func(string) (T, error),
	getID func(T) string,
	dest map[string]T,
	kind string,
) error {
	for _, file := range files {
		fullPath := ResolveFilePath(configPath, file)
		resource, err := loader(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load %s %s: %w", kind, file, err)
		}
		dest[getID(resource)] = resource
	}
	return nil
}

// mergeProviderSpecs merges inline provider specs into LoadedProviders.
func (c *Config) mergeProviderSpecs() error {
	for id, spec := range c.ProviderSpecs {
		if _, exists := c.LoadedProviders[id]; exists {
			return fmt.Errorf("provider %q defined in both provider_specs and providers file refs", id)
		}
		spec.ID = id
		c.LoadedProviders[id] = spec
		c.ProviderGroups[id] = defaultProviderGroup
		if len(spec.Capabilities) > 0 {
			c.ProviderCapabilities[id] = spec.Capabilities
		}
	}
	return nil
}

// mergeScenarioSpecs merges inline scenario specs into LoadedScenarios.
func (c *Config) mergeScenarioSpecs() error {
	return mergeSpecs(
		c.ScenarioSpecs, c.LoadedScenarios,
		func(s *Scenario, id string) { s.ID = id },
		"scenario_specs", "scenarios",
	)
}

// mergeEvalSpecs merges inline eval specs into LoadedEvals.
func (c *Config) mergeEvalSpecs() error {
	return mergeSpecs(
		c.EvalSpecs, c.LoadedEvals,
		func(e *Eval, id string) { e.ID = id },
		"eval_specs", "evals",
	)
}

// mergeToolSpecs merges inline tool specs into LoadedTools as marshaled YAML manifests.
func (c *Config) mergeToolSpecs() error {
	for name, spec := range c.ToolSpecs {
		spec.Name = name
		manifest := ToolConfigSchema{
			APIVersion: "promptkit.altairalabs.ai/v1alpha1",
			Kind:       "Tool",
			Spec:       *spec,
		}
		data, err := yaml.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("failed to marshal inline tool spec %q: %w", name, err)
		}
		c.LoadedTools = append(c.LoadedTools, ToolData{
			FilePath: fmt.Sprintf("<inline:%s>", name),
			Data:     data,
		})
	}
	return nil
}

// mergeJudgeSpecs merges inline judge specs into LoadedJudges.
func (c *Config) mergeJudgeSpecs() error {
	for name, spec := range c.JudgeSpecs {
		if _, exists := c.LoadedJudges[name]; exists {
			return fmt.Errorf("judge %q defined in both judge_specs and judges refs", name)
		}
		provider, ok := c.LoadedProviders[spec.Provider]
		if !ok {
			return fmt.Errorf("judge_specs %q references unknown provider %q", name, spec.Provider)
		}
		c.LoadedJudges[name] = &JudgeTarget{
			Name:     name,
			Provider: provider,
		}
	}
	return nil
}

// mergePromptSpecs merges inline prompt specs into LoadedPromptConfigs.
func (c *Config) mergePromptSpecs() error {
	for taskType, spec := range c.PromptSpecs {
		if _, exists := c.LoadedPromptConfigs[taskType]; exists {
			return fmt.Errorf(
				"prompt config %q defined in both prompt_specs and prompt_configs file refs",
				taskType,
			)
		}
		c.LoadedPromptConfigs[taskType] = &PromptConfigData{
			FilePath: fmt.Sprintf("<inline:%s>", taskType),
			Config:   &prompt.Config{Spec: *spec},
			TaskType: spec.TaskType,
		}
	}
	return nil
}

const (
	// kindEval is the K8s-style kind value for Eval configurations
	kindEval = "Eval"
)

// LoadConfig loads and validates configuration from a YAML file in K8s-style manifest format.
// Reads all referenced resource files (scenarios, providers, tools, personas) and populates
// the Config struct, making it self-contained for programmatic use without physical files.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Step 1: JSON Schema validation (structure, types, required fields, kind values)
	if err := ValidateArenaConfig(data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Use K8s version for unmarshaling to support full ObjectMeta
	var arenaConfigK8s ArenaConfigK8s
	if err := yaml.Unmarshal(data, &arenaConfigK8s); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Schema validation already confirmed required fields and kind value are correct

	cfg := &arenaConfigK8s.Spec

	// Determine base directory for resolving relative paths
	cfg.ConfigDir = ResolveConfigDir(cfg, filename)

	// Initialize loaded resource maps with appropriate capacity
	cfg.LoadedPromptConfigs = make(map[string]*PromptConfigData, len(cfg.PromptConfigs))
	cfg.LoadedProviders = make(map[string]*Provider, len(cfg.Providers))
	cfg.LoadedJudges = make(map[string]*JudgeTarget, len(cfg.Judges))
	cfg.LoadedScenarios = make(map[string]*Scenario, len(cfg.Scenarios))
	cfg.LoadedEvals = make(map[string]*Eval, len(cfg.Evals))
	cfg.LoadedTools = make([]ToolData, 0, len(cfg.Tools))
	cfg.LoadedPersonas = make(map[string]*UserPersonaPack)
	cfg.LoadedTTSProviders = make(map[string]*Provider, len(cfg.TTSProviders))
	cfg.LoadedSTTProviders = make(map[string]*Provider, len(cfg.STTProviders))
	cfg.LoadedEmbeddingProviders = make(map[string]*Provider, len(cfg.EmbeddingProviders))
	cfg.LoadedImageProviders = make(map[string]*Provider, len(cfg.ImageProviders))
	cfg.LoadedInferenceProviders = make(map[string]*Provider)
	cfg.ProviderGroups = make(map[string]string)
	cfg.ProviderCapabilities = make(map[string][]string)

	// Load all resources (file refs first, then merge inline specs)
	if err := cfg.loadProviders(filename); err != nil {
		return nil, err
	}
	if err := cfg.mergeProviderSpecs(); err != nil {
		return nil, err
	}
	if err := cfg.loadTTSProviders(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadSTTProviders(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadEmbeddingProviders(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadImageProviders(filename); err != nil {
		return nil, err
	}
	// validateInferenceDefaults must run AFTER loadProviders so the role:
	// inference entries are already in LoadedInferenceProviders.
	if err := cfg.validateInferenceDefaults(); err != nil {
		return nil, err
	}
	if err := cfg.validateVoiceBindings(); err != nil {
		return nil, err
	}
	if err := cfg.loadPromptConfigs(filename); err != nil {
		return nil, err
	}
	if err := cfg.mergePromptSpecs(); err != nil {
		return nil, err
	}
	if err := cfg.loadScenarios(filename); err != nil {
		return nil, err
	}
	if err := cfg.mergeScenarioSpecs(); err != nil {
		return nil, err
	}
	// Validate that every scenario's voice (when set) resolves to an arena
	// voice binding. Voice bindings were already validated above, so
	// ResolveVoice will work correctly for valid scenario voices.
	if err := cfg.validateScenarioVoices(); err != nil {
		return nil, err
	}
	if err := cfg.loadEvals(filename); err != nil {
		return nil, err
	}
	if err := cfg.mergeEvalSpecs(); err != nil {
		return nil, err
	}
	if err := cfg.loadTools(filename); err != nil {
		return nil, err
	}
	if err := cfg.mergeToolSpecs(); err != nil {
		return nil, err
	}
	if err := cfg.loadSkills(filename); err != nil {
		return nil, err
	}

	// Load self-play resources if enabled
	if cfg.SelfPlay != nil && cfg.SelfPlay.IsEnabled() {
		if err := cfg.loadSelfPlayResources(filename); err != nil {
			return nil, err
		}
		// Validate that every persona's voice (when set) resolves to an arena
		// voice binding. Voice bindings were already validated above, so
		// ResolveVoice will work correctly for valid persona voices.
		if err := cfg.validatePersonaVoices(); err != nil {
			return nil, err
		}
	}

	// Validate judge references against provider registry (mirrors self-play validation)
	if err := cfg.validateJudgeReferences(); err != nil {
		return nil, err
	}
	if err := cfg.buildJudgeTargets(); err != nil {
		return nil, err
	}
	if err := cfg.mergeJudgeSpecs(); err != nil {
		return nil, err
	}

	// Load pack file if specified
	if cfg.PackFile != "" {
		if err := cfg.loadPackFile(filename); err != nil {
			return nil, err
		}
	}

	// Validate the loaded configuration. Hard errors were already caught by schema
	// validation above; this catches semantic warnings (e.g. unused providers).
	validator := NewConfigValidatorWithPath(cfg, filename)
	_ = validator.Validate()
	cfg.ValidationWarnings = validator.GetWarnings()

	return cfg, nil
}

// k8sManifest is an interface for K8s-style manifest types
type k8sManifest interface {
	GetAPIVersion() string
	GetKind() string
	GetName() string
	SetID(id string)
}

// loadSimpleK8sManifest is a generic loader for K8s-style manifest files
// This is used for simple config types (Scenario, Provider) that don't support legacy formats
func loadSimpleK8sManifest[T k8sManifest](filename, expectedKind string) (T, error) {
	var zero T

	data, err := os.ReadFile(filename)
	if err != nil {
		return zero, fmt.Errorf("failed to read %s file: %w", expectedKind, err)
	}

	// Schema validation based on kind
	var validationErr error
	switch expectedKind {
	case "Scenario":
		validationErr = ValidateScenario(data)
	case kindEval:
		validationErr = ValidateEval(data)
	case "Provider":
		validationErr = ValidateProvider(data)
	case "Tool":
		validationErr = ValidateTool(data)
	case "Persona":
		validationErr = ValidatePersona(data)
	}
	if validationErr != nil {
		return zero, fmt.Errorf(errSchemaValidationFailed, expectedKind, validationErr)
	}

	var config T
	if err := yaml.Unmarshal(data, &config); err != nil {
		return zero, fmt.Errorf("failed to parse %s file: %w", expectedKind, err)
	}

	// Schema validation already confirmed required fields and kind value are correct

	// Use metadata.name as the ID
	config.SetID(config.GetName())
	return config, nil
}

// LoadScenario loads and parses a scenario from a YAML file in K8s-style manifest format
func LoadScenario(filename string) (*Scenario, error) {
	// Use K8s version for unmarshaling
	config, err := loadSimpleK8sManifest[*ScenarioConfigK8s](filename, "Scenario")
	if err != nil {
		return nil, err
	}

	// Validate nested configs (DuplexConfig, RelevanceConfig, etc.)
	if err := config.Spec.Validate(); err != nil {
		return nil, fmt.Errorf("scenario validation failed for %s: %w", filename, err)
	}

	// Copy K8s metadata.labels into the spec so downstream consumers
	// (engine, statestore, report) carry stratification labels alongside
	// the rest of the scenario.
	if len(config.Metadata.Labels) > 0 {
		config.Spec.Labels = maps.Clone(config.Metadata.Labels)
	}

	return &config.Spec, nil
}

// LoadEval loads and parses an eval configuration from a YAML file in K8s-style manifest format
func LoadEval(filename string) (*Eval, error) {
	// Use K8s version for unmarshaling
	config, err := loadSimpleK8sManifest[*EvalConfigK8s](filename, "Eval")
	if err != nil {
		return nil, err
	}

	// Resolve recording path relative to the eval file's directory
	if config.Spec.Recording.Path != "" && !filepath.IsAbs(config.Spec.Recording.Path) {
		config.Spec.Recording.Path = ResolveFilePath(filename, config.Spec.Recording.Path)
	}

	return &config.Spec, nil
}

// LoadProvider loads and parses a provider configuration from a YAML file in K8s-style manifest format
func LoadProvider(filename string) (*Provider, error) {
	// Use K8s version for unmarshaling
	config, err := loadSimpleK8sManifest[*ProviderConfigK8s](filename, "Provider")
	if err != nil {
		return nil, err
	}
	return &config.Spec, nil
}

// loadPromptConfigs loads and parses all referenced prompt configurations
func (c *Config) loadPromptConfigs(configPath string) error {
	for _, ref := range c.PromptConfigs {
		if ref.File == "" {
			continue
		}

		fullPath := ResolveFilePath(configPath, ref.File)

		// Read file once
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to read prompt file %s: %w", ref.File, err)
		}

		// Schema validation
		if err := ValidatePromptConfig(data); err != nil {
			return fmt.Errorf(errSchemaValidationFailed, ref.File, err)
		}

		// Parse configuration
		promptConfig, err := prompt.ParseConfig(data)
		if err != nil {
			return fmt.Errorf("failed to parse prompt %s: %w", ref.File, err)
		}

		// Store parsed config with metadata and variable overrides from arena.yaml
		c.LoadedPromptConfigs[ref.ID] = &PromptConfigData{
			FilePath: ref.File,
			Config:   promptConfig,
			TaskType: promptConfig.Spec.TaskType,
			Vars:     ref.Vars, // Store variable overrides from arena.yaml
		}
	}
	return nil
}

// loadScenarios loads all referenced scenarios
func (c *Config) loadScenarios(configPath string) error {
	for _, ref := range c.Scenarios {
		fullPath := ResolveFilePath(configPath, ref.File)
		scenario, err := LoadScenario(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load scenario %s: %w", ref.File, err)
		}
		c.LoadedScenarios[scenario.ID] = scenario
	}
	return nil
}

// loadEvals loads all referenced eval configurations
func (c *Config) loadEvals(configPath string) error {
	files := make([]string, len(c.Evals))
	for i, ref := range c.Evals {
		files[i] = ref.File
	}
	return loadResources(
		files, configPath, LoadEval,
		func(e *Eval) string { return e.ID },
		c.LoadedEvals, "eval",
	)
}

// loadProviders loads all referenced providers from the unified
// `providers:` list and routes each into the appropriate Loaded* map
// based on its `role:` value.
//
// Matrix membership is by interface compatibility with the LLM-shaped
// ProviderStage: role=llm and role=image both implement Predict and
// flow through ProviderStage normally, so both go into LoadedProviders
// (the agent-under-test matrix) AND each role's typed map. role=tts,
// role=stt, role=embedding use different interfaces (Synthesize,
// Transcribe, Embed) and never reach ProviderStage — they're routed
// only to their typed map and a one-line warning is emitted explaining
// they were loaded but excluded from the matrix.
//
// The dedicated `tts_providers:` / `stt_providers:` / `embedding_providers:`
// / `image_providers:` slots are still honored for backward
// compatibility and load via loadTTSProviders et al; pack authors are
// encouraged to put every provider in the unified `providers:` list and
// let role routing do the work.
func (c *Config) loadProviders(configPath string) error {
	for _, ref := range c.Providers {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load provider %s: %w", ref.File, err)
		}
		if err := provider.ValidateRole(); err != nil {
			return fmt.Errorf("provider %s: %w", provider.ID, err)
		}
		role := provider.GetRole()
		switch role {
		case RoleLLM, RoleImage:
			// Predict-compatible — eligible for the agent-under-test matrix.
			c.LoadedProviders[provider.ID] = provider
			group := ref.Group
			if group == "" {
				group = defaultProviderGroup
			}
			c.ProviderGroups[provider.ID] = group
			if len(provider.Capabilities) > 0 {
				c.ProviderCapabilities[provider.ID] = provider.Capabilities
			}
			if role == RoleImage {
				c.LoadedImageProviders[provider.ID] = provider
			}
		case RoleTTS:
			c.LoadedTTSProviders[provider.ID] = provider
			logger.Warn("Provider loaded but excluded from matrix",
				"provider", provider.ID, "role", role,
				"reason", "TTS uses Synthesize() interface, not Predict()")
		case RoleSTT:
			c.LoadedSTTProviders[provider.ID] = provider
			logger.Warn("Provider loaded but excluded from matrix",
				"provider", provider.ID, "role", role,
				"reason", "STT uses Transcribe() interface, not Predict()")
		case RoleEmbedding:
			c.LoadedEmbeddingProviders[provider.ID] = provider
			logger.Warn("Provider loaded but excluded from matrix",
				"provider", provider.ID, "role", role,
				"reason", "embedding providers use Embed() interface, not Predict()")
		case RoleInference:
			c.LoadedInferenceProviders[provider.ID] = provider
			logger.Warn("Provider loaded but excluded from matrix",
				"provider", provider.ID, "role", role,
				"reason", "inference providers use runtime/classify task interfaces, not Predict()")
		default:
			return fmt.Errorf("providers[%s]: unhandled role %q", ref.File, role)
		}
	}
	return nil
}

// loadTTSProviders loads all referenced TTS providers and validates that each
// declares role: tts.
func (c *Config) loadTTSProviders(configPath string) error {
	for _, ref := range c.TTSProviders {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("loading tts_providers[%s]: %w", ref.File, err)
		}
		if err := provider.ValidateRole(); err != nil {
			return fmt.Errorf("tts_providers[%s]: %w", ref.File, err)
		}
		if provider.GetRole() != RoleTTS {
			return fmt.Errorf("tts_providers[%s]: role must be %q, got %q",
				ref.File, RoleTTS, provider.GetRole())
		}
		c.LoadedTTSProviders[provider.ID] = provider
	}
	return nil
}

// loadSTTProviders loads all referenced STT providers and validates that each
// declares role: stt.
func (c *Config) loadSTTProviders(configPath string) error {
	for _, ref := range c.STTProviders {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("loading stt_providers[%s]: %w", ref.File, err)
		}
		if err := provider.ValidateRole(); err != nil {
			return fmt.Errorf("stt_providers[%s]: %w", ref.File, err)
		}
		if provider.GetRole() != RoleSTT {
			return fmt.Errorf("stt_providers[%s]: role must be %q, got %q",
				ref.File, RoleSTT, provider.GetRole())
		}
		c.LoadedSTTProviders[provider.ID] = provider
	}
	return nil
}

// loadEmbeddingProviders loads all referenced embedding providers and
// validates that each declares role: embedding.
func (c *Config) loadEmbeddingProviders(configPath string) error {
	for _, ref := range c.EmbeddingProviders {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("loading embedding_providers[%s]: %w", ref.File, err)
		}
		if err := provider.ValidateRole(); err != nil {
			return fmt.Errorf("embedding_providers[%s]: %w", ref.File, err)
		}
		if provider.GetRole() != RoleEmbedding {
			return fmt.Errorf("embedding_providers[%s]: role must be %q, got %q",
				ref.File, RoleEmbedding, provider.GetRole())
		}
		c.LoadedEmbeddingProviders[provider.ID] = provider
	}
	return nil
}

// validateInferenceDefaults ensures every id referenced by
// cfg.Defaults.Inference resolves to an entry in LoadedInferenceProviders
// (populated by loadProviders for entries declaring role: inference).
// Called from LoadConfig after all providers are loaded so unknown ids
// surface at load time, not at first eval.
func (c *Config) validateInferenceDefaults() error {
	if c.Defaults.Inference == nil {
		return nil
	}
	defaults := c.Defaults.Inference
	for label, id := range map[string]string{
		"audio_classifier": defaults.AudioClassifier,
		"text_classifier":  defaults.TextClassifier,
		"image_classifier": defaults.ImageClassifier,
		"video_classifier": defaults.VideoClassifier,
		"embedder":         defaults.Embedder,
	} {
		if id == "" {
			continue
		}
		if _, ok := c.LoadedInferenceProviders[id]; !ok {
			return fmt.Errorf(
				"defaults.inference.%s: unknown provider id %q (expected providers: entry with role: inference)",
				label, id)
		}
	}
	return nil
}

// loadImageProviders loads all referenced image providers and validates that
// each declares role: image.
func (c *Config) loadImageProviders(configPath string) error {
	for _, ref := range c.ImageProviders {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("loading image_providers[%s]: %w", ref.File, err)
		}
		if err := provider.ValidateRole(); err != nil {
			return fmt.Errorf("image_providers[%s]: %w", ref.File, err)
		}
		if provider.GetRole() != RoleImage {
			return fmt.Errorf("image_providers[%s]: role must be %q, got %q",
				ref.File, RoleImage, provider.GetRole())
		}
		c.LoadedImageProviders[provider.ID] = provider
	}
	return nil
}

// validateVoiceBindings checks that every entry in spec.voices references a
// provider id that was loaded into LoadedTTSProviders.
func (c *Config) validateVoiceBindings() error {
	for i := range c.Voices {
		binding := &c.Voices[i]
		if binding.ID == "" {
			return fmt.Errorf("voices[%d]: id is required", i)
		}
		if binding.Provider == "" {
			return fmt.Errorf("voices[%s]: provider is required", binding.ID)
		}
		if _, ok := c.LoadedTTSProviders[binding.Provider]; !ok {
			return fmt.Errorf("voices[%s]: provider id %q not found in tts_providers",
				binding.ID, binding.Provider)
		}
	}
	return nil
}

// validateScenarioVoices checks that every scenario's voice (when set) resolves
// to a valid arena voice binding. Scenarios without a voice field are allowed —
// they use persona-driven voices (selfplay) resolved via the arena voice catalog.
func (c *Config) validateScenarioVoices() error {
	for name, scenario := range c.LoadedScenarios {
		if scenario.Voice == "" {
			continue
		}
		if _, err := c.ResolveVoice(scenario.Voice); err != nil {
			return fmt.Errorf("scenario %s: %w", name, err)
		}
	}
	return nil
}

// validatePersonaVoices checks that every persona's voice (when set) resolves
// to a valid arena voice binding. Personas without a voice field are allowed
// (used by text-only scenarios).
func (c *Config) validatePersonaVoices() error {
	for name, persona := range c.LoadedPersonas {
		if persona.Voice == "" {
			continue
		}
		if _, err := c.ResolveVoice(persona.Voice); err != nil {
			return fmt.Errorf("persona %s: %w", name, err)
		}
	}
	return nil
}

// loadTools loads all referenced tools
func (c *Config) loadTools(configPath string) error {
	for _, ref := range c.Tools {
		fullPath := ResolveFilePath(configPath, ref.File)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to read tool file %s: %w", ref.File, err)
		}
		// Validate K8s-style YAML tool files against the schema.
		// JSON tool files (raw OpenAI format) are validated structurally
		// by the tool registry at load time, not by schema.
		ext := strings.ToLower(filepath.Ext(ref.File))
		if ext == extYAML || ext == extYML {
			if err := ValidateTool(data); err != nil {
				return fmt.Errorf(errSchemaValidationFailed, ref.File, err)
			}
		}
		c.LoadedTools = append(c.LoadedTools, ToolData{
			FilePath: ref.File,
			Data:     data,
		})
	}
	return nil
}

// loadSkills resolves skill source paths and validates containment.
func (c *Config) loadSkills(configPath string) error {
	if len(c.Skills) == 0 {
		return nil
	}

	// Resolve to absolute paths before the prefix comparison. When configPath
	// is relative (e.g. "config.arena.yaml" run from the example dir), Dir
	// returns "." and Clean("./skills") returns "skills" — the historical
	// HasPrefix check then fails closed because "skills" doesn't start with
	// "./". Anchoring both sides at filepath.Abs side-steps that.
	configDir := filepath.Dir(configPath)
	absBase, err := filepath.Abs(configDir)
	if err != nil {
		return fmt.Errorf("skills: resolve config dir %q: %w", configDir, err)
	}

	for _, src := range c.Skills {
		dir := src.EffectiveDir()
		if dir == "" {
			// Inline skill — pass through as-is.
			c.LoadedSkillSources = append(c.LoadedSkillSources, src)
			continue
		}

		// Resolve relative path.
		absDir := dir
		if !filepath.IsAbs(absDir) {
			absDir = filepath.Join(absBase, absDir)
		}
		absDir = filepath.Clean(absDir)

		// Validate path containment.
		if absDir != absBase && !strings.HasPrefix(absDir, absBase+string(filepath.Separator)) {
			return fmt.Errorf("skills: path traversal detected: %q resolves outside config directory %q", dir, configDir)
		}

		// Verify directory exists.
		info, err := os.Stat(absDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("skills: directory %q does not exist (resolved to %q)", dir, absDir)
		}

		resolved := src
		resolved.Dir = ""
		resolved.Path = absDir
		c.LoadedSkillSources = append(c.LoadedSkillSources, resolved)
	}
	return nil
}

// loadPackFile loads a .pack.json file and stores the result in LoadedPack.
func (c *Config) loadPackFile(configPath string) error {
	fullPath := ResolveFilePath(configPath, c.PackFile)
	pack, err := prompt.LoadPack(fullPath)
	if err != nil {
		return fmt.Errorf("failed to load pack file %s: %w", c.PackFile, err)
	}
	c.LoadedPack = pack
	return nil
}

// loadSelfPlayResources loads personas and validates self-play provider references
func (c *Config) loadSelfPlayResources(configPath string) error {
	// Load personas
	for _, ref := range c.SelfPlay.Personas {
		fullPath := ResolveFilePath(configPath, ref.File)
		persona, err := LoadPersona(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load persona %s: %w", ref.File, err)
		}
		c.LoadedPersonas[persona.ID] = persona
	}

	// Merge inline persona specs
	for id, spec := range c.SelfPlay.PersonaSpecs {
		if _, exists := c.LoadedPersonas[id]; exists {
			return fmt.Errorf("persona %q defined in both persona_specs and personas file refs", id)
		}
		spec.ID = id
		c.LoadedPersonas[id] = spec
	}

	// Validate self-play provider references against main provider registry
	for _, roleConfig := range c.SelfPlay.Roles {
		if roleConfig.Provider == "" {
			return fmt.Errorf("self-play role %s must specify a provider", roleConfig.ID)
		}

		// Verify provider exists (LoadedProviders is populated before this function is called)
		if _, exists := c.LoadedProviders[roleConfig.Provider]; !exists {
			return fmt.Errorf(
				"self-play role %s references unknown provider %s (must be defined in spec.providers)",
				roleConfig.ID, roleConfig.Provider,
			)
		}
	}

	return nil
}

// validateJudgeReferences ensures all judges reference known providers.
func (c *Config) validateJudgeReferences() error {
	for _, judge := range c.Judges {
		if judge.Provider == "" {
			return fmt.Errorf("judge %s must specify a provider", judge.Name)
		}

		if _, exists := c.LoadedProviders[judge.Provider]; !exists {
			return fmt.Errorf("judge %s references unknown provider %s (must be defined in spec.providers)",
				judge.Name, judge.Provider)
		}
	}
	return nil
}

// buildJudgeTargets resolves judge references to their provider configs.
// The judge's model is the provider's model.
func (c *Config) buildJudgeTargets() error {
	for _, judge := range c.Judges {
		provider, exists := c.LoadedProviders[judge.Provider]
		if !exists {
			return fmt.Errorf("judge %s references unknown provider %s (must be defined in spec.providers)",
				judge.Name, judge.Provider)
		}

		c.LoadedJudges[judge.Name] = &JudgeTarget{
			Name:     judge.Name,
			Provider: provider,
		}
	}
	return nil
}
