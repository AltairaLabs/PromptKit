package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/app"
)

var configInspectCmd = &cobra.Command{
	Use:   "config-inspect",
	Short: "Inspect and validate prompt arena configuration",
	Long: `Displays loaded configuration including prompt configs, providers, scenarios,
tools, personas, and validates cross-references.

Use --verbose to see detailed contents of each configuration file.
Use --section to focus on specific parts.
Use --short/-s for quick validation-only output.`,
	RunE:          runConfigInspect,
	SilenceUsage:  true, // Don't show usage on error - keeps output clean
	SilenceErrors: false,
}

const (
	// Section names
	sectionValidation = "validation"

	// Output format names for --format flag.
	inspectFormatJSON = "json"
	inspectFormatText = "text"
)

var (
	inspectFormat  string
	inspectVerbose bool
	inspectStats   bool
	inspectSection string
	inspectShort   bool
)

func init() {
	rootCmd.AddCommand(configInspectCmd)

	configInspectCmd.Flags().StringP("config", "c", "config.arena.yaml", "Configuration file path")
	configInspectCmd.Flags().StringVar(&inspectFormat, "format", "text", "Output format: text, json")
	configInspectCmd.Flags().BoolVarP(&inspectVerbose, "verbose", "v", false,
		"Show detailed information including file contents")
	configInspectCmd.Flags().BoolVar(&inspectStats, "stats", false, "Show cache statistics")
	configInspectCmd.Flags().StringVar(&inspectSection, "section", "",
		"Focus on specific section: prompts, providers, scenarios, tools, selfplay, judges, defaults, validation")
	configInspectCmd.Flags().BoolVarP(&inspectShort, "short", "s", false,
		"Show only validation results (shortcut for --section validation)")

	// Register dynamic completions (must be after flags are defined)
	RegisterConfigInspectCompletions()
}

func runConfigInspect(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("config") // NOSONAR: Flag existence is controlled by init(), error impossible

	// Apply --short flag as --section validation
	if inspectShort && inspectSection == "" {
		inspectSection = sectionValidation
	}

	// If config path is a directory, append arena.yaml
	if info, _ := os.Stat(configFile); info != nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	// Load configuration directly
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		if validationErr := emitConfigInspectValidationDiagnostics(configFile); validationErr != nil {
			return validationErr
		}
		return fmt.Errorf("config loading failed: %w", err)
	}

	// Collect inspection data
	inspection := collectInspectionData(cfg, configFile)

	// Validate configuration with config file path for proper relative path resolution
	validator := config.NewConfigValidatorWithPath(cfg, configFile)
	validationErr := validator.Validate()
	inspection.ValidationPassed = (validationErr == nil)
	if validationErr != nil {
		inspection.ValidationError = validationErr.Error()
		inspection.ValidationErrors = validator.GetErrors()
	}

	inspection.ValidationWarnings = len(validator.GetWarnings())
	inspection.ValidationWarningDetails = validator.GetWarnings()

	// Collect structured validation checks
	for _, check := range validator.GetChecks() {
		inspection.ValidationChecks = append(inspection.ValidationChecks, ValidationCheckData{
			Name:    check.Name,
			Passed:  check.Passed,
			Warning: check.Warning,
			Issues:  check.Issues,
		})
	}

	// Add additional connectivity checks
	inspection.ValidationChecks = append(inspection.ValidationChecks, collectConnectivityChecks(inspection)...)

	// JSON goes straight to stdout — no TUI.
	if inspectFormat == inspectFormatJSON {
		return outputJSON(inspection)
	}

	// Interactive text path: launch the hub with InspectPage as the root so
	// the user gets a scrollable, keyboard-navigable view of the config.
	// If the format is unrecognized, fall through to a clear error rather than
	// silently opening the TUI.
	if inspectFormat != inspectFormatText {
		return fmt.Errorf("unsupported format: %s", inspectFormat)
	}

	appCtx := &app.AppContext{
		Config:     cfg,
		ConfigPath: configFile,
		ResultsDir: app.ResultsDirFromConfig(configFile),
		Version:    GetVersion(),
	}
	renderOpts := inspect.RenderOptions{
		Verbose: inspectVerbose,
		Section: inspectSection,
		Stats:   inspectStats,
	}
	return app.Run(appCtx, app.NewInspectPageWithOptions(appCtx, renderOpts))
}

func emitConfigInspectValidationDiagnostics(configFile string) error {
	return runValidationChecks(configFile, "auto", false, false)
}
