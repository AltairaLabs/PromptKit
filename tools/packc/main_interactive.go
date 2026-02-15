package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/pflag"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
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
	case "completion":
		completionCommand()
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
  packc completion <shell>        Generate shell completion script (bash, zsh, fish, powershell)

Compile Command (main):
  packc compile [-c <arena.yaml>] [-o <pack-file>] [--id <pack-id>]

  Options:
    -c, --config  Path to arena.yaml file (default: config.arena.yaml)
    -o, --output  Output pack file path (default: {id}.pack.json)
        --id      Pack ID (default: current folder name, sanitized)

Compile Prompt Command (single prompt):
  packc compile-prompt -p <yaml-file> -o <json-file>

  Options:
    -p, --prompt  Path to prompt YAML file (required)
    -o, --output  Output pack file path (required)

Examples:
  # Compile using all defaults (config.arena.yaml, folder name as id)
  packc compile

  # Compile with custom config
  packc compile -c arena.yaml

  # Compile with all options specified
  packc compile -c arena.yaml -o packs/customer-support-pack.json --id customer-support

  # Compile single prompt
  packc compile-prompt -p prompts/support.yaml -o packs/support.json

  # Validate a pack
  packc validate packs/support.json

  # Inspect a pack
  packc inspect packs/support.json

  # Generate shell completions
  packc completion bash >> ~/.bashrc
  packc completion zsh >> ~/.zshrc`)
}

// compileFlags holds the parsed flags for the compile command.
type compileFlags struct {
	configFile string
	outputFile string
	packID     string
}

// sanitizePackID converts a folder name to a valid pack ID.
// It converts to lowercase, replaces spaces with dashes, and removes special characters.
func sanitizePackID(name string) string {
	// Convert to lowercase
	result := strings.ToLower(name)
	// Replace spaces with dashes
	result = strings.ReplaceAll(result, " ", "-")
	// Remove all characters except alphanumeric and dashes
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	result = reg.ReplaceAllString(result, "")
	// Clean up multiple consecutive dashes
	reg = regexp.MustCompile(`-+`)
	result = reg.ReplaceAllString(result, "-")
	// Trim leading/trailing dashes
	result = strings.Trim(result, "-")
	return result
}

// getDefaultPackID returns a default pack ID based on the current directory name.
func getDefaultPackID() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "pack"
	}
	return sanitizePackID(filepath.Base(cwd))
}

// parseCompileFlags parses and validates compile command flags.
func parseCompileFlags() compileFlags {
	fs := pflag.NewFlagSet("compile", pflag.ExitOnError)
	configFile := fs.StringP("config", "c", "config.arena.yaml", "Path to arena.yaml file")
	outputFile := fs.StringP("output", "o", "", "Output pack file path (default: {id}.pack.json)")
	packID := fs.String("id", "", "Pack ID (default: current folder name, sanitized)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Apply smart defaults
	resolvedID := *packID
	if resolvedID == "" {
		resolvedID = getDefaultPackID()
	}

	resolvedOutput := *outputFile
	if resolvedOutput == "" {
		resolvedOutput = resolvedID + ".pack.json"
	}

	return compileFlags{
		configFile: *configFile,
		outputFile: resolvedOutput,
		packID:     resolvedID,
	}
}

// validateAndWritePack validates the pack against schema and writes it to the output file.
func validateAndWritePack(data []byte, outputFile string) {
	fmt.Printf("Validating pack against schema...\n")
	validationResult, err := ValidatePackAgainstSchema(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ Schema validation could not be performed: %v\n", err)
		// Continue anyway - schema might not be available
	} else if !validationResult.Valid {
		fmt.Fprintf(os.Stderr, "⚠ Pack failed schema validation:\n")
		for _, validationErr := range validationResult.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", validationErr)
		}
		os.Exit(1)
	} else {
		fmt.Printf("✓ Pack validated against schema\n")
	}

	if err := os.WriteFile(outputFile, data, outputFilePerm); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write pack file: %v\n", err)
		os.Exit(1)
	}
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

func compileCommand() {
	flags := parseCompileFlags()

	cfg := mustLoadConfig(flags.configFile)
	memRepo := buildMemoryRepo(cfg)
	registry := prompt.NewRegistryWithRepository(memRepo)
	if registry == nil {
		fmt.Fprintln(os.Stderr, "No prompt configs found in arena.yaml")
		os.Exit(1)
	}

	fmt.Printf("Loaded %d prompt configs from memory repository\n", len(cfg.LoadedPromptConfigs))

	configDir := filepath.Dir(flags.configFile)
	validateLoadedMedia(cfg, configDir)

	compiler := prompt.NewPackCompiler(registry)

	fmt.Printf("Compiling %d prompts into pack '%s'...\n", len(cfg.PromptConfigs), flags.packID)

	// Parse tools from loaded tool data (per PromptPack spec Section 9)
	parsedTools := parseToolsFromConfig(cfg)
	if len(parsedTools) > 0 {
		fmt.Printf("Including %d tool definitions in pack\n", len(parsedTools))
	}

	// Parse pack-level evals from arena config
	packEvals := parsePackEvalsFromConfig(cfg)
	if len(packEvals) > 0 {
		fmt.Printf("Including %d pack-level eval definitions in pack\n", len(packEvals))
	}

	// Parse workflow and agents from arena config
	workflowConfig := parseWorkflowFromConfig(cfg)
	agentsConfig := parseAgentsFromConfig(cfg)

	// Build compile options
	var compileOpts []prompt.CompileOption
	if workflowConfig != nil {
		compileOpts = append(compileOpts, prompt.WithWorkflow(workflowConfig))
		fmt.Printf("Including workflow configuration in pack\n")
	}
	if agentsConfig != nil {
		compileOpts = append(compileOpts, prompt.WithAgents(agentsConfig))
		fmt.Printf("Including agents configuration in pack\n")
	}

	// Compile all prompts into a single pack with tool definitions and pack evals
	compilerVer := fmt.Sprintf("packc-%s", version)
	pack, err := compiler.CompileFromRegistryWithOptions(
		flags.packID, compilerVer, parsedTools, packEvals, compileOpts...,
	)
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

	validateAndWritePack(data, flags.outputFile)
	printPackSummary(pack, flags.outputFile)
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
	fs := pflag.NewFlagSet("compile-prompt", pflag.ExitOnError)
	promptFile := fs.StringP("prompt", "p", "", "Path to prompt YAML file")
	outputFile := fs.StringP("output", "o", "", "Output pack file path")

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

	// Read the pack file for schema validation
	packData, err := os.ReadFile(packFile) //nolint:gosec // packFile is from command line args
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading pack file: %v\n", err)
		os.Exit(1)
	}

	// Validate against PromptPack schema
	fmt.Printf("Validating against PromptPack schema...\n")
	schemaResult, err := ValidatePackAgainstSchema(packData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ Schema validation could not be performed: %v\n", err)
	} else if !schemaResult.Valid {
		fmt.Fprintf(os.Stderr, "✗ Pack failed schema validation:\n")
		for _, validationErr := range schemaResult.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", validationErr)
		}
		os.Exit(1)
	} else {
		fmt.Printf("✓ Schema validation passed\n")
	}

	// Load and validate pack structure
	pack, err := prompt.LoadPack(packFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading pack: %v\n", err)
		os.Exit(1)
	}

	warnings := pack.Validate()

	if len(warnings) == 0 {
		fmt.Println("✓ Pack structure is valid")
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
	printWorkflow(pack)
	printAgents(pack)
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

// parsePackEvalsFromConfig returns pack-level eval definitions from arena config
func parsePackEvalsFromConfig(cfg *config.Config) []evals.EvalDef {
	return cfg.PackEvals
}

// parseWorkflowFromConfig parses workflow config from arena config.
// The config loader deserializes YAML into interface{}, so we re-marshal to JSON
// then unmarshal into the typed struct.
func parseWorkflowFromConfig(cfg *config.Config) *prompt.WorkflowConfig {
	if cfg.Workflow == nil {
		return nil
	}
	data, err := json.Marshal(cfg.Workflow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal workflow config: %v\n", err)
		return nil
	}
	var wf prompt.WorkflowConfig
	if err := json.Unmarshal(data, &wf); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse workflow config: %v\n", err)
		return nil
	}
	return &wf
}

// parseAgentsFromConfig parses agents config from arena config.
func parseAgentsFromConfig(cfg *config.Config) *prompt.AgentsConfig {
	if cfg.Agents == nil {
		return nil
	}
	data, err := json.Marshal(cfg.Agents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal agents config: %v\n", err)
		return nil
	}
	var ag prompt.AgentsConfig
	if err := json.Unmarshal(data, &ag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse agents config: %v\n", err)
		return nil
	}
	return &ag
}

func completionCommand() {
	if len(os.Args) < minArgsWithPackArg {
		fmt.Fprintln(os.Stderr, "Error: shell type required (bash, zsh, fish, powershell)")
		fmt.Fprintln(os.Stderr, "Usage: packc completion <shell>")
		os.Exit(1)
	}

	shell := os.Args[2]

	switch shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	case "powershell":
		fmt.Print(powershellCompletion)
	default:
		fmt.Fprintf(os.Stderr, "Unknown shell: %s\n", shell)
		fmt.Fprintln(os.Stderr, "Supported shells: bash, zsh, fish, powershell")
		os.Exit(1)
	}
}

const bashCompletion = `# packc bash completion
_packc_completions() {
    local cur prev commands
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    commands="compile compile-prompt validate inspect version help completion"

    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=($(compgen -W "${commands}" -- "${cur}"))
        return 0
    fi

    case "${prev}" in
        compile)
            COMPREPLY=($(compgen -W "-c --config -o --output --id" -- "${cur}"))
            ;;
        compile-prompt)
            COMPREPLY=($(compgen -W "-p --prompt -o --output" -- "${cur}"))
            ;;
        -c|--config)
            COMPREPLY=($(compgen -f -X '!*.yaml' -- "${cur}"))
            ;;
        -o|--output)
            COMPREPLY=($(compgen -f -X '!*.json' -- "${cur}"))
            ;;
        -p|--prompt)
            COMPREPLY=($(compgen -f -X '!*.yaml' -- "${cur}"))
            ;;
        validate|inspect)
            COMPREPLY=($(compgen -f -X '!*.json' -- "${cur}"))
            ;;
        completion)
            COMPREPLY=($(compgen -W "bash zsh fish powershell" -- "${cur}"))
            ;;
    esac
}
complete -F _packc_completions packc
`

const zshCompletion = `#compdef packc

_packc() {
    local -a commands
    commands=(
        'compile:Compile ALL prompts from arena.yaml into a single pack'
        'compile-prompt:Compile a single prompt to pack format'
        'validate:Validate a pack file'
        'inspect:Display pack information'
        'version:Show version'
        'help:Show help'
        'completion:Generate shell completion script'
    )

    _arguments -C \
        '1: :->command' \
        '*: :->args'

    case $state in
        command)
            _describe 'command' commands
            ;;
        args)
            case $words[2] in
                compile)
                    _arguments \
                        '-c[Path to arena.yaml file]:config file:_files -g "*.yaml"' \
                        '--config[Path to arena.yaml file]:config file:_files -g "*.yaml"' \
                        '-o[Output pack file path]:output file:_files -g "*.json"' \
                        '--output[Output pack file path]:output file:_files -g "*.json"' \
                        '--id[Pack ID]:pack id:'
                    ;;
                compile-prompt)
                    _arguments \
                        '-p[Path to prompt YAML file]:prompt file:_files -g "*.yaml"' \
                        '--prompt[Path to prompt YAML file]:prompt file:_files -g "*.yaml"' \
                        '-o[Output pack file path]:output file:_files -g "*.json"' \
                        '--output[Output pack file path]:output file:_files -g "*.json"'
                    ;;
                validate|inspect)
                    _arguments '*:pack file:_files -g "*.json"'
                    ;;
                completion)
                    _arguments '1:shell:(bash zsh fish powershell)'
                    ;;
            esac
            ;;
    esac
}

_packc "$@"
`

const fishCompletion = `# packc fish completion
complete -c packc -f

# Commands
complete -c packc -n '__fish_use_subcommand' -a 'compile' -d 'Compile ALL prompts from arena.yaml into a single pack'
complete -c packc -n '__fish_use_subcommand' -a 'compile-prompt' -d 'Compile a single prompt to pack format'
complete -c packc -n '__fish_use_subcommand' -a 'validate' -d 'Validate a pack file'
complete -c packc -n '__fish_use_subcommand' -a 'inspect' -d 'Display pack information'
complete -c packc -n '__fish_use_subcommand' -a 'version' -d 'Show version'
complete -c packc -n '__fish_use_subcommand' -a 'help' -d 'Show help'
complete -c packc -n '__fish_use_subcommand' -a 'completion' -d 'Generate shell completion script'

# compile options
complete -c packc -n '__fish_seen_subcommand_from compile' -s c -l config -d 'Path to arena.yaml file' -r -F
complete -c packc -n '__fish_seen_subcommand_from compile' -s o -l output -d 'Output pack file path' -r -F
complete -c packc -n '__fish_seen_subcommand_from compile' -l id -d 'Pack ID' -r

# compile-prompt options
complete -c packc -n '__fish_seen_subcommand_from compile-prompt' -s p -l prompt -d 'Path to prompt YAML file' -r -F
complete -c packc -n '__fish_seen_subcommand_from compile-prompt' -s o -l output -d 'Output pack file path' -r -F

# completion shells
complete -c packc -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish powershell'
`

const powershellCompletion = `# packc PowerShell completion
Register-ArgumentCompleter -Native -CommandName packc -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commands = @{
        'compile' = 'Compile ALL prompts from arena.yaml into a single pack'
        'compile-prompt' = 'Compile a single prompt to pack format'
        'validate' = 'Validate a pack file'
        'inspect' = 'Display pack information'
        'version' = 'Show version'
        'help' = 'Show help'
        'completion' = 'Generate shell completion script'
    }

    $elements = $commandAst.CommandElements
    $command = $null
    if ($elements.Count -gt 1) {
        $command = $elements[1].Extent.Text
    }

    if ($elements.Count -eq 1 -or ($elements.Count -eq 2 -and $wordToComplete -eq $command)) {
        $commands.Keys | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $commands[$_])
        }
        return
    }

    switch ($command) {
        'compile' {
            $opts = @('-c', '--config', '-o', '--output', '--id')
            $opts | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterName', $_)
            }
        }
        'compile-prompt' {
            @('-p', '--prompt', '-o', '--output') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterName', $_)
            }
        }
        'completion' {
            @('bash', 'zsh', 'fish', 'powershell') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    }
}
`
