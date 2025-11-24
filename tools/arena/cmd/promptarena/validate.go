package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate configuration files against JSON schemas",
	Long: `Validates Arena configs, scenarios, providers, and other YAML files against their JSON schemas.
	
Automatically detects file type based on the 'kind' field in the YAML file.
Can also explicitly specify the type with --type flag.

Examples:
  promptarena validate arena.yaml
  promptarena validate scenarios/test.yaml --type scenario
  promptarena validate providers/*.yaml
  promptarena validate arena.yaml --schema-only`,
	RunE: runValidate,
}

var (
	validateType       string
	validateVerbose    bool
	validateSchemaOnly bool
)

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().StringVar(&validateType, "type", "auto", "Config type: auto, arena, scenario, provider, promptconfig, tool, persona")
	validateCmd.Flags().BoolVar(&validateVerbose, "verbose", false, "Show detailed validation errors")
	validateCmd.Flags().BoolVar(&validateSchemaOnly, "schema-only", false, "Only validate schema, skip business logic checks")
}

func runValidate(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("file path required")
	}

	filePath := args[0]
	data, configType, err := prepareValidation(filePath)
	if err != nil {
		return err
	}

	// Schema validation
	if err := performSchemaValidation(data, configType, filePath); err != nil {
		return err
	}

	// Business logic validation (if requested)
	if !validateSchemaOnly && configType == "arena" {
		if err := performBusinessLogicValidation(filePath); err != nil {
			return err
		}
	}

	fmt.Printf("\n✅ %s is valid\n", filepath.Base(filePath))
	return nil
}

func prepareValidation(filePath string) ([]byte, string, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("file not found: %s", filePath)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file: %w", err)
	}

	// Determine config type
	configType := validateType
	if configType == "auto" {
		detectedType, err := config.DetectConfigType(data)
		if err != nil {
			return nil, "", fmt.Errorf("could not auto-detect config type: %w\nUse --type to specify explicitly", err)
		}
		configType = string(detectedType)
	}

	return data, configType, nil
}

func performSchemaValidation(data []byte, configType string, filePath string) error {
	fmt.Printf("Validating %s as type '%s'...\n", filepath.Base(filePath), configType)

	result, err := validateWithSchema(data, config.ConfigType(configType))
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	if !result.Valid {
		fmt.Printf("❌ Schema validation failed for %s:\n", filePath)
		displayErrors(result.Errors, validateVerbose)
		return fmt.Errorf("schema validation failed with %d error(s)", len(result.Errors))
	}

	fmt.Printf("✅ Schema validation passed for %s\n", filePath)
	return nil
}

func performBusinessLogicValidation(filePath string) error {
	fmt.Println("\nRunning business logic validation...")
	cfg, err := config.LoadConfig(filePath)
	if err != nil {
		return fmt.Errorf("config loading failed: %w", err)
	}

	validator := config.NewConfigValidatorWithPath(cfg, filePath)
	if err := validator.Validate(); err != nil {
		fmt.Printf("❌ Business logic validation failed:\n")
		fmt.Printf("  %s\n", err.Error())
		return err
	}

	warnings := validator.GetWarnings()
	if len(warnings) > 0 {
		fmt.Printf("\n⚠️  Validation warnings (%d):\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	} else {
		fmt.Println("✅ Business logic validation passed")
	}

	return nil
}

func validateWithSchema(data []byte, configType config.ConfigType) (*config.SchemaValidationResult, error) {
	return config.ValidateWithSchema(data, configType)
}

func displayErrors(errors []config.SchemaValidationError, verbose bool) {
	maxErrors := 5
	if verbose {
		maxErrors = len(errors)
	}

	displayed := 0
	for i, e := range errors {
		if i >= maxErrors {
			break
		}
		displayError(e)
		displayed++
	}

	if !verbose && len(errors) > maxErrors {
		remaining := len(errors) - displayed
		fmt.Printf("\n  ... and %d more error(s) (use --verbose to see all)\n", remaining)
	}
}

func displayError(err config.SchemaValidationError) {
	// Clean up the field path for better readability
	field := err.Field
	field = strings.TrimPrefix(field, "(root).")
	if field == "(root)" {
		field = "root"
	}

	// Format the error message
	if err.Value != nil {
		fmt.Printf("  - %s: %s (value: %v)\n", field, err.Description, err.Value)
	} else {
		fmt.Printf("  - %s: %s\n", field, err.Description)
	}
}
