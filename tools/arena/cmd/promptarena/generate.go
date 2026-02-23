package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/generate"
	"github.com/AltairaLabs/PromptKit/tools/arena/generate/sources"
)

// generateRegistry is the global registry for session source adapters.
// External plugins can register adapters via init() functions.
var generateRegistry = generate.NewRegistry()

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate scenario files from session data",
	Long: `Generate Arena scenario YAML files from recorded sessions or external session sources.

Uses pluggable SessionSourceAdapter implementations to load session data
and convert it into reproducible test scenarios.

Examples:
  # Generate from local recording files
  promptarena generate --from-recordings "recordings/*.recording.json"

  # Generate from a named source adapter
  promptarena generate --source omnia --filter-passed=false

  # Generate workflow scenarios with a pack file
  promptarena generate --from-recordings "recordings/*.recording.json" --pack pack.json

  # Output to a specific directory
  promptarena generate --from-recordings "*.recording.json" --output scenarios/`,
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().String("source", "", "Named session source adapter (e.g., omnia)")          // NOSONAR
	generateCmd.Flags().String("from-recordings", "", "Glob path to local recording files")         // NOSONAR
	generateCmd.Flags().String("filter-eval-type", "", "Filter sessions by assertion failure type") // NOSONAR
	generateCmd.Flags().Bool("filter-passed", false, "Filter by pass/fail status")                  // NOSONAR
	generateCmd.Flags().String("pack", "", "Pack file path (generates workflow scenarios)")         // NOSONAR
	generateCmd.Flags().String("output", ".", "Output directory for generated scenario files")      // NOSONAR
	generateCmd.Flags().Bool("dedup", true, "Deduplicate sessions by failure pattern")              // NOSONAR
}

func resolveAdapter(cmd *cobra.Command) (generate.SessionSourceAdapter, error) {
	sourceName, _ := cmd.Flags().GetString("source")
	fromRecordings, _ := cmd.Flags().GetString("from-recordings")

	switch {
	case fromRecordings != "":
		return sources.NewRecordingsAdapter(fromRecordings), nil
	case sourceName != "":
		return generateRegistry.Get(sourceName)
	default:
		return nil, fmt.Errorf("specify either --source or --from-recordings")
	}
}

func buildListOptions(cmd *cobra.Command) (generate.ListOptions, error) {
	opts := generate.ListOptions{}

	if cmd.Flags().Changed("filter-passed") {
		val, err := cmd.Flags().GetBool("filter-passed")
		if err != nil {
			return opts, fmt.Errorf("invalid filter-passed value: %w", err)
		}
		opts.FilterPassed = &val
	}

	opts.FilterEvalType, _ = cmd.Flags().GetString("filter-eval-type")

	return opts, nil
}
