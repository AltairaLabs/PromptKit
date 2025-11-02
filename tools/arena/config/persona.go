package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/template"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PersonaConfig represents a K8s-style persona configuration manifest
type PersonaConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       UserPersonaPack   `yaml:"spec"`
}

// UserPersonaPack defines how the User LLM behaves in self-play scenarios
type UserPersonaPack struct {
	ID             string `json:"id" yaml:"id"`
	Description    string `json:"description" yaml:"description"`
	PromptActivity string `json:"prompt_activity,omitempty" yaml:"prompt_activity,omitempty"` // DEPRECATED: Legacy prompt builder reference

	// NEW: Template system (preferred)
	Fragments      []FragmentRef     `json:"fragments,omitempty" yaml:"fragments,omitempty"`             // Fragment references for composition
	SystemTemplate string            `json:"system_template,omitempty" yaml:"system_template,omitempty"` // Template with {{variables}}
	RequiredVars   []string          `json:"required_vars,omitempty" yaml:"required_vars,omitempty"`     // Variables that must be provided
	OptionalVars   map[string]string `json:"optional_vars,omitempty" yaml:"optional_vars,omitempty"`     // Variables with default values

	// LEGACY: Backward compatibility
	SystemPrompt string `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"` // Plain text system prompt (no variables)

	Goals       []string        `json:"goals" yaml:"goals"`
	Constraints []string        `json:"constraints" yaml:"constraints"`
	Style       PersonaStyle    `json:"style" yaml:"style"`
	Defaults    PersonaDefaults `json:"defaults" yaml:"defaults"`
}

// FragmentRef references a reusable prompt fragment
// Reuses the same structure as prompt.FragmentRef for consistency
type FragmentRef struct {
	Name     string `yaml:"name" json:"name"`
	Path     string `yaml:"path,omitempty" json:"path,omitempty"` // Optional: relative path to fragment file
	Required bool   `yaml:"required" json:"required"`
}

// PersonaStyle defines behavioral characteristics for the user persona
type PersonaStyle struct {
	Verbosity      string   `json:"verbosity" yaml:"verbosity"`             // "short", "medium", "long"
	ChallengeLevel string   `json:"challenge_level" yaml:"challenge_level"` // "low", "medium", "high"
	FrictionTags   []string `json:"friction_tags" yaml:"friction_tags"`     // e.g., ["skeptical", "impatient", "detail-oriented"]
}

// PersonaDefaults contains default LLM parameters for the persona
type PersonaDefaults struct {
	Temperature float32 `json:"temperature" yaml:"temperature"`
	Seed        int     `json:"seed" yaml:"seed"`
}

// LoadPersona loads a user persona from a YAML or JSON file
func LoadPersona(filename string) (*UserPersonaPack, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read persona file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".yaml" || ext == ".yml" {
		return loadYAMLPersona(data, filename)
	}

	return loadJSONPersona(data, filename)
}

// loadYAMLPersona loads a persona from YAML data
func loadYAMLPersona(data []byte, filename string) (*UserPersonaPack, error) {
	temp, err := parseYAMLData(data, filename)
	if err != nil {
		return nil, err
	}

	tempMap, ok := temp.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid YAML structure in %s", filename)
	}

	if isK8sManifest(tempMap) {
		return loadK8sManifest(tempMap, filename)
	}

	return loadLegacyYAMLFormat(tempMap, filename)
}

// parseYAMLData parses YAML data into a generic interface
func parseYAMLData(data []byte, filename string) (interface{}, error) {
	var temp interface{}
	if err := yaml.Unmarshal(data, &temp); err != nil {
		return nil, fmt.Errorf("failed to parse YAML persona file %s: %w", filename, err)
	}
	return temp, nil
}

// isK8sManifest checks if the YAML structure is a K8s manifest
func isK8sManifest(tempMap map[string]interface{}) bool {
	apiVersion, hasAPI := tempMap["apiVersion"].(string)
	return hasAPI && apiVersion != ""
}

// loadK8sManifest loads a persona from a K8s-style manifest
func loadK8sManifest(tempMap map[string]interface{}, filename string) (*UserPersonaPack, error) {
	jsonData, err := json.Marshal(tempMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert K8s manifest to JSON for %s: %w", filename, err)
	}

	var personaConfig PersonaConfig
	if err := json.Unmarshal(jsonData, &personaConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal K8s manifest %s: %w", filename, err)
	}

	if err := validateK8sManifest(&personaConfig, filename); err != nil {
		return nil, err
	}

	// Use metadata.name as persona ID
	personaConfig.Spec.ID = personaConfig.Metadata.Name

	return validateAndReturnPersona(&personaConfig.Spec, filename)
}

// validateK8sManifest validates K8s manifest structure
func validateK8sManifest(personaConfig *PersonaConfig, filename string) error {
	if personaConfig.Kind == "" {
		return fmt.Errorf("persona config %s is missing kind", filename)
	}
	if personaConfig.Kind != "Persona" {
		return fmt.Errorf("persona config %s has invalid kind: expected 'Persona', got '%s'", filename, personaConfig.Kind)
	}
	if personaConfig.Metadata.Name == "" {
		return fmt.Errorf("persona config %s is missing metadata.name", filename)
	}
	return nil
}

// loadLegacyYAMLFormat loads a persona from legacy YAML format
func loadLegacyYAMLFormat(tempMap map[string]interface{}, filename string) (*UserPersonaPack, error) {
	jsonData, err := json.Marshal(tempMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON for %s: %w", filename, err)
	}

	var persona UserPersonaPack
	if err := json.Unmarshal(jsonData, &persona); err != nil {
		return nil, fmt.Errorf("failed to unmarshal persona %s: %w", filename, err)
	}

	return validateAndReturnPersona(&persona, filename)
}

// loadJSONPersona loads a persona from JSON data
func loadJSONPersona(data []byte, filename string) (*UserPersonaPack, error) {
	var persona UserPersonaPack
	if err := json.Unmarshal(data, &persona); err != nil {
		return nil, fmt.Errorf("failed to parse JSON persona file: %w", err)
	}

	return validateAndReturnPersona(&persona, filename)
}

// validateAndReturnPersona validates a persona and returns it
func validateAndReturnPersona(persona *UserPersonaPack, filename string) (*UserPersonaPack, error) {
	// Validate required fields
	if persona.ID == "" {
		return nil, fmt.Errorf("persona ID is required in %s", filename)
	}

	// Require at least one prompt configuration method (in priority order):
	// 1. system_template (new, preferred)
	// 2. system_prompt (legacy, backwards compatible)
	// 3. prompt_activity (deprecated, scheduled for removal)
	if persona.SystemTemplate == "" && persona.SystemPrompt == "" && persona.PromptActivity == "" {
		return nil, fmt.Errorf("persona %s must specify system_template, system_prompt, or prompt_activity", persona.ID)
	}

	// Validate style properties
	if err := validatePersonaStyle(persona.Style); err != nil {
		return nil, fmt.Errorf("persona %s style validation failed: %w", persona.ID, err)
	}

	// Set defaults if not specified
	if persona.Defaults.Temperature == 0 {
		persona.Defaults.Temperature = 0.7
	}
	if persona.Defaults.Seed == 0 {
		persona.Defaults.Seed = 42
	}

	return persona, nil
}

// validatePersonaStyle validates the persona style properties
func validatePersonaStyle(style PersonaStyle) error {
	// Validate verbosity
	validVerbosities := map[string]bool{"short": true, "medium": true, "long": true}
	if style.Verbosity != "" && !validVerbosities[style.Verbosity] {
		return fmt.Errorf("invalid verbosity '%s', must be one of: short, medium, long", style.Verbosity)
	}

	// Validate challenge_level
	validChallengeLevels := map[string]bool{"low": true, "medium": true, "high": true}
	if style.ChallengeLevel != "" && !validChallengeLevels[style.ChallengeLevel] {
		return fmt.Errorf("invalid challenge_level '%s', must be one of: low, medium, high", style.ChallengeLevel)
	}

	// Validate friction_tags (no specific validation needed, but could add length limits)
	if len(style.FrictionTags) > 10 {
		return fmt.Errorf("too many friction_tags (%d), maximum allowed is 10", len(style.FrictionTags))
	}

	return nil
}

// BuildSystemPrompt builds the system prompt using template system or falls back to legacy methods.
// Priority order:
// 1. system_template + fragments (new, preferred)
// 2. system_prompt (legacy, plain text)
// 3. prompt_activity (deprecated, will be removed)
func (p *UserPersonaPack) BuildSystemPrompt(region string, contextVars map[string]string) (string, error) {
	// NEW: Use template system if system_template is specified
	if p.SystemTemplate != "" || len(p.Fragments) > 0 {
		return p.buildTemplatedPrompt(region, contextVars)
	}

	// LEGACY: Fallback to plain system_prompt
	if p.SystemPrompt != "" {
		return p.SystemPrompt, nil
	}

	// REMOVED: prompt_activity system was deprecated and has been removed
	// Use system_template instead
	if p.PromptActivity != "" {
		return "", fmt.Errorf("prompt_activity is no longer supported for persona %s, use system_template instead", p.ID)
	}

	return "", fmt.Errorf("no system_template or system_prompt specified for persona %s", p.ID)
}

// buildTemplatedPrompt uses the new template system to build a persona system prompt.
// This method supports:
// - Fragment composition for reusable persona components
// - Variable substitution with {{variable}} syntax
// - Required and optional variables
// - Persona-specific variable injection (goals, constraints, style)
func (p *UserPersonaPack) buildTemplatedPrompt(region string, contextVars map[string]string) (string, error) {
	// Create template renderer
	renderer := template.NewRenderer()

	// Build variable map by merging (priority: context > optional_vars)
	vars := make(map[string]string)

	// 1. Start with optional variable defaults
	for k, v := range p.OptionalVars {
		vars[k] = v
	}

	// 2. Merge context variables (higher priority)
	for k, v := range contextVars {
		vars[k] = v
	}

	// 3. Add persona-specific variables (highest priority, always injected)
	vars["persona_goals"] = strings.Join(p.Goals, ", ")
	vars["persona_constraints"] = strings.Join(p.Constraints, ", ")
	vars["verbosity"] = p.Style.Verbosity
	vars["challenge_level"] = p.Style.ChallengeLevel
	vars["friction_tags"] = strings.Join(p.Style.FrictionTags, ", ")
	vars["region"] = region

	// Set defaults for persona-specific variables if empty
	const defaultVerbosity = "medium"
	if vars["verbosity"] == "" {
		vars["verbosity"] = defaultVerbosity
	}
	if vars["challenge_level"] == "" {
		vars["challenge_level"] = "medium"
	}
	if vars["friction_tags"] == "" {
		vars["friction_tags"] = "none"
	}
	if vars["persona_goals"] == "" {
		vars["persona_goals"] = "Interact naturally and helpfully"
	}
	if vars["persona_constraints"] == "" {
		vars["persona_constraints"] = "Be respectful and constructive"
	}

	// 4. Process fragments if configured
	templateText := p.SystemTemplate
	if len(p.Fragments) > 0 {
		// For personas, fragments are typically stored relative to the persona file
		// We'll use the YAML repository approach to load fragments from disk
		// Note: This still uses file I/O but through the repository pattern
		// which is acceptable for the config layer

		// Create a basic fragment loader for personas
		// The fragment resolver itself will be created with repository pattern
		// when the runtime layer needs it

		// For now, just validate that fragments are specified
		// The actual fragment loading will be handled when needed
		// This is a temporary approach - proper fragment loading through repository
		// should be implemented when persona system is refactored further
		return "", fmt.Errorf("fragment loading for personas not yet fully implemented in config-first pattern for persona %s", p.ID)
	}

	// 5. Validate required variables
	if len(p.RequiredVars) > 0 {
		if err := renderer.ValidateRequiredVars(p.RequiredVars, vars); err != nil {
			return "", fmt.Errorf("persona %s validation failed: %w", p.ID, err)
		}
	}

	// 6. Render template with variables
	result, err := renderer.Render(templateText, vars)
	if err != nil {
		return "", fmt.Errorf("failed to render template for persona %s: %w", p.ID, err)
	}

	return result, nil
}

// convertFragmentRefs converts config.FragmentRef to prompt.FragmentRef
// (they're the same structure, but different types for package separation)
func convertFragmentRefs(refs []FragmentRef) []prompt.FragmentRef {
	result := make([]prompt.FragmentRef, len(refs))
	for i, ref := range refs {
		result[i] = prompt.FragmentRef{
			Name:     ref.Name,
			Path:     ref.Path,
			Required: ref.Required,
		}
	}
	return result
}
