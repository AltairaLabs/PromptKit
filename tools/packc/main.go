package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

const outputFilePerm = 0o600

var version = "dev" //nolint:gochecknoglobals // overridden via ldflags at build time

const warningFormat = "  - %s\n"

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

func validateLoadedMedia(cfg *config.Config, configDir string) {
	for _, promptData := range cfg.LoadedPromptConfigs {
		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			continue
		}

		warnings := validateMediaReferences(promptConfig, configDir)
		if len(warnings) > 0 {
			fmt.Printf("⚠ Media validation warnings for %s:\n", promptConfig.Spec.TaskType)
			for _, w := range warnings {
				fmt.Printf(warningFormat, w)
			}
		}
	}
}

func printPackInfo(pack *prompt.Pack) {
	fmt.Printf("\n=== Pack Information ===\n")
	fmt.Printf("Pack: %s\n", pack.Name)
	fmt.Printf("ID: %s\n", pack.ID)
	fmt.Printf("Version: %s\n", pack.Version)
	if pack.Description != "" {
		fmt.Printf("Description: %s\n", pack.Description)
	}
}

func printTemplateEngine(pack *prompt.Pack) {
	if pack.TemplateEngine != nil {
		fmt.Printf("\nTemplate Engine: %s (%s)\n", pack.TemplateEngine.Version, pack.TemplateEngine.Syntax)
		if len(pack.TemplateEngine.Features) > 0 {
			fmt.Printf("Features: %v\n", pack.TemplateEngine.Features)
		}
	}
}

func printPrompts(pack *prompt.Pack) {
	fmt.Printf("\n=== Prompts (%d) ===\n", len(pack.Prompts))
	taskTypes := make([]string, 0, len(pack.Prompts))
	for taskType := range pack.Prompts {
		taskTypes = append(taskTypes, taskType)
	}
	sort.Strings(taskTypes)
	for _, taskType := range taskTypes {
		printPromptDetails(taskType, pack.Prompts[taskType])
	}
}

func printPromptDetails(taskType string, p *prompt.PackPrompt) {
	fmt.Printf("\n[%s]\n", taskType)
	fmt.Printf("  Name: %s\n", p.Name)
	if p.Description != "" {
		fmt.Printf("  Description: %s\n", p.Description)
	}
	fmt.Printf("  Version: %s\n", p.Version)

	printPromptVariables(p)
	printPromptTools(p)
	printPromptValidators(p)
	printPromptTestedModels(p)
}

func printPromptVariables(p *prompt.PackPrompt) {
	requiredVars := []string{}
	optionalVars := []string{}
	for _, v := range p.Variables {
		if v.Required {
			requiredVars = append(requiredVars, v.Name)
		} else {
			optionalVars = append(optionalVars, v.Name)
		}
	}
	fmt.Printf("  Variables: %d required, %d optional\n", len(requiredVars), len(optionalVars))
	if len(requiredVars) > 0 {
		fmt.Printf("    Required: %v\n", requiredVars)
	}
}

func printPromptTools(p *prompt.PackPrompt) {
	if len(p.Tools) > 0 {
		fmt.Printf("  Tools (%d): %v\n", len(p.Tools), p.Tools)
	}
}

func printPromptValidators(p *prompt.PackPrompt) {
	if len(p.Validators) > 0 {
		fmt.Printf("  Validators (%d):\n", len(p.Validators))
		for _, v := range p.Validators {
			enabled := "disabled"
			if v.Enabled != nil && *v.Enabled {
				enabled = "enabled"
			}
			fmt.Printf("    - %s (%s)\n", v.Type, enabled)
		}
	}
}

func printPromptTestedModels(p *prompt.PackPrompt) {
	if len(p.TestedModels) > 0 {
		fmt.Printf("  Tested Models (%d):\n", len(p.TestedModels))
		for _, tm := range p.TestedModels {
			fmt.Printf("    - %s/%s: %.1f%% success, avg %d tokens\n",
				tm.Provider, tm.Model, tm.SuccessRate*100, tm.AvgTokens)
		}
	}
}

func printFragments(pack *prompt.Pack) {
	if len(pack.Fragments) > 0 {
		fmt.Printf("\nShared Fragments (%d): %v\n", len(pack.Fragments), getFragmentNames(pack.Fragments))
	}
}

func printWorkflow(pack *prompt.Pack) {
	if pack.Workflow == nil {
		return
	}
	fmt.Printf("\n=== Workflow (v%d) ===\n", pack.Workflow.Version)
	fmt.Printf("Entry: %s\n", pack.Workflow.Entry)
	fmt.Printf("States (%d):\n", len(pack.Workflow.States))
	stateNames := make([]string, 0, len(pack.Workflow.States))
	for name := range pack.Workflow.States {
		stateNames = append(stateNames, name)
	}
	sort.Strings(stateNames)
	for _, name := range stateNames {
		printWorkflowState(name, pack.Workflow.States[name])
	}
}

func printWorkflowState(name string, state *prompt.WorkflowState) {
	fmt.Printf("  [%s] prompt_task=%s\n", name, state.PromptTask)
	if state.Description != "" {
		fmt.Printf("    Description: %s\n", state.Description)
	}
	if len(state.OnEvent) > 0 {
		events := make([]string, 0, len(state.OnEvent))
		for event := range state.OnEvent {
			events = append(events, event)
		}
		sort.Strings(events)
		for _, event := range events {
			fmt.Printf("    on %s → %s\n", event, state.OnEvent[event])
		}
	}
	if state.Persistence != "" {
		fmt.Printf("    Persistence: %s\n", state.Persistence)
	}
	if state.Orchestration != "" {
		fmt.Printf("    Orchestration: %s\n", state.Orchestration)
	}
}

func printAgents(pack *prompt.Pack) {
	if pack.Agents == nil {
		return
	}
	fmt.Printf("\n=== Agents ===\n")
	fmt.Printf("Entry: %s\n", pack.Agents.Entry)
	fmt.Printf("Members (%d):\n", len(pack.Agents.Members))
	memberNames := make([]string, 0, len(pack.Agents.Members))
	for name := range pack.Agents.Members {
		memberNames = append(memberNames, name)
	}
	sort.Strings(memberNames)
	for _, name := range memberNames {
		agent := pack.Agents.Members[name]
		fmt.Printf("  [%s]\n", name)
		if agent.Description != "" {
			fmt.Printf("    Description: %s\n", agent.Description)
		}
		if len(agent.Tags) > 0 {
			fmt.Printf("    Tags: %v\n", agent.Tags)
		}
		if len(agent.InputModes) > 0 {
			fmt.Printf("    Input modes: %v\n", agent.InputModes)
		}
		if len(agent.OutputModes) > 0 {
			fmt.Printf("    Output modes: %v\n", agent.OutputModes)
		}
	}
}

func printMetadata(pack *prompt.Pack) {
	if pack.Metadata != nil {
		fmt.Printf("\nPack Metadata:\n")
		if pack.Metadata.Domain != "" {
			fmt.Printf("  Domain: %s\n", pack.Metadata.Domain)
		}
		if pack.Metadata.Language != "" {
			fmt.Printf("  Language: %s\n", pack.Metadata.Language)
		}
		if len(pack.Metadata.Tags) > 0 {
			fmt.Printf("  Tags: %v\n", pack.Metadata.Tags)
		}
	}
}

func printCompilationInfo(pack *prompt.Pack) {
	if pack.Compilation != nil {
		fmt.Printf("\nCompilation: %s, %s, Schema: %s\n",
			pack.Compilation.CompiledWith,
			pack.Compilation.CreatedAt,
			pack.Compilation.Schema)
	}
}

func getFragmentNames(fragments map[string]string) []string {
	names := make([]string, 0, len(fragments))
	for name := range fragments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// runSkillValidation validates skill configurations in a pack, returning errors and warnings.
// Returns fatal errors (should abort) and non-fatal warnings (informational).
func runSkillValidation(pack *prompt.Pack, dir string) (fatalErrors, warnings []string) {
	fatalErrors = ValidateSkillErrors(pack, dir)
	warnings = ValidateSkills(pack, dir)
	return fatalErrors, warnings
}

// printPackSummary prints a summary of the compiled pack.
func printPackSummary(pack *prompt.Pack, outputFile string) {
	fmt.Printf("✓ Pack compiled successfully: %s\n", outputFile)
	fmt.Printf("  Contains %d prompts: %v\n", len(pack.Prompts), pack.ListPrompts())
	if len(pack.Tools) > 0 {
		toolNames := make([]string, 0, len(pack.Tools))
		for name := range pack.Tools {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		fmt.Printf("  Contains %d tools: %v\n", len(pack.Tools), toolNames)
	}
	if len(pack.Evals) > 0 {
		fmt.Printf("  Pack-level evals: %d\n", len(pack.Evals))
	}
	if pack.Workflow != nil {
		fmt.Printf("  Workflow: %d states (entry: %s)\n", len(pack.Workflow.States), pack.Workflow.Entry)
	}
	if pack.Agents != nil {
		fmt.Printf("  Agents: %d members (entry: %s)\n", len(pack.Agents.Members), pack.Agents.Entry)
	}
}

// validateMediaReferences checks if media files referenced in examples exist
func validateMediaReferences(config *prompt.Config, baseDir string) []string {
	var warnings []string

	if config.Spec.MediaConfig == nil || !config.Spec.MediaConfig.Enabled {
		return warnings
	}

	for _, example := range config.Spec.MediaConfig.Examples {
		warnings = append(warnings, validateExampleMediaReferences(example, baseDir)...)
	}

	return warnings
}

// validateExampleMediaReferences validates media references in a single example
func validateExampleMediaReferences(example prompt.MultimodalExample, baseDir string) []string {
	var warnings []string

	for i, part := range example.Parts {
		if part.Media == nil || part.Media.FilePath == "" {
			continue
		}

		// Only validate file paths (not URLs)
		filePath := part.Media.FilePath
		// If path is relative, resolve from base directory
		if !filepath.IsAbs(filePath) && baseDir != "" {
			filePath = filepath.Join(baseDir, filePath)
		}

		// Check if file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf(
				"Media file not found: %s (example: %s, part %d)",
				part.Media.FilePath, example.Name, i))
		}
	}

	return warnings
}
