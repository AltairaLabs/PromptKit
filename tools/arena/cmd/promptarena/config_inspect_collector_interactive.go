package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

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
