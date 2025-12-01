package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

const outputFilePerm = 0o600

const version = "v0.1.0"

const warningFormat = "  - %s\n"

func buildMemoryRepo(cfg *config.Config) *memory.PromptRepository {
	memRepo := memory.NewPromptRepository()

	for _, promptData := range cfg.LoadedPromptConfigs {
		if promptData.Config == nil {
			continue
		}

		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: prompt config %s has invalid type\n", promptData.FilePath)
			os.Exit(1)
		}

		if err := memRepo.SavePrompt(promptConfig); err != nil {
			fmt.Fprintf(os.Stderr, "Error registering prompt %s: %v\n", promptData.FilePath, err)
			os.Exit(1)
		}
	}

	return memRepo
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
	for taskType, p := range pack.Prompts {
		printPromptDetails(taskType, p)
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
	return names
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
