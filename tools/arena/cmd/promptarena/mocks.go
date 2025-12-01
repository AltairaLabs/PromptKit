package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/mocks"
)

var (
	mocksInput           string
	mocksOutput          string
	mocksPerScenario     bool
	mocksMerge           bool
	mocksScenarioFilters []string
	mocksProviderFilters []string
	mocksDryRun          bool
	mocksDefaultResponse string
)

var mocksCmd = &cobra.Command{
	Use:   "mocks",
	Short: "Manage mock responses for PromptArena",
}

var mocksGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate mock provider responses from Arena JSON results",
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := loadRunResults(mocksInput, mocksScenarioFilters, mocksProviderFilters)
		if err != nil {
			return err
		}

		file, err := mocks.BuildFile(results)
		if err != nil {
			return fmt.Errorf("build mock file: %w", err)
		}

		opts := mocks.WriteOptions{
			OutputPath:      mocksOutput,
			PerScenario:     mocksPerScenario,
			Merge:           mocksMerge,
			DefaultResponse: mocksDefaultResponse,
			DryRun:          mocksDryRun,
		}

		outputs, err := mocks.WriteFiles(file, opts)
		if err != nil {
			return fmt.Errorf("write mocks: %w", err)
		}

		for path, content := range outputs {
			if mocksDryRun {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "--- %s (dry-run)\n%s\n", path, string(content)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Generated %s\n", path); err != nil {
				return err
			}
		}

		return nil
	},
}

func init() { //nolint:gochecknoinits
	rootCmd.AddCommand(mocksCmd)
	mocksCmd.AddCommand(mocksGenerateCmd)

	mocksGenerateCmd.Flags().StringVarP(&mocksInput, "input", "i", "out", "Path to Arena JSON result file or directory")
	mocksGenerateCmd.Flags().StringVarP(
		&mocksOutput,
		"output",
		"o",
		"providers/mock-generated.yaml",
		"Output file or directory (if --per-scenario)",
	)
	mocksGenerateCmd.Flags().BoolVar(
		&mocksPerScenario,
		"per-scenario",
		false,
		"Write one file per scenario (requires --output directory)",
	)
	mocksGenerateCmd.Flags().BoolVar(
		&mocksMerge,
		"merge",
		false,
		"Merge with existing mock file(s) instead of overwriting",
	)
	mocksGenerateCmd.Flags().StringSliceVar(
		&mocksScenarioFilters,
		"scenario",
		[]string{},
		"Only include specified scenario IDs (repeatable)",
	)
	mocksGenerateCmd.Flags().StringSliceVar(
		&mocksProviderFilters,
		"provider",
		[]string{},
		"Only include specified provider IDs (repeatable)",
	)
	mocksGenerateCmd.Flags().BoolVar(&mocksDryRun, "dry-run", false, "Print generated YAML instead of writing files")
	mocksGenerateCmd.Flags().StringVar(
		&mocksDefaultResponse,
		"default-response",
		"",
		"Set defaultResponse when not present in generated output",
	)
}

//nolint:gocognit
func loadRunResults(inputPath string, scenarioFilter, providerFilter []string) ([]engine.RunResult, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(inputPath)
		if err != nil {
			return nil, fmt.Errorf("read dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".json") {
				files = append(files, filepath.Join(inputPath, e.Name()))
			}
		}
	} else {
		files = append(files, inputPath)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no JSON result files found at %s", inputPath)
	}

	scenarioAllow := toSet(scenarioFilter)
	providerAllow := toSet(providerFilter)

	var results []engine.RunResult
	for _, path := range files {
		data, err := os.ReadFile(path) //nolint:gosec // reading known result files
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var res engine.RunResult
		if err := json.Unmarshal(data, &res); err != nil {
			// Skip files that are not run results (e.g., index.json)
			continue
		}

		if res.RunID == "" || res.ScenarioID == "" || res.ProviderID == "" {
			// Likely not a run file (skip silently)
			continue
		}

		if len(scenarioAllow) > 0 && !scenarioAllow[res.ScenarioID] {
			continue
		}
		if len(providerAllow) > 0 && !providerAllow[res.ProviderID] {
			continue
		}

		results = append(results, res)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no run results matched filters")
	}

	return results, nil
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}
