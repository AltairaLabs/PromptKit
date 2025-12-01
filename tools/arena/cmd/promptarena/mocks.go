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

func loadRunResults(inputPath string, scenarioFilter, providerFilter []string) ([]engine.RunResult, error) {
	files, err := collectJSONFiles(inputPath)
	if err != nil {
		return nil, err
	}

	scenarioAllow := toSet(scenarioFilter)
	providerAllow := toSet(providerFilter)

	results := make([]engine.RunResult, 0, len(files))
	for _, path := range files {
		res, ok, err := parseRun(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if !matchesFilters(&res, scenarioAllow, providerAllow) {
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

func collectJSONFiles(inputPath string) ([]string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}

	if !info.IsDir() {
		return []string{inputPath}, nil
	}

	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(inputPath, e.Name()))
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no JSON result files found at %s", inputPath)
	}

	return files, nil
}

func parseRun(path string) (engine.RunResult, bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading known result files
	if err != nil {
		return engine.RunResult{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var res engine.RunResult
	if err := json.Unmarshal(data, &res); err != nil {
		return engine.RunResult{}, false, nil
	}
	if res.RunID == "" || res.ScenarioID == "" || res.ProviderID == "" {
		return engine.RunResult{}, false, nil
	}
	return res, true, nil
}

func matchesFilters(res *engine.RunResult, scenarioAllow, providerAllow map[string]bool) bool {
	if len(scenarioAllow) > 0 && !scenarioAllow[res.ScenarioID] {
		return false
	}
	if len(providerAllow) > 0 && !providerAllow[res.ProviderID] {
		return false
	}
	return true
}
