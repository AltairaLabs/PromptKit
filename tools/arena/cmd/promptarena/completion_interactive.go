package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func init() {
	rootCmd.AddCommand(completionCmd)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion script for the specified shell.
The completions include dynamic suggestions based on your arena configuration.

To load completions:

Bash:
  $ source <(promptarena completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ promptarena completion bash > /etc/bash_completion.d/promptarena
  # macOS:
  $ promptarena completion bash > $(brew --prefix)/etc/bash_completion.d/promptarena

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  # To load completions for each session, execute once:
  $ promptarena completion zsh > "${fpath[1]}/_promptarena"
  # You will need to start a new shell for this setup to take effect.

Fish:
  $ promptarena completion fish | source
  # To load completions for each session, execute once:
  $ promptarena completion fish > ~/.config/fish/completions/promptarena.fish

PowerShell:
  PS> promptarena completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> promptarena completion powershell > promptarena.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

// RegisterRunCompletions registers dynamic completion functions for the run command flags.
// This must be called after runCmd flags are initialized.
func RegisterRunCompletions() {
	// Dynamic completion for --scenario flag
	_ = runCmd.RegisterFlagCompletionFunc("scenario", completeScenarios)

	// Dynamic completion for --provider flag
	_ = runCmd.RegisterFlagCompletionFunc("provider", completeProviders)

	// Dynamic completion for --region flag
	_ = runCmd.RegisterFlagCompletionFunc("region", completeRegions)

	// Dynamic completion for --roles flag
	_ = runCmd.RegisterFlagCompletionFunc("roles", completeRoles)

	// Dynamic completion for --config flag (yaml files)
	_ = runCmd.RegisterFlagCompletionFunc("config", completeConfigFiles)

	// Static completion for --format flag
	_ = runCmd.RegisterFlagCompletionFunc("format", completeFormats)
	_ = runCmd.RegisterFlagCompletionFunc("formats", completeFormats)
}

// loadConfigForCompletion attempts to load the arena config for completion suggestions
func loadConfigForCompletion(cmd *cobra.Command) *config.Config {
	// Try to get config path from flag, or use default
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = "config.arena.yaml"
	}

	// Check if file exists in current directory
	if _, err := os.Stat(configPath); err != nil {
		// Try looking in parent directories
		cwd, err := os.Getwd()
		if err != nil {
			return nil
		}
		found := false
		for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, configPath)
			if _, err := os.Stat(candidate); err == nil {
				configPath = candidate
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	// Load the config silently for completion
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil
	}
	return cfg
}

// completeScenarios provides dynamic completion for scenario names
func completeScenarios(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletion(cmd)
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var scenarios []string
	for name := range cfg.LoadedScenarios {
		if toComplete == "" || strings.HasPrefix(name, toComplete) {
			scenarios = append(scenarios, name)
		}
	}
	return scenarios, cobra.ShellCompDirectiveNoFileComp
}

// completeProviders provides dynamic completion for provider names
func completeProviders(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletion(cmd)
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var providers []string
	for name := range cfg.LoadedProviders {
		if toComplete == "" || strings.HasPrefix(name, toComplete) {
			providers = append(providers, name)
		}
	}
	return providers, cobra.ShellCompDirectiveNoFileComp
}

// completeRegions provides dynamic completion for region names
func completeRegions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Regions are typically user-defined in scenario context variables
	// Provide common defaults
	commonRegions := []string{"us", "uk", "au", "eu", "asia", "latam"}

	var regions []string
	for _, region := range commonRegions {
		if toComplete == "" || strings.HasPrefix(region, toComplete) {
			regions = append(regions, region)
		}
	}
	return regions, cobra.ShellCompDirectiveNoFileComp
}

// completeRoles provides dynamic completion for self-play role names
func completeRoles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletion(cmd)
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var roles []string
	if cfg.SelfPlay != nil {
		for _, role := range cfg.SelfPlay.Roles {
			if toComplete == "" || strings.HasPrefix(role.ID, toComplete) {
				roles = append(roles, role.ID)
			}
		}
	}
	return roles, cobra.ShellCompDirectiveNoFileComp
}

// completeConfigFiles provides completion for config file paths
func completeConfigFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Return yaml files as suggestions
	return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
}

// RegisterConfigInspectCompletions registers dynamic completion functions for the config-inspect command.
// This must be called after configInspectCmd flags are initialized.
func RegisterConfigInspectCompletions() {
	// Dynamic completion for --config flag (yaml files)
	_ = configInspectCmd.RegisterFlagCompletionFunc("config", completeConfigFiles)

	// Static completion for --format flag
	_ = configInspectCmd.RegisterFlagCompletionFunc("format", completeInspectFormat)

	// Static completion for --section flag
	_ = configInspectCmd.RegisterFlagCompletionFunc("section", completeSections)
}

// RegisterInitCompletions registers dynamic completion functions for the init command.
// This must be called after initCmd flags are initialized.
func RegisterInitCompletions() {
	// Dynamic completion for --template flag that reads from built-in and community templates
	_ = initCmd.RegisterFlagCompletionFunc("template", completeTemplates)

	// Static completion for --provider flag
	_ = initCmd.RegisterFlagCompletionFunc("provider", completeInitProviders)
}

// completeTemplates provides dynamic completion for init template names
// It reads from built-in templates and community repo templates
func completeTemplates(
	cmd *cobra.Command,
	_ []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	var matches []string

	// Load built-in templates
	matches = append(matches, collectBuiltinTemplates(toComplete)...)

	// Load community/remote templates from repos config
	matches = append(matches, collectRepoTemplates(cmd, toComplete)...)

	return matches, cobra.ShellCompDirectiveNoFileComp
}

// collectBuiltinTemplates returns matching built-in template names
func collectBuiltinTemplates(toComplete string) []string {
	var matches []string
	loader := templates.NewLoader("")
	builtinList, err := loader.ListBuiltIn()
	if err != nil {
		return matches
	}
	for _, t := range builtinList {
		if toComplete == "" || strings.HasPrefix(t.Name, toComplete) {
			matches = append(matches, formatTemplateCompletion(t.Name, t.Description))
		}
	}
	return matches
}

// collectRepoTemplates returns matching templates from configured repos
func collectRepoTemplates(cmd *cobra.Command, toComplete string) []string {
	var matches []string
	repoConfigPath, _ := cmd.Flags().GetString("repo-config")
	if repoConfigPath == "" {
		repoConfigPath = templates.DefaultRepoConfigPath()
	}

	repoCfg, err := templates.LoadRepoConfig(repoConfigPath)
	if err != nil {
		return matches
	}

	for repoName, repo := range repoCfg.Repos {
		matches = append(matches, collectRepoIndexTemplates(repoName, repo.URL, toComplete)...)
	}
	return matches
}

// collectRepoIndexTemplates returns matching templates from a single repo index
func collectRepoIndexTemplates(repoName, repoURL, toComplete string) []string {
	var matches []string
	index, err := templates.LoadIndex(repoURL)
	if err != nil {
		return matches
	}
	for _, entry := range index.Spec.Entries {
		fullName := fmt.Sprintf("%s/%s", repoName, entry.Name)
		if matchesTemplatePrefix(fullName, entry.Name, toComplete) {
			matches = append(matches, formatTemplateCompletion(fullName, entry.Description))
		}
	}
	return matches
}

// matchesTemplatePrefix checks if template name matches the completion prefix
func matchesTemplatePrefix(fullName, shortName, toComplete string) bool {
	return toComplete == "" ||
		strings.HasPrefix(fullName, toComplete) ||
		strings.HasPrefix(shortName, toComplete)
}

// formatTemplateCompletion formats a template name with optional description for shell completion
func formatTemplateCompletion(name, description string) string {
	if description != "" {
		return fmt.Sprintf("%s\t%s", name, description)
	}
	return name
}

// completeFormats provides completion for output format options
func completeFormats(
	_ *cobra.Command,
	_ []string,
	_ string,
) ([]string, cobra.ShellCompDirective) {
	return []string{"json", "junit", "html", "markdown"}, cobra.ShellCompDirectiveNoFileComp
}

// completeInspectFormat provides completion for config-inspect format options
func completeInspectFormat(
	_ *cobra.Command,
	_ []string,
	_ string,
) ([]string, cobra.ShellCompDirective) {
	return []string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp
}

// completeSections provides completion for config section names
func completeSections(
	_ *cobra.Command,
	_ []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	sections := []string{
		"prompts", "providers", "scenarios", "tools",
		"selfplay", "judges", "defaults", "validation",
	}
	var matches []string
	for _, s := range sections {
		if toComplete == "" || strings.HasPrefix(s, toComplete) {
			matches = append(matches, s)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

// completeInitProviders provides completion for init provider options
func completeInitProviders(
	_ *cobra.Command,
	_ []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	providers := []string{"openai", "anthropic", "google", "mock"}
	var matches []string
	for _, p := range providers {
		if toComplete == "" || strings.HasPrefix(p, toComplete) {
			matches = append(matches, p)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}
