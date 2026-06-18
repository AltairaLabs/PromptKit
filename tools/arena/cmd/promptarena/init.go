package main

import (
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new Arena test project",
	Long: `Create a new Arena test project from a template.

This command creates a complete project structure with:
- Configuration files (arena.yaml)
- Prompt definitions
- Provider configurations
- Example scenarios
- Documentation

Examples:
  # Interactive mode (recommended)
  promptarena init

  # Quick start with defaults
  promptarena init my-project --quick

  # Use specific template
  promptarena init my-project --template customer-support

  # From local template
  promptarena init my-project --template ./path/to/template.yaml`,
	RunE: runInit,
}

const projectNameVar = "project_name"

var (
	initTemplate      string
	initQuick         bool
	initNoGit         bool
	initNoEnv         bool
	initProvider      string
	initOutputDir     string
	initTemplateIndex string
	initTemplateCache string
	initRepoConfig    string
	initVerbose       bool
	initNoAgent       bool
)

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVar(&initTemplate, "template", "quick-start", "Template to use")
	initCmd.Flags().BoolVar(&initQuick, "quick", false, "Use defaults, skip interactive prompts")
	initCmd.Flags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	initCmd.Flags().BoolVar(&initNoEnv, "no-env", false, "Skip .env file creation")
	initCmd.Flags().StringVar(&initProvider, "provider", "", "Provider to configure (openai, anthropic, google, mock)")
	initCmd.Flags().StringVar(&initOutputDir, "output", ".", "Output directory")
	initCmd.Flags().BoolVar(&initVerbose, "verbose", false, "Show detailed generation progress")
	initCmd.Flags().StringVar(&initTemplateIndex, "template-index", templates.DefaultRepoName,
		"Template repo name or index URL/path for remote templates")
	initCmd.Flags().StringVar(&initRepoConfig, "repo-config", templates.DefaultRepoConfigPath(),
		"Template repo config file")
	initCmd.Flags().StringVar(&initTemplateCache, "template-cache", templates.DefaultCacheDir(),
		"Cache directory for remote templates")
	initCmd.Flags().BoolVar(&initNoAgent, "no-agent", false,
		"Skip writing the AI-agent brief (.claude skill + AGENTS.md)")

	// Register dynamic completions (must be after flags are defined)
	RegisterInitCompletions()
}

func runInit(cmd *cobra.Command, args []string) error {
	// Get project name
	projectName := getProjectName(args)

	// Load template
	repoCfg, err := templates.LoadRepoConfig(initRepoConfig)
	if err != nil {
		return fmt.Errorf("load repo config: %w", err)
	}

	// Resolve repository for the template
	resolver := templates.NewRepoResolver(repoCfg)
	repo, _ := resolver.ResolveRepoForTemplate(initTemplate, initTemplateIndex)

	loader := templates.NewLoaderWithRepo(initTemplateCache, repo)
	tmpl, err := loader.Load(initTemplate)
	if err != nil {
		return fmt.Errorf("failed to load template %s: %w", initTemplate, err)
	}

	// Collect configuration
	config, err := collectConfiguration(cmd, tmpl, projectName)
	if err != nil {
		return fmt.Errorf("failed to collect configuration: %w", err)
	}

	// Generate project
	generator := templates.NewGenerator(tmpl, loader)
	result, err := generator.Generate(config)
	if err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	// Brief any AI coding agent that opens this project (unless opted out).
	agentBriefed := false
	if !initNoAgent && result.Success {
		if briefErr := writeAgentBrief(result); briefErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("agent brief: %v", briefErr))
		} else {
			agentBriefed = true
		}
	}

	// Display results
	printSuccessMessage(projectName, result, agentBriefed)

	return nil
}

func getProjectName(args []string) string {
	if len(args) > 0 {
		return args[0]
	}

	if !initQuick {
		prompt := promptui.Prompt{
			Label:   "Project name",
			Default: "my-arena-tests",
		}
		result, err := prompt.Run()
		if err == nil && result != "" {
			return result
		}
	}

	return "my-arena-tests"
}

func collectConfiguration(cmd *cobra.Command, tmpl *templates.Template, projectName string) (*templates.TemplateConfig, error) {
	config := &templates.TemplateConfig{
		ProjectName: projectName,
		OutputDir:   initOutputDir,
		Variables:   make(map[string]interface{}),
		Template:    tmpl,
		Verbose:     initVerbose,
	}

	config.Variables[projectNameVar] = projectName

	if initQuick {
		return collectQuickModeVariables(config, tmpl)
	}

	return collectInteractiveVariables(config, tmpl)
}

func collectQuickModeVariables(config *templates.TemplateConfig, tmpl *templates.Template) (*templates.TemplateConfig, error) {
	for _, v := range tmpl.Spec.Variables {
		if v.Name == projectNameVar {
			continue
		}
		if v.Default != nil {
			config.Variables[v.Name] = v.Default
		} else if v.Required {
			return nil, fmt.Errorf("required variable %s has no default value", v.Name)
		}
	}

	applyCommandLineOverrides(config)
	return config, nil
}

func applyCommandLineOverrides(config *templates.TemplateConfig) {
	if initProvider != "" {
		config.Variables["provider"] = initProvider
	}
	if initNoEnv {
		config.Variables["include_env"] = false
	}
}

func printSuccessMessage(projectName string, result *templates.GenerationResult, agentBriefed bool) {
	fmt.Println()
	fmt.Println("✨ Created", projectName+"/")
	fmt.Println()

	if len(result.FilesCreated) > 0 {
		fmt.Println("📁 Files created:")
		for _, file := range result.FilesCreated {
			fmt.Printf("   - %s\n", file)
		}
		fmt.Println()
	}

	if len(result.Warnings) > 0 {
		fmt.Println("⚠️  Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("   - %s\n", warning)
		}
		fmt.Println()
	}

	if agentBriefed {
		fmt.Println("📎 AI-agent brief written (.claude/skills + AGENTS.md) —")
		fmt.Println("   open this folder in Claude Code or Codex and it'll know PromptArena conventions.")
		fmt.Println()
	}

	fmt.Println("🚀 Next steps:")
	fmt.Printf("   1. cd %s\n", projectName)
	fmt.Println("   2. promptarena run")
	fmt.Println("   3. open out/report.html")
	fmt.Println()
	fmt.Println("📖 Need help? Visit https://promptkit.altairalabs.ai/arena/tutorials")
	fmt.Println()
	fmt.Println("✅ Happy testing!")
}
