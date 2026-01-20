package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ValidationCheck represents a single validation check result
type ValidationCheck struct {
	Name    string   // Name of the check (e.g., "Prompt configs exist")
	Passed  bool     // Whether the check passed
	Issues  []string // List of specific issues if failed
	Warning bool     // Whether this is a warning (not an error)
}

// ValidationResult contains structured validation results
type ValidationResult struct {
	Checks []ValidationCheck
}

// ConfigValidator validates configuration consistency and references
type ConfigValidator struct {
	config     *Config
	configPath string // path to the config file for resolving relative paths
	errors     []error
	warns      []string
	checks     []ValidationCheck // structured check results
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(cfg *Config) *ConfigValidator {
	return &ConfigValidator{
		config: cfg,
		errors: make([]error, 0),
		warns:  make([]string, 0),
		checks: make([]ValidationCheck, 0),
	}
}

// NewConfigValidatorWithPath creates a new configuration validator with a config file path
// for resolving relative file references
func NewConfigValidatorWithPath(cfg *Config, configPath string) *ConfigValidator {
	return &ConfigValidator{
		config:     cfg,
		configPath: configPath,
		errors:     make([]error, 0),
		warns:      make([]string, 0),
		checks:     make([]ValidationCheck, 0),
	}
}

// Validate performs comprehensive validation of the configuration
func (v *ConfigValidator) Validate() error {
	v.validatePromptConfigs()
	v.validateProviders()
	v.validateScenarios()
	v.validateEvals()
	v.validatePersonas()
	v.validateSelfPlay()
	v.validateJudges()
	v.validateJudgeDefaults()
	v.validateCrossReferences()
	v.validateToolUsage()
	v.validateTaskTypeUsage()

	if len(v.errors) > 0 {
		return fmt.Errorf("configuration validation failed with %d errors: %v", len(v.errors), v.errors)
	}
	return nil
}

// GetWarnings returns all validation warnings
func (v *ConfigValidator) GetWarnings() []string {
	return v.warns
}

// GetErrors returns all validation errors as strings
func (v *ConfigValidator) GetErrors() []string {
	var errStrings []string
	for _, err := range v.errors {
		errStrings = append(errStrings, err.Error())
	}
	return errStrings
}

// validatePromptConfigs validates prompt configuration entries
func (v *ConfigValidator) validatePromptConfigs() {
	if len(v.config.PromptConfigs) == 0 {
		v.warns = append(v.warns, "no prompt configs defined")
		return
	}

	seen := make(map[string]bool)
	for i, pc := range v.config.PromptConfigs {
		// Validate ID is not empty
		if pc.ID == "" {
			v.errors = append(v.errors, fmt.Errorf("prompt config at index %d missing ID", i))
			continue
		}

		// Check for duplicates
		if seen[pc.ID] {
			v.errors = append(v.errors, fmt.Errorf("duplicate prompt config ID: %s", pc.ID))
		}
		seen[pc.ID] = true

		// Validate file exists if specified
		if pc.File != "" {
			// Resolve path relative to base config directory
			configDir := ResolveConfigDir(v.config, v.configPath)
			checkPath := pc.File
			if !filepath.IsAbs(checkPath) {
				checkPath = filepath.Join(configDir, checkPath)
			}
			if _, err := os.Stat(checkPath); os.IsNotExist(err) {
				v.errors = append(v.errors, fmt.Errorf("prompt config file not found: %s", pc.File))
			}
		} else {
			v.warns = append(v.warns, fmt.Sprintf("prompt config %s has no file specified", pc.ID))
		}
	}
}

// validateProviders validates provider configurations
func (v *ConfigValidator) validateProviders() {
	if len(v.config.Providers) == 0 {
		v.warns = append(v.warns, "no providers defined")
		return
	}

	seen := make(map[string]bool)
	for i, provider := range v.config.Providers {
		// Check for duplicate files
		if seen[provider.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate provider file: %s", provider.File))
		}
		seen[provider.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := provider.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, provider.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("provider file not found: %s", provider.File))
		}

		// Default group if not set
		if provider.Group == "" {
			v.config.Providers[i].Group = "default"
		}
	}
}

// validateScenarios validates scenario file references
func (v *ConfigValidator) validateScenarios() {
	if len(v.config.Scenarios) == 0 && len(v.config.Evals) == 0 {
		v.warns = append(v.warns, "no scenarios or evals defined")
		return
	}

	seen := make(map[string]bool)
	for _, scenario := range v.config.Scenarios {
		// Check for duplicates
		if seen[scenario.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate scenario file: %s", scenario.File))
		}
		seen[scenario.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := scenario.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, scenario.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("scenario file not found: %s", scenario.File))
		}
	}
}

// validateEvals validates eval file references
func (v *ConfigValidator) validateEvals() {
	if len(v.config.Evals) == 0 {
		return
	}

	seen := make(map[string]bool)
	for _, eval := range v.config.Evals {
		// Check for duplicates
		if seen[eval.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate eval file: %s", eval.File))
		}
		seen[eval.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := eval.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, eval.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("eval file not found: %s", eval.File))
		}
	}
}

// validatePersonas validates persona configurations
func (v *ConfigValidator) validatePersonas() {
	if v.config.SelfPlay == nil || len(v.config.SelfPlay.Personas) == 0 {
		v.warns = append(v.warns, "no personas defined")
		return
	}

	seen := make(map[string]bool)
	for _, personaRef := range v.config.SelfPlay.Personas {
		// Check for duplicate files
		if seen[personaRef.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate persona file: %s", personaRef.File))
		}
		seen[personaRef.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := personaRef.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, personaRef.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("persona file not found: %s", personaRef.File))
		}
	}
}

// validateSelfPlay validates self-play configuration
func (v *ConfigValidator) validateSelfPlay() {
	if v.config.SelfPlay == nil {
		return // Self-play is optional
	}

	if !v.config.SelfPlay.Enabled {
		return // Disabled, no need to validate
	}

	// Validate roles
	if len(v.config.SelfPlay.Roles) == 0 {
		v.warns = append(v.warns, "self-play enabled but no roles defined")
		return
	}

	seen := make(map[string]bool)

	for _, role := range v.config.SelfPlay.Roles {
		// Check for duplicate role IDs
		if seen[role.ID] {
			v.errors = append(v.errors, fmt.Errorf("duplicate self-play role ID: %s", role.ID))
		}
		seen[role.ID] = true

		// Validate provider ID reference
		if role.Provider == "" {
			v.errors = append(v.errors, fmt.Errorf("self-play role %s missing provider", role.ID))
		}
		// Note: Provider existence validation happens in loadSelfPlayResources
		// after all providers are loaded
	}
}

// validateJudges validates judge configuration (names, provider references)
func (v *ConfigValidator) validateJudges() {
	if len(v.config.Judges) == 0 {
		return
	}

	seen := make(map[string]bool)
	for _, judge := range v.config.Judges {
		if judge.Name == "" {
			v.errors = append(v.errors, fmt.Errorf("judge entry missing name"))
		}
		if seen[judge.Name] {
			v.errors = append(v.errors, fmt.Errorf("duplicate judge name: %s", judge.Name))
		}
		seen[judge.Name] = true

		if judge.Provider == "" {
			v.errors = append(v.errors, fmt.Errorf("judge %s missing provider", judge.Name))
		}
		// Provider existence is enforced in loader via validateJudgeReferences after providers load
	}
}

// validateJudgeDefaults validates judge default settings (optional)
func (v *ConfigValidator) validateJudgeDefaults() {
	if v.config.JudgeDefaults == nil {
		return
	}
	// No required fields, but warn if prompt is empty when judge defaults are present
	if v.config.JudgeDefaults.Prompt == "" {
		v.warns = append(v.warns, "judge_defaults specified without a prompt")
	}
}

// validateCrossReferences validates references between components
func (v *ConfigValidator) validateCrossReferences() {
	taskTypes := v.getPromptTaskTypes()

	// Validate scenario task_type references
	for _, scenarioConfig := range v.config.Scenarios {
		scenario, err := LoadScenario(scenarioConfig.File)
		if err != nil {
			// Already reported in validateScenarios
			continue
		}

		// Check task_type exists in loaded prompt configs
		if scenario.TaskType != "" && !taskTypes[scenario.TaskType] {
			err := fmt.Errorf("scenario %s references unknown task_type: %s",
				scenarioConfig.File, scenario.TaskType)
			v.errors = append(v.errors, err)
		}
	}
}

// Helper methods to build ID sets
func (v *ConfigValidator) getPromptConfigIDs() map[string]bool {
	ids := make(map[string]bool)
	for _, pc := range v.config.PromptConfigs {
		if pc.ID != "" {
			ids[pc.ID] = true
		}
	}
	return ids
}

// getPromptTaskTypes returns all task_types from loaded prompt configs
func (v *ConfigValidator) getPromptTaskTypes() map[string]bool {
	taskTypes := make(map[string]bool)
	for _, pcData := range v.config.LoadedPromptConfigs {
		if pcData != nil && pcData.TaskType != "" {
			taskTypes[pcData.TaskType] = true
		}
	}
	return taskTypes
}

// validateToolUsage checks that tools are properly connected to prompts
func (v *ConfigValidator) validateToolUsage() {
	definedTools := v.getDefinedToolNames()
	allowedTools := v.getAllowedToolNames()

	check := v.buildToolUsageCheck(definedTools, allowedTools)
	if len(check.Issues) > 0 || check.Passed {
		v.checks = append(v.checks, check)
	}
}

// getDefinedToolNames returns all tool names defined in the config
func (v *ConfigValidator) getDefinedToolNames() map[string]bool {
	definedTools := make(map[string]bool)
	for _, t := range v.config.LoadedTools {
		baseName := filepath.Base(t.FilePath)
		definedTools[baseName] = true
	}
	return definedTools
}

// getAllowedToolNames returns all tool names allowed by prompts
func (v *ConfigValidator) getAllowedToolNames() map[string]bool {
	allowedTools := make(map[string]bool)
	for _, pc := range v.config.LoadedPromptConfigs {
		if pc != nil && pc.Config != nil {
			if promptCfg, ok := pc.Config.(interface{ GetAllowedTools() []string }); ok {
				for _, tool := range promptCfg.GetAllowedTools() {
					allowedTools[tool] = true
				}
			}
		}
	}
	return allowedTools
}

// buildToolUsageCheck builds the validation check for tool usage
func (v *ConfigValidator) buildToolUsageCheck(definedTools, allowedTools map[string]bool) ValidationCheck {
	check := ValidationCheck{
		Name:   "Tools are used by prompts",
		Passed: true,
	}
	for toolFile := range definedTools {
		// Check if this specific tool is allowed by any prompt
		if !allowedTools[toolFile] {
			check.Passed = false
			check.Warning = true
			issue := fmt.Sprintf("tool %s is defined but not allowed by any prompt", toolFile)
			check.Issues = append(check.Issues, issue)
		}
	}
	return check
}

// validateTaskTypeUsage checks task_type connections between prompts and scenarios
func (v *ConfigValidator) validateTaskTypeUsage() {
	taskTypeToPrompts := v.getTaskTypeToPromptMapping()
	usedTaskTypes := v.getUsedTaskTypes()

	v.checkDuplicateTaskTypes(taskTypeToPrompts)
	v.checkUnusedTaskTypes(taskTypeToPrompts, usedTaskTypes)
	v.checkMissingTaskTypes(taskTypeToPrompts, usedTaskTypes)
}

// getTaskTypeToPromptMapping returns a map of task types to prompt IDs
func (v *ConfigValidator) getTaskTypeToPromptMapping() map[string][]string {
	taskTypeToPrompts := make(map[string][]string)
	for id, pc := range v.config.LoadedPromptConfigs {
		if pc != nil && pc.Config != nil {
			if promptCfg, ok := pc.Config.(interface{ GetTaskType() string }); ok {
				taskType := promptCfg.GetTaskType()
				if taskType != "" {
					taskTypeToPrompts[taskType] = append(taskTypeToPrompts[taskType], id)
				}
			}
		}
	}
	return taskTypeToPrompts
}

// getUsedTaskTypes returns task types referenced by scenarios
func (v *ConfigValidator) getUsedTaskTypes() map[string]bool {
	usedTaskTypes := make(map[string]bool)
	for _, scenario := range v.config.LoadedScenarios {
		if scenario != nil && scenario.TaskType != "" {
			usedTaskTypes[scenario.TaskType] = true
		}
	}
	return usedTaskTypes
}

// checkDuplicateTaskTypes checks for multiple prompts with the same task type
func (v *ConfigValidator) checkDuplicateTaskTypes(taskTypeToPrompts map[string][]string) {
	check := ValidationCheck{
		Name:   "Unique task types per prompt",
		Passed: true,
	}
	for taskType, prompts := range taskTypeToPrompts {
		if len(prompts) > 1 {
			check.Passed = false
			issue := fmt.Sprintf("task_type '%s' is defined by multiple prompts: %v", taskType, prompts)
			check.Issues = append(check.Issues, issue)
		}
	}
	v.checks = append(v.checks, check)
}

// checkUnusedTaskTypes checks for prompt task types not used in any scenario
func (v *ConfigValidator) checkUnusedTaskTypes(taskTypeToPrompts map[string][]string, usedTaskTypes map[string]bool) {
	check := ValidationCheck{
		Name:    "Prompt task types used in scenarios",
		Passed:  true,
		Warning: true,
	}
	for taskType, prompts := range taskTypeToPrompts {
		if !usedTaskTypes[taskType] {
			check.Passed = false
			issue := fmt.Sprintf("task_type '%s' (from %v) is not used by any scenario", taskType, prompts)
			check.Issues = append(check.Issues, issue)
		}
	}
	if len(check.Issues) > 0 {
		v.checks = append(v.checks, check)
	}
}

// checkMissingTaskTypes checks for scenarios referencing non-existent task types
func (v *ConfigValidator) checkMissingTaskTypes(taskTypeToPrompts map[string][]string, usedTaskTypes map[string]bool) {
	check := ValidationCheck{
		Name:   "Scenario task types exist",
		Passed: true,
	}
	for taskType := range usedTaskTypes {
		if _, exists := taskTypeToPrompts[taskType]; !exists {
			check.Passed = false
			issue := fmt.Sprintf("scenario references task_type '%s' which has no matching prompt", taskType)
			check.Issues = append(check.Issues, issue)
		}
	}
	v.checks = append(v.checks, check)
}

// GetChecks returns the structured validation checks
func (v *ConfigValidator) GetChecks() []ValidationCheck {
	return v.checks
}
