package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/schema-gen/generators"
)

const (
	dirPermissions  = 0750
	filePermissions = 0600
)

var (
	// defaultOutputDir uses the schema version from config
	defaultOutputDir = "schemas/" + config.SchemaVersion
)

var (
	checkMode  = flag.Bool("check", false, "Check if schemas are up-to-date (for CI)")
	outputDir  = flag.String("output-dir", defaultOutputDir, "Output directory for generated schemas")
	verbose    = flag.Bool("verbose", false, "Enable verbose output")
	formatOnly = flag.Bool("format", false, "Only format existing schemas without regenerating")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return fmt.Errorf("failed to find repository root: %w", err)
	}

	outputPath := filepath.Join(repoRoot, *outputDir)

	if *formatOnly {
		return formatExistingSchemas(outputPath)
	}

	schemaGens := []struct {
		name      string
		generator func() (interface{}, error)
		filename  string
	}{
		{"Arena", generators.GenerateArenaSchema, "arena.json"},
		{"Scenario", generators.GenerateScenarioSchema, "scenario.json"},
		{"Eval", generators.GenerateEvalSchema, "eval.json"},
		{"Provider", generators.GenerateProviderSchema, "provider.json"},
		{"PromptConfig", generators.GeneratePromptConfigSchema, "promptconfig.json"},
		{"Tool", generators.GenerateToolSchema, "tool.json"},
		{"Persona", generators.GeneratePersonaSchema, "persona.json"},
		{"Logging", generators.GenerateLoggingSchema, "logging.json"},
	}

	if mkdirErr := os.MkdirAll(outputPath, dirPermissions); mkdirErr != nil {
		return fmt.Errorf("failed to create output directory: %w", mkdirErr)
	}

	if genErr := generateCommonSchemas(outputPath); genErr != nil {
		return fmt.Errorf("failed to generate common schemas: %w", genErr)
	}

	hasChanges, err := generateSchemas(outputPath, schemaGens)
	if err != nil {
		return err
	}

	// Generate latest schema references
	if !*checkMode {
		if err := generateLatestSchemas(repoRoot, schemaGens); err != nil {
			return fmt.Errorf("failed to generate latest schemas: %w", err)
		}
	}

	if *checkMode {
		if hasChanges {
			return fmt.Errorf("schemas are out of date - run 'make schemas' to update")
		}
		fmt.Println("✓ All schemas are up to date")
	} else {
		fmt.Printf("✓ Generated %d schemas in %s\n", len(schemaGens), outputPath)
		fmt.Printf("✓ Generated latest schema references\n")
	}

	return nil
}

func generateSchemas(outputPath string, schemaGens []struct {
	name      string
	generator func() (interface{}, error)
	filename  string
}) (bool, error) {
	var hasChanges bool

	for _, sg := range schemaGens {
		changed, err := generateSingleSchema(outputPath, sg.name, sg.generator, sg.filename)
		if err != nil {
			return false, err
		}
		if changed {
			hasChanges = true
		}
	}

	return hasChanges, nil
}

func generateSingleSchema(
	outputPath, name string,
	generator func() (interface{}, error),
	filename string,
) (bool, error) {
	if *verbose {
		fmt.Printf("Generating %s schema...\n", name)
	}

	schema, err := generator()
	if err != nil {
		return false, fmt.Errorf("failed to generate %s schema: %w", name, err)
	}

	data, err := marshalSchema(schema, name)
	if err != nil {
		return false, err
	}

	outputFile := filepath.Join(outputPath, filename)

	if *checkMode {
		return checkSchemaUpToDate(outputFile, data, filename)
	}

	return false, writeSchemaFile(outputFile, data, filename)
}

func marshalSchema(schema interface{}, name string) ([]byte, error) {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s schema: %w", name, err)
	}
	return append(data, '\n'), nil
}

func checkSchemaUpToDate(outputFile string, data []byte, filename string) (bool, error) {
	existing, err := os.ReadFile(outputFile) // #nosec G304 -- outputFile is constructed from safe inputs
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Schema missing: %s\n", filename)
			return true, nil
		}
		return false, fmt.Errorf("failed to read existing schema %s: %w", filename, err)
	}

	if !bytes.Equal(existing, data) {
		fmt.Printf("Schema out of date: %s\n", filename)
		return true, nil
	}

	return false, nil
}

func writeSchemaFile(outputFile string, data []byte, filename string) error {
	if err := os.WriteFile(outputFile, data, filePermissions); err != nil {
		return fmt.Errorf("failed to write schema %s: %w", filename, err)
	}

	if *verbose {
		fmt.Printf("✓ Generated %s\n", outputFile)
	}

	return nil
}

func generateCommonSchemas(outputPath string) error {
	commonDir := filepath.Join(outputPath, "common")
	if err := os.MkdirAll(commonDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create common directory: %w", err)
	}

	commonSchemas := []struct {
		name      string
		generator func() (interface{}, error)
		filename  string
	}{
		{"Metadata", generators.GenerateMetadataSchema, "metadata.json"},
		{"Assertions", generators.GenerateAssertionsSchema, "assertions.json"},
		{"Media", generators.GenerateMediaSchema, "media.json"},
	}

	for _, cs := range commonSchemas {
		if *verbose {
			fmt.Printf("Generating common/%s schema...\n", cs.name)
		}

		schema, err := cs.generator()
		if err != nil {
			return fmt.Errorf("failed to generate %s schema: %w", cs.name, err)
		}

		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s schema: %w", cs.name, err)
		}

		data = append(data, '\n')
		outputFile := filepath.Join(commonDir, cs.filename)

		if err := os.WriteFile(outputFile, data, filePermissions); err != nil {
			return fmt.Errorf("failed to write schema %s: %w", cs.filename, err)
		}

		if *verbose {
			fmt.Printf("✓ Generated %s\n", outputFile)
		}
	}

	return nil
}

func formatExistingSchemas(outputPath string) error {
	pattern := filepath.Join(outputPath, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob schemas: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 -- file is from Glob, safe to read
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		var schema interface{}
		if unmarshalErr := json.Unmarshal(data, &schema); unmarshalErr != nil {
			return fmt.Errorf("failed to parse %s: %w", file, unmarshalErr)
		}

		formatted, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format %s: %w", file, err)
		}

		formatted = append(formatted, '\n')

		if err := os.WriteFile(file, formatted, filePermissions); err != nil {
			return fmt.Errorf("failed to write %s: %w", file, err)
		}

		fmt.Printf("✓ Formatted %s\n", file)
	}

	return nil
}

func generateLatestSchemas(repoRoot string, schemaGens []struct {
	name      string
	generator func() (interface{}, error)
	filename  string
}) error {
	latestDir := filepath.Join(repoRoot, "docs", "public", "schemas", "latest")
	if err := os.MkdirAll(latestDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create latest directory: %w", err)
	}

	baseURL := "https://promptkit.altairalabs.ai/schemas"

	// Generate main schema refs
	for _, sg := range schemaGens {
		if err := generateSchemaRef(latestDir, baseURL, sg.filename, ""); err != nil {
			return err
		}
	}

	// Generate common schema refs
	commonLatestDir := filepath.Join(latestDir, "common")
	if err := os.MkdirAll(commonLatestDir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create latest/common directory: %w", err)
	}

	commonSchemas := []string{"metadata.json", "assertions.json", "media.json"}
	for _, filename := range commonSchemas {
		if err := generateSchemaRef(commonLatestDir, baseURL, filename, "common/"); err != nil {
			return err
		}
	}

	return nil
}

// generateSchemaRef creates a JSON reference file pointing to a versioned schema
func generateSchemaRef(outputDir, baseURL, filename, pathPrefix string) error {
	ref := map[string]string{
		"$ref": fmt.Sprintf("%s/%s/%s%s", baseURL, config.SchemaVersion, pathPrefix, filename),
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal latest ref for %s%s: %w", pathPrefix, filename, err)
	}
	data = append(data, '\n')

	outputFile := filepath.Join(outputDir, filename)
	if err := os.WriteFile(outputFile, data, filePermissions); err != nil {
		return fmt.Errorf("failed to write latest ref %s%s: %w", pathPrefix, filename, err)
	}

	if *verbose {
		fmt.Printf("✓ Generated latest/%s%s -> %s/%s%s\n", pathPrefix, filename, config.SchemaVersion, pathPrefix, filename)
	}

	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if info, _ := os.Stat(filepath.Join(dir, "go.work")); info != nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found (no go.work file)")
		}
		dir = parent
	}
}
