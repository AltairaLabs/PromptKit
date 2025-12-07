package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const (
	minArgsForCommand  = 2
	minArgsWithPackArg = 3
)

func main() {
	if len(os.Args) < minArgsForCommand {
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

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *outputFile == "" || *packID == "" {
		fmt.Fprintln(os.Stderr, "Error: --output and --id are required")
		fs.Usage()
		os.Exit(1)
	}

	cfg := mustLoadConfig(*configFile)
	memRepo := buildMemoryRepo(cfg)
	registry := prompt.NewRegistryWithRepository(memRepo)
	if registry == nil {
		fmt.Fprintln(os.Stderr, "No prompt configs found in arena.yaml")
		os.Exit(1)
	}

	fmt.Printf("Loaded %d prompt configs from memory repository\n", len(cfg.LoadedPromptConfigs))

	configDir := filepath.Dir(*configFile)
	validateLoadedMedia(cfg, configDir)

	compiler := prompt.NewPackCompiler(registry)

	fmt.Printf("Compiling %d prompts into pack '%s'...\n", len(cfg.PromptConfigs), *packID)

	// Parse tools from loaded tool data (per PromptPack spec Section 9)
	parsedTools := parseToolsFromConfig(cfg)
	if len(parsedTools) > 0 {
		fmt.Printf("Including %d tool definitions in pack\n", len(parsedTools))
	}

	// Compile all prompts into a single pack with tool definitions
	pack, err := compiler.CompileFromRegistryWithParsedTools(*packID, fmt.Sprintf("packc-%s", version), parsedTools)
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
	if err := os.WriteFile(*outputFile, data, outputFilePerm); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write pack file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Pack compiled successfully: %s\n", *outputFile)
	fmt.Printf("  Contains %d prompts: %v\n", len(pack.Prompts), pack.ListPrompts())
	if len(pack.Tools) > 0 {
		toolNames := make([]string, 0, len(pack.Tools))
		for name := range pack.Tools {
			toolNames = append(toolNames, name)
		}
		fmt.Printf("  Contains %d tools: %v\n", len(pack.Tools), toolNames)
	}
}

func mustLoadConfig(configFile string) *config.Config {
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading arena config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func compilePromptCommand() {
	fs := flag.NewFlagSet("compile-prompt", flag.ExitOnError)
	promptFile := fs.String("prompt", "", "Path to prompt YAML file")
	outputFile := fs.String("output", "", "Output pack file path")

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

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
	memRepo := buildMemoryRepo(&config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			taskType: {
				FilePath: *promptFile,
				Config:   promptConfig,
			},
		},
	})

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
	if len(os.Args) < minArgsWithPackArg {
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
	if len(os.Args) < minArgsWithPackArg {
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

// parseToolsFromConfig parses raw tool YAML data from config into ParsedTool structs
// Uses the tools.Registry which handles YAML→JSON conversion properly
func parseToolsFromConfig(cfg *config.Config) []prompt.ParsedTool {
	var result []prompt.ParsedTool

	if len(cfg.LoadedTools) == 0 {
		return result
	}

	// Create a temporary registry to parse tools
	registry := tools.NewRegistry()

	for _, td := range cfg.LoadedTools {
		// Use registry's LoadToolFromBytes which handles YAML→JSON properly
		if err := registry.LoadToolFromBytes(td.FilePath, td.Data); err != nil {
			// Log warning but continue - tool may be invalid or not a tool file
			fmt.Fprintf(os.Stderr, "Warning: skipping tool %s: %v\n", td.FilePath, err)
			continue
		}
	}

	// Extract parsed tools from registry
	for name, tool := range registry.GetTools() {
		result = append(result, prompt.ParsedTool{
			Name:        name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	return result
}
