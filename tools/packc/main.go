package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

const version = "v0.1.0"

const warningFormat = "  - %s\n"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "compile":
		compileCommand()
	case "compile-prompt":
		compilePromptCommand()
	case "validate":
		validateCommand()
	case "inspect":
		inspectCommand()
	case "version":
		fmt.Printf("packc %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`packc - PromptKit Pack Compiler

Usage:
  packc compile [options]         Compile ALL prompts from arena.yaml into a single pack
  packc compile-prompt [options]  Compile a single prompt to pack format
  packc validate <pack-file>      Validate a pack file
  packc inspect <pack-file>       Display pack information
  packc version                   Show version
  packc help                      Show this help

Compile Command (main):
  packc compile --config <arena.yaml> --output <pack-file> --id <pack-id>

  Options:
    --config      Path to arena.yaml file (required)
    --output      Output pack file path (required)
    --id          Pack ID (e.g., "customer-support") (required)

Compile Prompt Command (single prompt):
  packc compile-prompt --prompt <yaml-file> --output <json-file> [--config-dir <dir>]

  Options:
    --prompt      Path to prompt YAML file (required)
    --tools       Path to tools directory (optional)
    --output      Output pack file path (required)
    --config-dir  Base directory for config files (default: current directory)

Examples:
  # Compile all prompts into single pack (most common)
  packc compile --config arena.yaml --output packs/customer-support-pack.json --id customer-support

  # Compile single prompt
  packc compile-prompt --prompt prompts/support.yaml --output packs/support.json

  # Validate a pack
  packc validate packs/support.json

  # Inspect a pack
  packc inspect packs/support.json`)
}

func compileCommand() {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	configFile := fs.String("config", "arena.yaml", "Path to arena.yaml file")
	outputFile := fs.String("output", "", "Output pack file path")
	packID := fs.String("id", "", "Pack ID (e.g., 'customer-support')")

	fs.Parse(os.Args[2:])

	if *outputFile == "" || *packID == "" {
		fmt.Fprintln(os.Stderr, "Error: --output and --id are required")
		fs.Usage()
		os.Exit(1)
	}

	// Load arena config
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading arena config: %v\n", err)
		os.Exit(1)
	}

	// Create memory repository
	memRepo := memory.NewPromptRepository()

	// Register all pre-loaded prompt configs
	for _, promptData := range cfg.LoadedPromptConfigs {
		if promptData.Config == nil {
			continue
		}

		// Type assert the config
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

	// Create registry with memory repository
	registry := prompt.NewRegistryWithRepository(memRepo)

	if registry == nil {
		fmt.Fprintln(os.Stderr, "No prompt configs found in arena.yaml")
		os.Exit(1)
	}

	fmt.Printf("Loaded %d prompt configs from memory repository\n", len(cfg.LoadedPromptConfigs))

	// Validate media references
	configDir := filepath.Dir(*configFile)
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

	compiler := prompt.NewPackCompiler(registry)

	fmt.Printf("Compiling %d prompts into pack '%s'...\n", len(cfg.PromptConfigs), *packID)

	// Compile all prompts into a single pack
	pack, err := compiler.CompileFromRegistry(*packID, fmt.Sprintf("packc-%s", version))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compilation failed: %v\n", err)
		os.Exit(1)
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal pack: %v\n", err)
		os.Exit(1)
	}

	// Write to file
	if err := os.WriteFile(*outputFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write pack file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Pack compiled successfully: %s\n", *outputFile)
	fmt.Printf("  Contains %d prompts: %v\n", len(pack.Prompts), pack.ListPrompts())
}

func compilePromptCommand() {
	fs := flag.NewFlagSet("compile-prompt", flag.ExitOnError)
	promptFile := fs.String("prompt", "", "Path to prompt YAML file")
	outputFile := fs.String("output", "", "Output pack file path")

	fs.Parse(os.Args[2:])

	if *promptFile == "" || *outputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --prompt and --output are required")
		fs.Usage()
		os.Exit(1)
	}

	// Read and parse prompt file
	data, err := os.ReadFile(*promptFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading prompt file: %v\n", err)
		os.Exit(1)
	}

	promptConfig, err := prompt.ParseConfig(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing prompt config: %v\n", err)
		os.Exit(1)
	}

	taskType := promptConfig.Spec.TaskType

	// Validate media references
	promptDir := filepath.Dir(*promptFile)
	warnings := validateMediaReferences(promptConfig, promptDir)
	if len(warnings) > 0 {
		fmt.Printf("⚠ Media validation warnings:\n")
		for _, w := range warnings {
			fmt.Printf(warningFormat, w)
		}
	}

	// Create memory repository and register the prompt
	memRepo := memory.NewPromptRepository()
	if err := memRepo.SavePrompt(promptConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving prompt to repository: %v\n", err)
		os.Exit(1)
	}

	// Create registry with memory repository
	registry := prompt.NewRegistryWithRepository(memRepo)

	// Create compiler and compile
	compiler := prompt.NewPackCompiler(registry)

	fmt.Printf("Compiling prompt '%s' to pack...\n", taskType)

	if err := compiler.CompileToFile(taskType, *outputFile, fmt.Sprintf("packc-%s", version)); err != nil {
		fmt.Fprintf(os.Stderr, "Compilation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Pack compiled successfully: %s\n", *outputFile)
}

func validateCommand() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Error: pack file path required")
		fmt.Fprintln(os.Stderr, "Usage: packc validate <pack-file>")
		os.Exit(1)
	}

	packFile := os.Args[2]

	fmt.Printf("Validating pack: %s\n", packFile)

	pack, err := prompt.LoadPack(packFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading pack: %v\n", err)
		os.Exit(1)
	}

	warnings := pack.Validate()

	if len(warnings) == 0 {
		fmt.Println("✓ Pack is valid")
	} else {
		fmt.Printf("⚠ Pack has %d warnings:\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf(warningFormat, w)
		}
		os.Exit(1)
	}
}

func inspectCommand() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Error: pack file path required")
		fmt.Fprintln(os.Stderr, "Usage: packc inspect <pack-file>")
		os.Exit(1)
	}

	packFile := os.Args[2]

	pack, err := prompt.LoadPack(packFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading pack: %v\n", err)
		os.Exit(1)
	}

	printPackInfo(pack)
	printTemplateEngine(pack)
	printPrompts(pack)
	printFragments(pack)
	printMetadata(pack)
	printCompilationInfo(pack)

	fmt.Println()
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
