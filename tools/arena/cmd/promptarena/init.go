package main

import (
	"fmt"
	"strings"

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
)

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVar(&initTemplate, "template", "quick-start", "Template to use")
	initCmd.Flags().BoolVar(&initQuick, "quick", false, "Use defaults, skip interactive prompts")
	initCmd.Flags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	initCmd.Flags().BoolVar(&initNoEnv, "no-env", false, "Skip .env file creation")
	initCmd.Flags().StringVar(&initProvider, "provider", "", "Provider to configure (openai, anthropic, google, mock)")
	initCmd.Flags().StringVar(&initOutputDir, "output", ".", "Output directory")
	initCmd.Flags().StringVar(&initTemplateIndex, "template-index", templates.DefaultRepoName,
		"Template repo name or index URL/path for remote templates")
	initCmd.Flags().StringVar(&initRepoConfig, "repo-config", templates.DefaultRepoConfigPath(),
		"Template repo config file")
	initCmd.Flags().StringVar(&initTemplateCache, "template-cache", templates.DefaultCacheDir(),
		"Cache directory for remote templates")
}

func runInit(cmd *cobra.Command, args []string) error {
	// Get project name
	projectName := getProjectName(args)

	// Load template
	repoCfg, err := templates.LoadRepoConfig(initRepoConfig)
	if err != nil {
		return fmt.Errorf("load repo config: %w", err)
	}
	templates.DefaultIndex = templates.ResolveIndex(initTemplateIndex, repoCfg)
	loader := templates.NewLoader(initTemplateCache)
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

	// Display results
	printSuccessMessage(projectName, result)

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
	}

	config.Variables["project_name"] = projectName

	if initQuick {
		return collectQuickModeVariables(config, tmpl)
	}

	return collectInteractiveVariables(config, tmpl)
}

func collectQuickModeVariables(config *templates.TemplateConfig, tmpl *templates.Template) (*templates.TemplateConfig, error) {
	for _, v := range tmpl.Spec.Variables {
		if v.Name == "project_name" {
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

func collectInteractiveVariables(config *templates.TemplateConfig, tmpl *templates.Template) (*templates.TemplateConfig, error) {
	fmt.Println("🏟️  Welcome to PromptArena!")
	fmt.Println()
	fmt.Println("Let's set up your testing project.")
	fmt.Println()

	for _, v := range tmpl.Spec.Variables {
		if v.Name == "project_name" {
			continue
		}

		value, err := promptForVariable(v)
		if err != nil {
			return nil, err
		}
		config.Variables[v.Name] = value
	}

	return config, nil
}

func promptForVariable(v templates.Variable) (interface{}, error) {
	promptText := buildPromptText(v)

	switch v.Type {
	case "boolean":
		return promptForBoolean(promptText)
	case "select":
		return promptForSelect(promptText, v)
	case "array":
		return promptForArray(promptText, v)
	default:
		return promptForString(promptText, v)
	}
}

func buildPromptText(v templates.Variable) string {
	promptText := v.Prompt
	if promptText == "" {
		promptText = v.Name
	}
	if v.Description != "" {
		promptText = fmt.Sprintf("%s (%s)", promptText, v.Description)
	}
	return promptText
}

func promptForBoolean(promptText string) (interface{}, error) {
	prompt := promptui.Prompt{
		Label:     promptText,
		IsConfirm: true,
	}
	_, err := prompt.Run()
	if err != nil && err != promptui.ErrAbort {
		return nil, err
	}
	return err != promptui.ErrAbort, nil
}

func promptForSelect(promptText string, v templates.Variable) (interface{}, error) {
	prompt := promptui.Select{
		Label: promptText,
		Items: v.Options,
	}
	if v.Default != nil {
		defaultStr := fmt.Sprintf("%v", v.Default)
		for i, opt := range v.Options {
			if opt == defaultStr {
				prompt.CursorPos = i
				break
			}
		}
	}
	_, result, err := prompt.Run()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func promptForArray(promptText string, v templates.Variable) (interface{}, error) {
	defaultStr := getArrayDefaultString(v)
	prompt := promptui.Prompt{
		Label:   promptText + " (comma-separated)",
		Default: defaultStr,
	}
	result, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	items := strings.Split(result, ",")
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	return items, nil
}

func getArrayDefaultString(v templates.Variable) string {
	if v.Default == nil {
		return ""
	}
	if arr, ok := v.Default.([]interface{}); ok {
		strs := make([]string, len(arr))
		for i, item := range arr {
			strs[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(strs, ",")
	}
	return ""
}

func promptForString(promptText string, v templates.Variable) (interface{}, error) {
	defaultStr := ""
	if v.Default != nil {
		defaultStr = fmt.Sprintf("%v", v.Default)
	}

	prompt := promptui.Prompt{
		Label:   promptText,
		Default: defaultStr,
	}
	result, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	if v.Type == "number" {
		return parseNumber(result)
	}

	return result, nil
}

func parseNumber(result string) (interface{}, error) {
	var num float64
	if n, _ := fmt.Sscanf(result, "%f", &num); n == 1 {
		return num, nil
	}
	return result, nil
}

func printSuccessMessage(projectName string, result *templates.GenerationResult) {
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

	fmt.Println("🚀 Next steps:")
	fmt.Printf("   1. cd %s\n", projectName)
	fmt.Println("   2. promptarena run")
	fmt.Println("   3. open out/report.html")
	fmt.Println()
	fmt.Println("📖 Need help? Visit https://promptkit.altairalabs.ai/arena/tutorials")
	fmt.Println()
	fmt.Println("✅ Happy testing!")
}
