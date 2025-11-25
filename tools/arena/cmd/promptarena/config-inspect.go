package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

var configInspectCmd = &cobra.Command{
	Use:   "config-inspect",
	Short: "Inspect and validate prompt arena configuration",
	Long:  `Displays loaded configuration including prompt configs, providers, scenarios, and validates cross-references.`,
	RunE:  runConfigInspect,
}

const (
	indentedItemFormat = "  - %s\n"
)

var (
	inspectFormat  string
	inspectVerbose bool
	inspectStats   bool
)

func init() {
	rootCmd.AddCommand(configInspectCmd)

	configInspectCmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")
	configInspectCmd.Flags().StringVar(&inspectFormat, "format", "text", "Output format: text, json")
	configInspectCmd.Flags().BoolVar(&inspectVerbose, "verbose", false, "Show detailed information")
	configInspectCmd.Flags().BoolVar(&inspectStats, "stats", false, "Show cache statistics")
}

func runConfigInspect(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("config") // NOSONAR: Flag existence is controlled by init(), error impossible

	// If config path is a directory, append arena.yaml
	if info, _ := os.Stat(configFile); info != nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	// Load configuration directly
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Collect inspection data
	inspection := collectInspectionData(cfg)

	// Validate configuration with config file path for proper relative path resolution
	validator := config.NewConfigValidatorWithPath(cfg, configFile)
	validationErr := validator.Validate()
	inspection.ValidationPassed = (validationErr == nil)
	if validationErr != nil {
		inspection.ValidationError = validationErr.Error()
	}
	inspection.ValidationWarnings = len(validator.GetWarnings())
	if inspectVerbose {
		inspection.ValidationWarningDetails = validator.GetWarnings()
	}

	// Output results
	switch inspectFormat {
	case "json":
		return outputJSON(inspection)
	case "text":
		return outputText(inspection)
	default:
		return fmt.Errorf("unsupported format: %s", inspectFormat)
	}
}

// InspectionData contains all collected configuration inspection data
type InspectionData struct {
	PromptConfigs            []string        `json:"prompt_configs"`
	Providers                []string        `json:"providers"`
	Scenarios                []string        `json:"scenarios"`
	SelfPlayRoles            []string        `json:"self_play_roles,omitempty"`
	AvailableTaskTypes       []string        `json:"available_task_types"`
	AvailableRegions         []string        `json:"available_regions"`
	ValidationPassed         bool            `json:"validation_passed"`
	ValidationError          string          `json:"validation_error,omitempty"`
	ValidationWarnings       int             `json:"validation_warnings"`
	ValidationWarningDetails []string        `json:"validation_warning_details,omitempty"`
	CacheStats               *CacheStatsData `json:"cache_stats,omitempty"`
}

// CacheStatsData contains cache statistics
type CacheStatsData struct {
	PromptCache   CacheInfo `json:"prompt_cache"`
	FragmentCache CacheInfo `json:"fragment_cache"`
	SelfPlayCache CacheInfo `json:"self_play_cache,omitempty"`
}

// CacheInfo contains information about a specific cache
type CacheInfo struct {
	Size    int      `json:"size"`
	Entries []string `json:"entries,omitempty"`
	Hits    int      `json:"hits,omitempty"`
	Misses  int      `json:"misses,omitempty"`
	HitRate float64  `json:"hit_rate,omitempty"`
}

func collectInspectionData(cfg *config.Config) *InspectionData {
	data := &InspectionData{
		AvailableTaskTypes: getAvailableTaskTypes(cfg),
		AvailableRegions:   []string{}, // Regions are not stored in config, would need to parse prompt files
	}

	// Collect prompt configs
	for _, pc := range cfg.PromptConfigs {
		data.PromptConfigs = append(data.PromptConfigs, pc.File)
	}

	// Collect providers
	for _, p := range cfg.Providers {
		data.Providers = append(data.Providers, p.File)
	}

	// Collect scenarios
	for _, s := range cfg.Scenarios {
		data.Scenarios = append(data.Scenarios, s.File)
	}

	// Collect self-play roles
	if cfg.SelfPlay != nil {
		for _, role := range cfg.SelfPlay.Roles {
			data.SelfPlayRoles = append(data.SelfPlayRoles, role.ID)
		}
	}

	// Collect cache stats if requested
	if inspectStats {
		data.CacheStats = collectCacheStats(cfg)
	}

	return data
}

// getAvailableTaskTypes extracts task types from loaded prompt configs
func getAvailableTaskTypes(cfg *config.Config) []string {
	taskTypes := make([]string, 0, len(cfg.LoadedPromptConfigs))
	for taskType := range cfg.LoadedPromptConfigs {
		taskTypes = append(taskTypes, taskType)
	}
	return taskTypes
}

func collectCacheStats(cfg *config.Config) *CacheStatsData {
	// Collect basic cache information from loaded config
	loadedPrompts := make([]string, 0, len(cfg.LoadedPromptConfigs))
	for taskType := range cfg.LoadedPromptConfigs {
		loadedPrompts = append(loadedPrompts, taskType)
	}

	stats := &CacheStatsData{
		PromptCache: CacheInfo{
			Size:    len(cfg.LoadedPromptConfigs),
			Entries: loadedPrompts,
		},
		FragmentCache: CacheInfo{
			Size: 0, // Fragment cache is internal to prompt registry, not directly accessible
		},
	}

	// Collect self-play cache stats if available
	if cfg.SelfPlay != nil && len(cfg.LoadedPersonas) > 0 {
		cachedPairs := make([]string, 0)
		for personaID := range cfg.LoadedPersonas {
			if cfg.SelfPlay.Roles != nil {
				for _, role := range cfg.SelfPlay.Roles {
					cachedPairs = append(cachedPairs, fmt.Sprintf("%s:%s", role.ID, personaID))
				}
			}
		}

		stats.SelfPlayCache = CacheInfo{
			Size: len(cachedPairs),
		}

		if inspectVerbose {
			stats.SelfPlayCache.Entries = cachedPairs
		}
	}

	return stats
}

func outputJSON(data *InspectionData) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func outputText(data *InspectionData) error {
	printConfigurationSummary(data)
	printAvailableOptions(data)
	printValidationResults(data)

	if inspectStats && data.CacheStats != nil {
		printCacheStatistics(data.CacheStats)
	}

	return nil
}

// printConfigurationSummary prints the configuration summary section
func printConfigurationSummary(data *InspectionData) {
	fmt.Println("=== Prompt Arena Configuration ===")
	fmt.Println()

	printConfigSection("Prompt Configs", len(data.PromptConfigs), data.PromptConfigs)
	printConfigSection("Providers", len(data.Providers), data.Providers)
	printConfigSection("Scenarios", len(data.Scenarios), data.Scenarios)

	if len(data.SelfPlayRoles) > 0 {
		printConfigSection("Self-Play Roles", len(data.SelfPlayRoles), data.SelfPlayRoles)
	}
}

// printConfigSection prints a configuration section with optional verbose details
func printConfigSection(title string, count int, items []string) {
	fmt.Printf("%s: %d\n", title, count)
	if inspectVerbose {
		for _, item := range items {
			fmt.Printf(indentedItemFormat, item)
		}
	}
	fmt.Println()
}

// printAvailableOptions prints the available options section
func printAvailableOptions(data *InspectionData) {
	fmt.Println("=== Available Options ===")
	fmt.Printf("Task Types: %v\n", data.AvailableTaskTypes)
	fmt.Printf("Regions: %v\n", data.AvailableRegions)
	fmt.Println()
}

// printValidationResults prints the validation results section
func printValidationResults(data *InspectionData) {
	fmt.Println("=== Configuration Validation ===")

	printValidationStatus(data)
	printValidationWarnings(data)

	fmt.Println()
}

// printValidationStatus prints the main validation status
func printValidationStatus(data *InspectionData) {
	if data.ValidationPassed {
		fmt.Println("✓ Configuration is valid")
	} else {
		fmt.Println("✗ Configuration has errors")
		if inspectVerbose && data.ValidationError != "" {
			fmt.Printf("  %s\n", data.ValidationError)
		}
	}
}

// printValidationWarnings prints validation warnings if any
func printValidationWarnings(data *InspectionData) {
	if data.ValidationWarnings > 0 {
		fmt.Printf("⚠ Configuration has %d warning(s)\n", data.ValidationWarnings)
	}

	if inspectVerbose && len(data.ValidationWarningDetails) > 0 {
		fmt.Println("\nValidation Warnings:")
		for _, warn := range data.ValidationWarningDetails {
			fmt.Printf(indentedItemFormat, warn)
		}
	}
}

// printCacheStatistics prints the cache statistics section
func printCacheStatistics(cacheStats *CacheStatsData) {
	fmt.Println("=== Cache Statistics ===")

	printCacheInfo("Prompt Cache", &cacheStats.PromptCache)
	printCacheInfo("Fragment Cache", &cacheStats.FragmentCache)

	if cacheStats.SelfPlayCache.Size > 0 {
		printSelfPlayCacheInfo(&cacheStats.SelfPlayCache)
	}

	fmt.Println()
}

// printCacheInfo prints information about a specific cache
func printCacheInfo(cacheName string, cache *CacheInfo) {
	fmt.Printf("%s: %d entries\n", cacheName, cache.Size)
	if inspectVerbose && len(cache.Entries) > 0 {
		for _, entry := range cache.Entries {
			fmt.Printf(indentedItemFormat, entry)
		}
	}
}

// printSelfPlayCacheInfo prints self-play cache information with hit rate statistics
func printSelfPlayCacheInfo(cache *CacheInfo) {
	fmt.Printf("Self-Play Cache: %d entries\n", cache.Size)
	fmt.Printf("  Hits: %d, Misses: %d, Hit Rate: %.2f%%\n",
		cache.Hits, cache.Misses, cache.HitRate*100)

	if inspectVerbose && len(cache.Entries) > 0 {
		for _, entry := range cache.Entries {
			fmt.Printf(indentedItemFormat, entry)
		}
	}
}
