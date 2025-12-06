package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

var configInspectCmd = &cobra.Command{
	Use:   "config-inspect",
	Short: "Inspect and validate prompt arena configuration",
	Long: `Displays loaded configuration including prompt configs, providers, scenarios, 
tools, personas, and validates cross-references.

Use --verbose to see detailed contents of each configuration file.
Use --section to focus on specific parts (prompts, providers, scenarios, tools, personas).`,
	RunE: runConfigInspect,
}

const (
	indentedItemFormat  = "  - %s\n"
	boxWidth            = 78
	maxDescLength       = 55
	maxDescLengthPrompt = 60
	maxGoalsDisplayed   = 3
	maxGoalLength       = 50
	percentMultiplier   = 100

	// Label constants for display output
	labelFile        = "  File: "
	labelDesc        = "  Desc: "
	labelModel       = "  Model: "
	labelTemperature = "  Temperature: "
	labelMaxTok      = "  Max Tokens: "
	labelTaskType    = "  Task Type: "
	labelTask        = "  Task: "
	labelVersion     = "  Version: "
	labelVariables   = "  Variables: "
	labelTools       = "  Tools: "
	labelValidators  = "  Validators: "
	labelMedia       = "  Media: "
	labelMode        = "  Mode: "
	labelParams      = "  Params: "
	labelTimeout     = "  Timeout: "
	labelFeatures    = "  Features: "
	labelFlags       = "  Flags: "
	labelProviders   = "  Providers: "
	labelGoals       = "  Goals:"

	// Format strings
	entriesFormat = "%d entries"
)

var (
	inspectFormat  string
	inspectVerbose bool
	inspectStats   bool
	inspectSection string
)

func init() {
	rootCmd.AddCommand(configInspectCmd)

	configInspectCmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")
	configInspectCmd.Flags().StringVar(&inspectFormat, "format", "text", "Output format: text, json")
	configInspectCmd.Flags().BoolVar(&inspectVerbose, "verbose", false,
		"Show detailed information including file contents")
	configInspectCmd.Flags().BoolVar(&inspectStats, "stats", false, "Show cache statistics")
	configInspectCmd.Flags().StringVar(&inspectSection, "section", "",
		"Focus on specific section: prompts, providers, scenarios, tools, personas, judges")
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

	// Output results
	switch inspectFormat {
	case "json":
		return outputJSON(inspection)
	case "text":
		return outputText(inspection, cfg)
	default:
		return fmt.Errorf("unsupported format: %s", inspectFormat)
	}
}

// InspectionData contains all collected configuration inspection data
type InspectionData struct {
	ConfigFile               string                `json:"config_file"`
	ArenaName                string                `json:"arena_name,omitempty"`
	PromptConfigs            []PromptInspectData   `json:"prompt_configs"`
	Providers                []ProviderInspectData `json:"providers"`
	Scenarios                []ScenarioInspectData `json:"scenarios"`
	Tools                    []ToolInspectData     `json:"tools,omitempty"`
	Personas                 []PersonaInspectData  `json:"personas,omitempty"`
	Judges                   []JudgeInspectData    `json:"judges,omitempty"`
	SelfPlayRoles            []SelfPlayRoleData    `json:"self_play_roles,omitempty"`
	SelfPlayEnabled          bool                  `json:"self_play_enabled"`
	AvailableTaskTypes       []string              `json:"available_task_types"`
	Defaults                 *DefaultsInspectData  `json:"defaults,omitempty"`
	ValidationPassed         bool                  `json:"validation_passed"`
	ValidationError          string                `json:"validation_error,omitempty"`
	ValidationErrors         []string              `json:"validation_errors,omitempty"`
	ValidationWarnings       int                   `json:"validation_warnings"`
	ValidationWarningDetails []string              `json:"validation_warning_details,omitempty"`
	ValidationChecks         []ValidationCheckData `json:"validation_checks,omitempty"`
	CacheStats               *CacheStatsData       `json:"cache_stats,omitempty"`
}

// ValidationCheckData represents a single validation check result for display
type ValidationCheckData struct {
	Name    string   `json:"name"`
	Passed  bool     `json:"passed"`
	Warning bool     `json:"warning,omitempty"`
	Issues  []string `json:"issues,omitempty"`
}

// PromptInspectData contains detailed prompt configuration info
type PromptInspectData struct {
	ID             string            `json:"id"`
	File           string            `json:"file"`
	TaskType       string            `json:"task_type,omitempty"`
	Version        string            `json:"version,omitempty"`
	Description    string            `json:"description,omitempty"`
	Variables      []string          `json:"variables,omitempty"`
	VariableValues map[string]string `json:"variable_values,omitempty"`
	AllowedTools   []string          `json:"allowed_tools,omitempty"`
	Validators     []string          `json:"validators,omitempty"`
	HasMedia       bool              `json:"has_media,omitempty"`
}

// ProviderInspectData contains detailed provider configuration info
type ProviderInspectData struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Type        string   `json:"type,omitempty"`
	Model       string   `json:"model,omitempty"`
	Group       string   `json:"group,omitempty"`
	Temperature float32  `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	UsedBy      []string `json:"used_by,omitempty"` // What uses this provider (e.g., "judge:quality", "role:user")
}

// ScenarioInspectData contains detailed scenario configuration info
type ScenarioInspectData struct {
	ID                 string   `json:"id"`
	File               string   `json:"file"`
	TaskType           string   `json:"task_type,omitempty"`
	Description        string   `json:"description,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	TurnCount          int      `json:"turn_count"`
	HasSelfPlay        bool     `json:"has_self_play,omitempty"`
	AssertionCount     int      `json:"assertion_count,omitempty"`
	ConvAssertionCount int      `json:"conv_assertion_count,omitempty"`
	Providers          []string `json:"providers,omitempty"`
	Streaming          bool     `json:"streaming,omitempty"`
}

// ToolInspectData contains detailed tool configuration info
type ToolInspectData struct {
	Name          string   `json:"name"`
	File          string   `json:"file"`
	Description   string   `json:"description,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	TimeoutMs     int      `json:"timeout_ms,omitempty"`
	InputParams   []string `json:"input_params,omitempty"`
	HasMockData   bool     `json:"has_mock_data,omitempty"`
	HasHTTPConfig bool     `json:"has_http_config,omitempty"`
}

// PersonaInspectData contains detailed persona configuration info
type PersonaInspectData struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Description string   `json:"description,omitempty"`
	Goals       []string `json:"goals,omitempty"`
	RoleID      string   `json:"role_id,omitempty"`  // Self-play role ID using this persona
	Provider    string   `json:"provider,omitempty"` // Provider assigned to this role
}

// JudgeInspectData contains detailed judge configuration info
type JudgeInspectData struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// SelfPlayRoleData contains detailed self-play role configuration info
type SelfPlayRoleData struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Persona  string `json:"persona,omitempty"` // Persona used by this role (from scenarios)
}

// DefaultsInspectData contains arena defaults
type DefaultsInspectData struct {
	Temperature   float32  `json:"temperature,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Seed          int      `json:"seed,omitempty"`
	Concurrency   int      `json:"concurrency,omitempty"`
	OutputDir     string   `json:"output_dir,omitempty"`
	OutputFormats []string `json:"output_formats,omitempty"`
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

func collectInspectionData(cfg *config.Config, configFile string) *InspectionData {
	data := &InspectionData{
		ConfigFile:         filepath.Base(configFile),
		AvailableTaskTypes: getAvailableTaskTypes(cfg),
		SelfPlayEnabled:    cfg.SelfPlay != nil && cfg.SelfPlay.Enabled,
	}

	// Collect each section using helper functions
	data.PromptConfigs = collectPromptConfigs(cfg)
	data.Providers = collectProviders(cfg)
	data.Scenarios = collectScenarios(cfg)
	data.Tools = collectTools(cfg)
	data.Personas = collectPersonas(cfg)
	data.Judges = collectJudges(cfg)
	data.SelfPlayRoles = collectSelfPlayRoles(cfg)
	data.Defaults = collectDefaults(cfg)

	// Collect cache stats if requested
	if inspectStats {
		data.CacheStats = collectCacheStats(cfg)
	}

	return data
}

// collectPromptConfigs extracts prompt configuration details
func collectPromptConfigs(cfg *config.Config) []PromptInspectData {
	var prompts []PromptInspectData
	for _, pc := range cfg.PromptConfigs {
		promptData := PromptInspectData{
			ID:             pc.ID,
			File:           pc.File,
			VariableValues: pc.Vars,
		}
		populatePromptDetails(&promptData, cfg, pc.ID)
		prompts = append(prompts, promptData)
	}
	return prompts
}

// populatePromptDetails fills in details from loaded prompt config
func populatePromptDetails(promptData *PromptInspectData, cfg *config.Config, promptID string) {
	loaded, ok := cfg.LoadedPromptConfigs[promptID]
	if !ok || loaded.Config == nil {
		return
	}
	promptCfg, ok := loaded.Config.(*prompt.Config)
	if !ok {
		return
	}
	promptData.TaskType = promptCfg.Spec.TaskType
	promptData.Version = promptCfg.Spec.Version
	promptData.Description = promptCfg.Spec.Description
	promptData.AllowedTools = promptCfg.Spec.AllowedTools
	promptData.HasMedia = promptCfg.Spec.MediaConfig != nil && promptCfg.Spec.MediaConfig.Enabled

	for _, v := range promptCfg.Spec.Variables {
		promptData.Variables = append(promptData.Variables, v.Name)
	}
	for _, v := range promptCfg.Spec.Validators {
		promptData.Validators = append(promptData.Validators, v.Type)
	}
}

// collectProviders extracts provider configuration details
func collectProviders(cfg *config.Config) []ProviderInspectData {
	// Build a map of provider usage
	providerUsage := buildProviderUsageMap(cfg)

	var providers []ProviderInspectData
	for _, p := range cfg.Providers {
		providerData := ProviderInspectData{
			File:  p.File,
			Group: p.Group,
		}
		populateProviderDetails(&providerData, cfg, p.File)
		// Skip providers that couldn't be loaded (no ID means no data)
		if providerData.ID == "" {
			continue
		}
		if usage, ok := providerUsage[providerData.ID]; ok {
			providerData.UsedBy = usage
		}
		providers = append(providers, providerData)
	}

	// Add any judge providers not already in the list
	providers = appendJudgeProviders(providers, cfg, providerUsage)

	return providers
}

// buildProviderUsageMap builds a map of provider ID to what uses it
func buildProviderUsageMap(cfg *config.Config) map[string][]string {
	usage := make(map[string][]string)

	// Track judge usage
	for _, j := range cfg.Judges {
		if j.Provider != "" {
			usage[j.Provider] = append(usage[j.Provider], "judge:"+j.Name)
		}
	}

	// Track self-play role usage
	if cfg.SelfPlay != nil {
		for _, role := range cfg.SelfPlay.Roles {
			if role.Provider != "" {
				usage[role.Provider] = append(usage[role.Provider], "role:"+role.ID)
			}
		}
	}

	return usage
}

// appendJudgeProviders adds judge providers that aren't already in the providers list
func appendJudgeProviders(
	providers []ProviderInspectData,
	cfg *config.Config,
	usage map[string][]string,
) []ProviderInspectData {
	existingIDs := make(map[string]bool)
	for _, p := range providers {
		existingIDs[p.ID] = true
	}

	// Check for judge providers not in the main provider list
	for _, j := range cfg.Judges {
		if j.Provider == "" || existingIDs[j.Provider] {
			continue
		}
		// Try to find this provider in LoadedProviders
		if loaded, ok := cfg.LoadedProviders[j.Provider]; ok && loaded != nil {
			providerData := ProviderInspectData{
				ID:          loaded.ID,
				Type:        loaded.Type,
				Model:       loaded.Model,
				Group:       "judge",
				Temperature: loaded.Defaults.Temperature,
				MaxTokens:   loaded.Defaults.MaxTokens,
			}
			if u, ok := usage[j.Provider]; ok {
				providerData.UsedBy = u
			}
			providers = append(providers, providerData)
			existingIDs[j.Provider] = true
		}
	}

	return providers
}

// populateProviderDetails fills in details from loaded provider
func populateProviderDetails(providerData *ProviderInspectData, cfg *config.Config, filePath string) {
	fileBase := getProviderIDFromFile(filePath)
	matched := findMatchingProvider(cfg, fileBase)
	if matched != nil {
		providerData.ID = matched.ID
		providerData.Type = matched.Type
		providerData.Model = matched.Model
		providerData.Temperature = matched.Defaults.Temperature
		providerData.MaxTokens = matched.Defaults.MaxTokens
	}
}

// findMatchingProvider finds a loaded provider by file base name
func findMatchingProvider(cfg *config.Config, fileBase string) *config.Provider {
	for provID, loaded := range cfg.LoadedProviders {
		if loaded == nil {
			continue
		}
		normalizedFile := strings.ReplaceAll(fileBase, "-", "")
		normalizedID := strings.ReplaceAll(provID, "-", "")
		if provID == fileBase || normalizedID == normalizedFile {
			return loaded
		}
	}
	return nil
}

// collectScenarios extracts scenario configuration details
func collectScenarios(cfg *config.Config) []ScenarioInspectData {
	var scenarios []ScenarioInspectData
	for _, s := range cfg.Scenarios {
		scenarioData := ScenarioInspectData{File: s.File}
		populateScenarioDetails(&scenarioData, cfg, s.File)
		scenarios = append(scenarios, scenarioData)
	}
	return scenarios
}

// populateScenarioDetails fills in details from loaded scenario
func populateScenarioDetails(scenarioData *ScenarioInspectData, cfg *config.Config, filePath string) {
	for id, loaded := range cfg.LoadedScenarios {
		if !strings.HasSuffix(filePath, filepath.Base(loaded.ID)) && loaded.ID != id {
			continue
		}
		scenarioData.ID = loaded.ID
		scenarioData.TaskType = loaded.TaskType
		scenarioData.Description = loaded.Description
		scenarioData.Mode = loaded.Mode
		scenarioData.TurnCount = len(loaded.Turns)
		scenarioData.Providers = loaded.Providers
		scenarioData.Streaming = loaded.Streaming
		scenarioData.ConvAssertionCount = len(loaded.ConversationAssertions)
		countScenarioAssertions(scenarioData, loaded)
		break
	}
}

// countScenarioAssertions counts assertions and detects self-play
func countScenarioAssertions(scenarioData *ScenarioInspectData, loaded *config.Scenario) {
	for i := range loaded.Turns {
		scenarioData.AssertionCount += len(loaded.Turns[i].Assertions)
		if loaded.Turns[i].Persona != "" || loaded.Turns[i].Turns > 0 {
			scenarioData.HasSelfPlay = true
		}
	}
}

// collectTools extracts tool configuration details
func collectTools(cfg *config.Config) []ToolInspectData {
	var tools []ToolInspectData
	for _, t := range cfg.LoadedTools {
		toolData := ToolInspectData{File: t.FilePath}
		parseToolManifest(&toolData, t.Data)
		tools = append(tools, toolData)
	}
	return tools
}

// toolManifestMetadata represents the metadata section of a tool manifest
type toolManifestMetadata struct {
	Name string `yaml:"name"`
}

// toolInputSchema represents the input schema for a tool
type toolInputSchema struct {
	Properties map[string]interface{} `yaml:"properties"`
	Required   []string               `yaml:"required"`
}

// toolManifestSpec represents the spec section of a tool manifest
type toolManifestSpec struct {
	Name         string          `yaml:"name"`
	Description  string          `yaml:"description"`
	Mode         string          `yaml:"mode"`
	TimeoutMs    int             `yaml:"timeout_ms"`
	InputSchema  toolInputSchema `yaml:"input_schema"`
	MockResult   interface{}     `yaml:"mock_result"`
	MockTemplate string          `yaml:"mock_template"`
	HTTP         interface{}     `yaml:"http"`
}

// toolManifest represents the structure of a tool YAML file
type toolManifest struct {
	Metadata toolManifestMetadata `yaml:"metadata"`
	Spec     toolManifestSpec     `yaml:"spec"`
}

// parseToolManifest parses tool YAML data into ToolInspectData
func parseToolManifest(toolData *ToolInspectData, data []byte) {
	if len(data) == 0 {
		return
	}
	var manifest toolManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return
	}
	if manifest.Metadata.Name != "" {
		toolData.Name = manifest.Metadata.Name
	} else if manifest.Spec.Name != "" {
		toolData.Name = manifest.Spec.Name
	}
	toolData.Description = manifest.Spec.Description
	toolData.Mode = manifest.Spec.Mode
	toolData.TimeoutMs = manifest.Spec.TimeoutMs
	toolData.HasMockData = manifest.Spec.MockResult != nil || manifest.Spec.MockTemplate != ""
	toolData.HasHTTPConfig = manifest.Spec.HTTP != nil

	for paramName := range manifest.Spec.InputSchema.Properties {
		toolData.InputParams = append(toolData.InputParams, paramName)
	}
}

// collectPersonas extracts persona configuration details
func collectPersonas(cfg *config.Config) []PersonaInspectData {
	var personas []PersonaInspectData
	for id, persona := range cfg.LoadedPersonas {
		personaData := PersonaInspectData{
			ID:          id,
			Description: persona.Description,
		}
		if len(persona.Goals) > 0 {
			personaData.Goals = persona.Goals
		}
		personas = append(personas, personaData)
	}
	return personas
}

// collectJudges extracts judge configuration details
func collectJudges(cfg *config.Config) []JudgeInspectData {
	var judges []JudgeInspectData
	for _, j := range cfg.Judges {
		judges = append(judges, JudgeInspectData{
			Name:     j.Name,
			Provider: j.Provider,
			Model:    j.Model,
		})
	}
	return judges
}

// collectSelfPlayRoles extracts self-play role configuration
func collectSelfPlayRoles(cfg *config.Config) []SelfPlayRoleData {
	if cfg.SelfPlay == nil {
		return nil
	}

	// Build role -> persona mapping from scenarios
	rolePersonaMap := buildRolePersonaMap(cfg)

	var roles []SelfPlayRoleData
	for _, role := range cfg.SelfPlay.Roles {
		roleData := SelfPlayRoleData{
			ID:       role.ID,
			Provider: role.Provider,
		}
		if persona, ok := rolePersonaMap[role.ID]; ok {
			roleData.Persona = persona
		}
		roles = append(roles, roleData)
	}
	return roles
}

// buildRolePersonaMap scans scenarios to find which personas are used by which roles
func buildRolePersonaMap(cfg *config.Config) map[string]string {
	rolePersona := make(map[string]string)
	for _, scenario := range cfg.LoadedScenarios {
		for i := range scenario.Turns {
			turn := &scenario.Turns[i]
			if turn.Persona != "" && turn.Role != "user" && turn.Role != "assistant" {
				// This is a self-play turn with a persona
				rolePersona[turn.Role] = turn.Persona
			}
		}
	}
	return rolePersona
}

// collectDefaults extracts default configuration values
func collectDefaults(cfg *config.Config) *DefaultsInspectData {
	outputCfg := cfg.Defaults.GetOutputConfig()
	return &DefaultsInspectData{
		Temperature:   cfg.Defaults.Temperature,
		MaxTokens:     cfg.Defaults.MaxTokens,
		Seed:          cfg.Defaults.Seed,
		Concurrency:   cfg.Defaults.Concurrency,
		OutputDir:     outputCfg.Dir,
		OutputFormats: outputCfg.Formats,
	}
}

// getProviderIDFromFile extracts a provider ID from the file path
func getProviderIDFromFile(filePath string) string {
	base := filepath.Base(filePath)
	// Remove common suffixes
	for _, suffix := range []string{".provider.yaml", ".provider.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return base
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

// Style definitions for terminal output
var (
	// Box styles
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(theme.ColorPrimary)).
			Padding(0, 1).
			Width(boxWidth)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorPrimary)).
			MarginBottom(1)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(theme.ColorWhite)).
				Background(lipgloss.Color(theme.ColorPrimary)).
				Padding(0, 1).
				MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorLightGray))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWhite))

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorEmerald)).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorGray))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorSuccess))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWarning))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorError))

	tagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorPrimary)).
			Background(lipgloss.Color("#2D2D3D")).
			Padding(0, 1)
)

func outputText(data *InspectionData, _ *config.Config) error {
	printBanner(data.ConfigFile)
	printSections(data)
	return nil
}

// printBanner prints the inspector header banner
func printBanner(configFile string) {
	banner := headerStyle.Render("✨ PromptArena Configuration Inspector ✨")
	fmt.Println(banner)
	fmt.Println()
	fmt.Println(boxStyle.Render(
		labelStyle.Render("Configuration: ") + highlightStyle.Render(configFile),
	))
	fmt.Println()
}

// sectionVisibility tracks which sections should be displayed
type sectionVisibility struct {
	all       bool
	prompts   bool
	providers bool
	scenarios bool
	tools     bool
	personas  bool
	judges    bool
}

// getSectionVisibility determines which sections to show based on inspectSection flag
func getSectionVisibility() sectionVisibility {
	showAll := inspectSection == ""
	return sectionVisibility{
		all:       showAll,
		prompts:   showAll || inspectSection == "prompts",
		providers: showAll || inspectSection == "providers",
		scenarios: showAll || inspectSection == "scenarios",
		tools:     showAll || inspectSection == "tools",
		personas:  showAll || inspectSection == "personas",
		judges:    showAll || inspectSection == "judges",
	}
}

// printSections prints all visible sections
func printSections(data *InspectionData) {
	vis := getSectionVisibility()
	printConfigSections(data, vis)
	printSummarySections(data, vis)
}

// printConfigSections prints the main configuration sections
func printConfigSections(data *InspectionData, vis sectionVisibility) {
	if vis.prompts && len(data.PromptConfigs) > 0 {
		printPromptsSection(data)
	}
	if vis.providers && len(data.Providers) > 0 {
		printProvidersSection(data)
	}
	if vis.scenarios && len(data.Scenarios) > 0 {
		printScenariosSection(data)
	}
	if vis.tools && len(data.Tools) > 0 {
		printToolsSection(data)
	}
	if vis.personas && len(data.Personas) > 0 {
		printPersonasSection(data)
	}
	if vis.judges && len(data.Judges) > 0 {
		printJudgesSection(data)
	}
}

// printSummarySections prints the summary sections (defaults, validation, cache)
func printSummarySections(data *InspectionData, vis sectionVisibility) {
	if vis.all && data.Defaults != nil {
		printDefaultsSection(data)
	}
	if vis.all {
		printValidationSection(data)
	}
	if inspectStats && data.CacheStats != nil {
		printCacheStatistics(data.CacheStats)
	}
}

func printPromptsSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 📋 Prompt Configs (%d) ", len(data.PromptConfigs))))
	fmt.Println()

	for i := range data.PromptConfigs {
		p := &data.PromptConfigs[i]
		lines := buildPromptLines(p)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildPromptLines builds display lines for a prompt config
func buildPromptLines(p *PromptInspectData) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if p.TaskType != "" {
		lines = append(lines, labelStyle.Render(labelTaskType)+tagStyle.Render(p.TaskType))
	}
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))

	if inspectVerbose {
		lines = append(lines, buildPromptVerboseLines(p)...)
	}
	return lines
}

// buildPromptVerboseLines builds verbose detail lines for a prompt
func buildPromptVerboseLines(p *PromptInspectData) []string {
	var lines []string
	if p.Description != "" {
		desc := truncateInspectString(p.Description, maxDescLengthPrompt)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}
	if p.Version != "" {
		lines = append(lines, labelStyle.Render(labelVersion)+valueStyle.Render(p.Version))
	}
	if len(p.Variables) > 0 {
		lines = append(lines, labelStyle.Render(labelVariables)+tagStyle.Render(strings.Join(p.Variables, ", ")))
		for k, v := range p.VariableValues {
			lines = append(lines, dimStyle.Render("    "+k+": ")+valueStyle.Render(v))
		}
	}
	if len(p.AllowedTools) > 0 {
		lines = append(lines, labelStyle.Render(labelTools)+valueStyle.Render(strings.Join(p.AllowedTools, ", ")))
	}
	if len(p.Validators) > 0 {
		lines = append(lines, labelStyle.Render(labelValidators)+valueStyle.Render(strings.Join(p.Validators, ", ")))
	}
	if p.HasMedia {
		lines = append(lines, labelStyle.Render(labelMedia)+successStyle.Render("✓ enabled"))
	}
	return lines
}

func printProvidersSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔌 Providers (%d) ", len(data.Providers))))
	fmt.Println()

	byGroup := groupProvidersByGroup(data.Providers)
	groups := getSortedGroups(byGroup)

	for _, group := range groups {
		fmt.Println(labelStyle.Render("Group: ") + tagStyle.Render(group))
		for _, p := range byGroup[group] {
			lines := buildProviderLines(&p)
			fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
		}
	}
	fmt.Println()
}

// groupProvidersByGroup groups providers by their group field
func groupProvidersByGroup(providers []ProviderInspectData) map[string][]ProviderInspectData {
	byGroup := make(map[string][]ProviderInspectData)
	for _, p := range providers {
		group := p.Group
		if group == "" {
			group = "default"
		}
		byGroup[group] = append(byGroup[group], p)
	}
	return byGroup
}

// getSortedGroups returns sorted group names
func getSortedGroups(byGroup map[string][]ProviderInspectData) []string {
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

// buildProviderLines builds display lines for a provider
func buildProviderLines(p *ProviderInspectData) []string {
	headerLine := highlightStyle.Render(p.ID)
	if p.Type != "" {
		headerLine += dimStyle.Render(" (") + valueStyle.Render(p.Type) + dimStyle.Render(")")
	}
	lines := []string{headerLine}

	if p.Model != "" {
		lines = append(lines, labelStyle.Render(labelModel)+valueStyle.Render(p.Model))
	}
	if inspectVerbose {
		lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))
		if p.Temperature > 0 {
			lines = append(lines, labelStyle.Render(labelTemperature)+valueStyle.Render(fmt.Sprintf("%.2f", p.Temperature)))
		}
		if p.MaxTokens > 0 {
			lines = append(lines, labelStyle.Render(labelMaxTok)+valueStyle.Render(fmt.Sprintf("%d", p.MaxTokens)))
		}
	}
	return lines
}

func printScenariosSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🎬 Scenarios (%d) ", len(data.Scenarios))))
	fmt.Println()

	for i := range data.Scenarios {
		s := &data.Scenarios[i]
		lines := buildScenarioLines(s)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildScenarioLines builds display lines for a scenario
func buildScenarioLines(s *ScenarioInspectData) []string {
	headerLine := highlightStyle.Render(s.ID)
	if s.Mode != "" {
		headerLine += dimStyle.Render(" [") + tagStyle.Render(s.Mode) + dimStyle.Render("]")
	}

	infoLine := labelStyle.Render(labelTask) + valueStyle.Render(s.TaskType)
	infoLine += dimStyle.Render(" • ") + labelStyle.Render("Turns: ") + valueStyle.Render(fmt.Sprintf("%d", s.TurnCount))

	lines := []string{headerLine, infoLine}
	if inspectVerbose {
		lines = append(lines, buildScenarioVerboseLines(s)...)
	}
	return lines
}

// buildScenarioVerboseLines builds verbose detail lines for a scenario
func buildScenarioVerboseLines(s *ScenarioInspectData) []string {
	var lines []string
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(s.File))

	if s.Description != "" {
		desc := truncateInspectString(s.Description, maxDescLength)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}

	flags := buildScenarioFlags(s)
	if len(flags) > 0 {
		lines = append(lines, labelStyle.Render(labelFlags)+strings.Join(flags, " "))
	}

	if len(s.Providers) > 0 {
		lines = append(lines, labelStyle.Render(labelProviders)+valueStyle.Render(strings.Join(s.Providers, ", ")))
	}
	return lines
}

// buildScenarioFlags builds the flags list for a scenario
func buildScenarioFlags(s *ScenarioInspectData) []string {
	var flags []string
	if s.HasSelfPlay {
		flags = append(flags, tagStyle.Render("self-play"))
	}
	if s.Streaming {
		flags = append(flags, tagStyle.Render("streaming"))
	}
	if s.AssertionCount > 0 {
		flags = append(flags, tagStyle.Render(fmt.Sprintf("%d turn assertions", s.AssertionCount)))
	}
	if s.ConvAssertionCount > 0 {
		flags = append(flags, tagStyle.Render(fmt.Sprintf("%d conv assertions", s.ConvAssertionCount)))
	}
	return flags
}

func printToolsSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔧 Tools (%d) ", len(data.Tools))))
	fmt.Println()

	for _, t := range data.Tools {
		lines := buildToolLines(&t)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildToolLines builds display lines for a tool
func buildToolLines(t *ToolInspectData) []string {
	var lines []string

	// Header with name or filename
	header := t.Name
	if header == "" {
		header = filepath.Base(t.File)
	}
	lines = append(lines, highlightStyle.Render(header))

	if t.Mode != "" {
		lines = append(lines, labelStyle.Render(labelMode)+tagStyle.Render(getToolModeDisplay(t.Mode)))
	}

	if t.Name != "" {
		lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(filepath.Base(t.File)))
	}

	if inspectVerbose {
		lines = append(lines, buildToolVerboseLines(t)...)
	}
	return lines
}

// getToolModeDisplay returns a display string for tool mode
func getToolModeDisplay(mode string) string {
	switch mode {
	case "mock":
		return "🧪 mock"
	case "live":
		return "🔴 live"
	default:
		return mode
	}
}

// buildToolVerboseLines builds verbose detail lines for a tool
func buildToolVerboseLines(t *ToolInspectData) []string {
	var lines []string
	if t.Description != "" {
		desc := truncateInspectString(t.Description, maxDescLength)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}
	if len(t.InputParams) > 0 {
		lines = append(lines, labelStyle.Render(labelParams)+tagStyle.Render(strings.Join(t.InputParams, ", ")))
	}
	if t.TimeoutMs > 0 {
		lines = append(lines, labelStyle.Render(labelTimeout)+valueStyle.Render(fmt.Sprintf("%dms", t.TimeoutMs)))
	}

	var flags []string
	if t.HasMockData {
		flags = append(flags, "mock data")
	}
	if t.HasHTTPConfig {
		flags = append(flags, "HTTP")
	}
	if len(flags) > 0 {
		lines = append(lines, labelStyle.Render(labelFeatures)+dimStyle.Render(strings.Join(flags, ", ")))
	}
	return lines
}

func printPersonasSection(data *InspectionData) {
	// Combined Self-Play section showing personas and roles together
	header := fmt.Sprintf(" 🎭 Self-Play (%d personas, %d roles) ", len(data.Personas), len(data.SelfPlayRoles))
	fmt.Println(sectionHeaderStyle.Render(header))
	fmt.Println()

	// Show personas
	if len(data.Personas) > 0 {
		fmt.Println(labelStyle.Render("Personas:"))
		for _, p := range data.Personas {
			lines := buildPersonaLines(&p)
			fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
		}
	}

	// Show self-play roles
	if len(data.SelfPlayRoles) > 0 {
		fmt.Println(labelStyle.Render("Roles:"))
		var lines []string
		for _, role := range data.SelfPlayRoles {
			line := highlightStyle.Render(role.ID)
			if role.Persona != "" {
				line += dimStyle.Render(" (") + valueStyle.Render(role.Persona) + dimStyle.Render(")")
			}
			if role.Provider != "" {
				line += dimStyle.Render(" → ") + valueStyle.Render(role.Provider)
			}
			lines = append(lines, line)
		}
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildPersonaLines builds display lines for a persona
func buildPersonaLines(p *PersonaInspectData) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if inspectVerbose {
		if p.Description != "" {
			desc := truncateInspectString(p.Description, maxDescLength)
			lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
		}
		if len(p.Goals) > 0 {
			lines = append(lines, labelStyle.Render(labelGoals))
			lines = append(lines, buildGoalLines(p.Goals)...)
		}
	}
	return lines
}

// buildGoalLines builds display lines for persona goals (limited to maxGoalsDisplayed)
func buildGoalLines(goals []string) []string {
	var lines []string
	for i, goal := range goals {
		if i >= maxGoalsDisplayed {
			remaining := len(goals) - maxGoalsDisplayed
			lines = append(lines, dimStyle.Render(fmt.Sprintf("    ... and %d more", remaining)))
			break
		}
		goalStr := truncateInspectString(goal, maxGoalLength)
		lines = append(lines, dimStyle.Render("    • ")+valueStyle.Render(goalStr))
	}
	return lines
}

func printJudgesSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" ⚖️  Judges (%d) ", len(data.Judges))))
	fmt.Println()

	var lines []string
	for _, j := range data.Judges {
		line := highlightStyle.Render(j.Name)
		line += dimStyle.Render(" → ") + valueStyle.Render(j.Provider)
		if j.Model != "" {
			line += dimStyle.Render(" (") + valueStyle.Render(j.Model) + dimStyle.Render(")")
		}
		lines = append(lines, line)
	}
	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	fmt.Println()
}

func printDefaultsSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(" ⚙️  Defaults "))
	fmt.Println()

	var lines []string
	d := data.Defaults

	if d.Temperature > 0 {
		lines = append(lines, labelStyle.Render("Temperature: ")+valueStyle.Render(fmt.Sprintf("%.2f", d.Temperature)))
	}
	if d.MaxTokens > 0 {
		lines = append(lines, labelStyle.Render("Max Tokens: ")+valueStyle.Render(fmt.Sprintf("%d", d.MaxTokens)))
	}
	if d.Seed > 0 {
		lines = append(lines, labelStyle.Render("Seed: ")+valueStyle.Render(fmt.Sprintf("%d", d.Seed)))
	}
	if d.Concurrency > 0 {
		lines = append(lines, labelStyle.Render("Concurrency: ")+valueStyle.Render(fmt.Sprintf("%d", d.Concurrency)))
	}
	if d.OutputDir != "" {
		lines = append(lines, labelStyle.Render("Output Dir: ")+valueStyle.Render(d.OutputDir))
	}
	if len(d.OutputFormats) > 0 {
		lines = append(lines, labelStyle.Render("Formats: ")+valueStyle.Render(strings.Join(d.OutputFormats, ", ")))
	}

	if len(lines) > 0 {
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

func printValidationSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(" ✅ Validation "))
	fmt.Println()

	lines := buildValidationLines(data)
	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	fmt.Println()
}

// buildValidationLines builds all display lines for validation section
func buildValidationLines(data *InspectionData) []string {
	failedCount := countFailedChecks(data.ValidationChecks)
	var lines []string

	// Summary line
	if data.ValidationPassed && failedCount == 0 {
		lines = append(lines, successStyle.Render("✓ Configuration is valid"))
	} else {
		lines = append(lines, errorStyle.Render("✗ Configuration has errors"))
	}

	// Show check results as a checklist
	if len(data.ValidationChecks) > 0 {
		lines = append(lines, "", labelStyle.Render("Connectivity Checks:"))
		lines = append(lines, buildCheckLines(data.ValidationChecks)...)
	}

	// Show validation error details
	if !data.ValidationPassed && len(data.ValidationErrors) > 0 {
		lines = append(lines, "", errorStyle.Render("Errors:"))
		for _, errMsg := range data.ValidationErrors {
			lines = append(lines, errorStyle.Render("  ☒ ")+valueStyle.Render(errMsg))
		}
	}

	// Show actionable warnings
	actionableWarnings := filterActionableWarnings(data.ValidationWarningDetails)
	if len(actionableWarnings) > 0 {
		lines = append(lines, "", dimStyle.Render("Notes:"))
		for _, warn := range actionableWarnings {
			lines = append(lines, dimStyle.Render("  ℹ "+warn))
		}
	}

	return lines
}

// countFailedChecks counts validation checks that failed (not warnings)
func countFailedChecks(checks []ValidationCheckData) int {
	count := 0
	for _, check := range checks {
		if !check.Passed && !check.Warning {
			count++
		}
	}
	return count
}

// buildCheckLines builds display lines for validation checks
func buildCheckLines(checks []ValidationCheckData) []string {
	var lines []string
	for _, check := range checks {
		lines = append(lines, buildSingleCheckLine(check)...)
	}
	return lines
}

// buildSingleCheckLine builds display lines for a single validation check
func buildSingleCheckLine(check ValidationCheckData) []string {
	var lines []string
	if check.Passed {
		lines = append(lines, successStyle.Render("  ☑ ")+valueStyle.Render(check.Name))
	} else if check.Warning {
		lines = append(lines, warningStyle.Render("  ⚠ ")+valueStyle.Render(check.Name))
		for _, issue := range check.Issues {
			lines = append(lines, dimStyle.Render("      • "+issue))
		}
	} else {
		lines = append(lines, errorStyle.Render("  ☒ ")+valueStyle.Render(check.Name))
		for _, issue := range check.Issues {
			lines = append(lines, errorStyle.Render("      • "+issue))
		}
	}
	return lines
}

// filterActionableWarnings filters out informational "not defined" warnings
// and keeps only warnings that suggest something might need fixing
func filterActionableWarnings(warnings []string) []string {
	var actionable []string
	for _, w := range warnings {
		// Skip informational "not defined" messages for optional features
		if strings.Contains(w, "no personas defined") ||
			strings.Contains(w, "no scenarios defined") ||
			strings.Contains(w, "no prompt configs defined") ||
			strings.Contains(w, "no providers defined") {
			continue
		}
		actionable = append(actionable, w)
	}
	return actionable
}

// printCacheStatistics prints the cache statistics section
func printCacheStatistics(cacheStats *CacheStatsData) {
	fmt.Println(sectionHeaderStyle.Render(" 💾 Cache Statistics "))
	fmt.Println()

	var lines []string

	promptCacheEntry := fmt.Sprintf(entriesFormat, cacheStats.PromptCache.Size)
	lines = append(lines, labelStyle.Render("Prompt Cache: ")+valueStyle.Render(promptCacheEntry))
	if inspectVerbose && len(cacheStats.PromptCache.Entries) > 0 {
		for _, entry := range cacheStats.PromptCache.Entries {
			lines = append(lines, dimStyle.Render("  • "+entry))
		}
	}

	fragmentCacheEntry := fmt.Sprintf(entriesFormat, cacheStats.FragmentCache.Size)
	lines = append(lines, labelStyle.Render("Fragment Cache: ")+valueStyle.Render(fragmentCacheEntry))

	if cacheStats.SelfPlayCache.Size > 0 {
		selfPlayEntry := fmt.Sprintf(entriesFormat, cacheStats.SelfPlayCache.Size)
		lines = append(lines, labelStyle.Render("Self-Play Cache: ")+valueStyle.Render(selfPlayEntry))
		if cacheStats.SelfPlayCache.HitRate > 0 {
			hitRate := cacheStats.SelfPlayCache.HitRate * percentMultiplier
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  Hit Rate: %.1f%%", hitRate)))
		}
	}

	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	fmt.Println()
}

// truncateInspectString truncates a string to the given length with ellipsis
func truncateInspectString(s string, maxLen int) string {
	// Remove newlines and extra whitespace
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// collectConnectivityChecks validates that components are properly connected
// This adds additional checks beyond what the validator already provides
func collectConnectivityChecks(data *InspectionData) []ValidationCheckData {
	var checks []ValidationCheckData

	// Check 1: Tools defined but not allowed on any prompt
	definedToolNames := make(map[string]bool)
	for _, t := range data.Tools {
		if t.Name != "" {
			definedToolNames[t.Name] = true
		}
	}

	allowedToolNames := make(map[string]bool)
	for i := range data.PromptConfigs {
		for _, tool := range data.PromptConfigs[i].AllowedTools {
			allowedToolNames[tool] = true
		}
	}

	toolsUsedCheck := ValidationCheckData{
		Name:   "Tools connected to prompts",
		Passed: true,
	}
	for toolName := range definedToolNames {
		if !allowedToolNames[toolName] {
			toolsUsedCheck.Passed = false
			toolsUsedCheck.Warning = true
			toolsUsedCheck.Issues = append(toolsUsedCheck.Issues,
				fmt.Sprintf("tool '%s' is defined but not allowed on any prompt", toolName))
		}
	}
	checks = append(checks, toolsUsedCheck)

	// Check 2: Tools allowed on prompts but not defined
	toolsDefinedCheck := ValidationCheckData{
		Name:   "Allowed tools are defined",
		Passed: true,
	}
	for toolName := range allowedToolNames {
		if !definedToolNames[toolName] {
			toolsDefinedCheck.Passed = false
			toolsDefinedCheck.Issues = append(toolsDefinedCheck.Issues,
				fmt.Sprintf("prompt allows tool '%s' but it is not defined in tools section", toolName))
		}
	}
	checks = append(checks, toolsDefinedCheck)

	// Check 3: Self-play roles have valid providers (if self-play is enabled)
	if data.SelfPlayEnabled && len(data.SelfPlayRoles) > 0 {
		selfPlayProvidersCheck := ValidationCheckData{
			Name:   "Self-play roles have valid providers",
			Passed: true,
		}
		// Self-play providers are validated by the main validator
		checks = append(checks, selfPlayProvidersCheck)
	}

	return checks
}
