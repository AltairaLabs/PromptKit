package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// buildMemoryRepo creates a memory-backed prompt repository from the loaded config.
func buildMemoryRepo(cfg *config.Config) (*memory.PromptRepository, error) {
	memRepo := memory.NewPromptRepository()

	for _, promptData := range cfg.LoadedPromptConfigs {
		if promptData.Config == nil {
			continue
		}

		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			return nil, fmt.Errorf("prompt config %s has invalid type", promptData.FilePath)
		}

		if err := memRepo.SavePrompt(promptConfig); err != nil {
			return nil, fmt.Errorf("registering prompt %s: %w", promptData.FilePath, err)
		}
	}

	return memRepo, nil
}

// collectMediaWarnings validates media references across all loaded prompts
// and returns any warnings found.
func collectMediaWarnings(cfg *config.Config, configDir string) []string {
	var warnings []string

	for _, promptData := range cfg.LoadedPromptConfigs {
		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			continue
		}

		w := validateMediaReferences(promptConfig, configDir)
		if len(w) > 0 {
			for _, msg := range w {
				warnings = append(warnings, fmt.Sprintf("media(%s): %s", promptConfig.Spec.TaskType, msg))
			}
		}
	}

	return warnings
}

// validateMediaReferences checks if media files referenced in examples exist.
func validateMediaReferences(cfg *prompt.Config, baseDir string) []string {
	var warnings []string

	if cfg.Spec.MediaConfig == nil || !cfg.Spec.MediaConfig.Enabled {
		return warnings
	}

	for _, example := range cfg.Spec.MediaConfig.Examples {
		warnings = append(warnings, validateExampleMediaReferences(example, baseDir)...)
	}

	return warnings
}

// validateExampleMediaReferences validates media references in a single example.
func validateExampleMediaReferences(example prompt.MultimodalExample, baseDir string) []string {
	var warnings []string

	for i, part := range example.Parts {
		if part.Media == nil || part.Media.FilePath == "" {
			continue
		}

		filePath := part.Media.FilePath
		if !filepath.IsAbs(filePath) && baseDir != "" {
			filePath = filepath.Join(baseDir, filePath)
		}

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf(
				"Media file not found: %s (example: %s, part %d)",
				part.Media.FilePath, example.Name, i))
		}
	}

	return warnings
}

// parseToolsFromConfig parses raw tool YAML data from config into ParsedTool structs.
func parseToolsFromConfig(cfg *config.Config) []prompt.ParsedTool {
	var result []prompt.ParsedTool

	if len(cfg.LoadedTools) == 0 {
		return result
	}

	registry := tools.NewRegistry()

	for _, td := range cfg.LoadedTools {
		if err := registry.LoadToolFromBytes(td.FilePath, td.Data); err != nil {
			// Skip invalid tools — caller can't do much about them.
			continue
		}
	}

	for name, tool := range registry.GetTools() {
		result = append(result, prompt.ParsedTool{
			Name:        name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	return result
}

// parsePackEvalsFromConfig returns pack-level eval definitions from arena config.
func parsePackEvalsFromConfig(cfg *config.Config) []evals.EvalDef {
	return cfg.PackEvals
}

// parseWorkflowFromConfig parses workflow config from arena config.
// Returns nil, nil when no workflow is configured.
func parseWorkflowFromConfig(cfg *config.Config) (*prompt.WorkflowConfig, error) {
	return workflow.ParseConfig(cfg.Workflow)
}

// parseAgentsFromConfig parses agents config from arena config.
// Returns nil, nil when no agents are configured.
func parseAgentsFromConfig(cfg *config.Config) (*prompt.AgentsConfig, error) {
	if cfg.Agents == nil {
		return nil, nil
	}
	data, err := json.Marshal(cfg.Agents)
	if err != nil {
		return nil, fmt.Errorf("marshaling agents: %w", err)
	}
	var ag prompt.AgentsConfig
	if err := json.Unmarshal(data, &ag); err != nil {
		return nil, fmt.Errorf("parsing agents: %w", err)
	}
	return &ag, nil
}

// parseSkillsFromConfig returns skill source configs from the loaded arena config.
// Paths are converted back to relative (relative to config dir) for portability.
func parseSkillsFromConfig(cfg *config.Config) []prompt.SkillSourceConfig {
	if len(cfg.LoadedSkillSources) == 0 {
		return nil
	}

	result := make([]prompt.SkillSourceConfig, len(cfg.LoadedSkillSources))
	for i, src := range cfg.LoadedSkillSources {
		result[i] = src
		// Convert paths back to relative (to config dir) for pack portability.
		if dir := src.EffectiveDir(); dir != "" && cfg.ConfigDir != "" {
			if rel, err := filepath.Rel(cfg.ConfigDir, dir); err == nil {
				result[i].Dir = ""
				result[i].Path = rel
			}
		}
	}
	return result
}

// runSkillValidation validates skill configurations in a pack, returning errors and warnings.
func runSkillValidation(pack *prompt.Pack, dir string) (fatalErrors, warnings []string) {
	fatalErrors = ValidateSkillErrors(pack, dir)
	warnings = ValidateSkills(pack, dir)
	return fatalErrors, warnings
}

// validateSchema validates the compiled pack JSON against the PromptPack schema.
func validateSchema(packJSON []byte) error {
	packSchemaURL := schema.ExtractSchemaURL(packJSON)

	schemaLoader, err := schema.GetSchemaLoader(packSchemaURL)
	if err != nil {
		// Schema might not be available — treat as non-fatal but return error
		return fmt.Errorf("schema validation could not be performed: %w", err)
	}

	result, err := schema.ValidateJSONAgainstLoader(packJSON, schemaLoader)
	if err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	if !result.Valid {
		var errs []string
		for _, e := range result.Errors {
			errs = append(errs, e.Error())
		}
		return fmt.Errorf("pack failed schema validation: %v", errs)
	}

	return nil
}

// Pre-compiled regexes for sanitizePackID.
var (
	reNonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]`)
	reMultipleDashes  = regexp.MustCompile(`-+`)
)

// sanitizePackID converts a folder name to a valid pack ID.
func sanitizePackID(name string) string {
	result := strings.ToLower(name)
	result = strings.ReplaceAll(result, " ", "-")
	result = reNonAlphanumDash.ReplaceAllString(result, "")
	result = reMultipleDashes.ReplaceAllString(result, "-")
	result = strings.Trim(result, "-")
	return result
}
