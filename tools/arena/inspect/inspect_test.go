package inspect_test

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// minimalCfg returns a *config.Config with enough populated fields to exercise
// most of the collector and renderer paths without loading files from disk.
func minimalCfg() *config.Config {
	return &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			"chat": {Config: nil}, // Config nil — populatePromptDetails returns early
		},
		LoadedProviders: map[string]*config.Provider{
			"mock-prov": {
				ID:    "mock-prov",
				Type:  "mock",
				Model: "test-model",
				Defaults: config.ProviderDefaults{
					Temperature: 0.7,
					MaxTokens:   1024,
				},
			},
		},
		LoadedScenarios: map[string]*config.Scenario{
			"s1": {
				ID:          "s1",
				TaskType:    "chat",
				Description: "a test scenario",
				Mode:        "chat",
				Turns: []config.TurnDefinition{
					{Assertions: []config.AssertionConfig{{}}},
					{Persona: "alice"},
				},
				Providers: []string{"mock-prov"},
				Streaming: true,
			},
		},
		LoadedPersonas: map[string]*config.UserPersonaPack{
			"alice": {
				Description: "Friendly persona",
				Goals:       []string{"goal1", "goal2", "goal3", "goal4"},
			},
		},
		LoadedJudges: map[string]*config.JudgeTarget{},
		LoadedTools:  []config.ToolData{},
		Providers: []config.ProviderRef{
			{File: "mock-prov.yaml", Group: "default"},
		},
		Scenarios: []config.ScenarioRef{
			{File: "s1.yaml"},
		},
		PromptConfigs: []config.PromptConfigRef{
			{ID: "chat", File: "chat.yaml", Vars: map[string]string{"k": "v"}},
		},
		Judges: []config.JudgeRef{},
		Defaults: config.Defaults{
			Temperature: 0.5,
			MaxTokens:   2048,
			Concurrency: 4,
		},
	}
}

// TestCollectInspectionData_FullConfig exercises CollectInspectionData with a
// populated config struct.
func TestCollectInspectionData_FullConfig(t *testing.T) {
	cfg := minimalCfg()
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	if data == nil {
		t.Fatal("CollectInspectionData returned nil")
	}
	if data.ConfigFile != "arena.yaml" {
		t.Errorf("ConfigFile = %q, want %q", data.ConfigFile, "arena.yaml")
	}
	if len(data.AvailableTaskTypes) == 0 {
		t.Error("expected AvailableTaskTypes to be populated")
	}
	if data.Defaults == nil {
		t.Error("expected Defaults to be set")
	}
	if data.Defaults.Temperature != 0.5 {
		t.Errorf("Defaults.Temperature = %v, want 0.5", data.Defaults.Temperature)
	}
	if len(data.Personas) == 0 {
		t.Error("expected Personas to be collected")
	}
}

// TestCollectInspectionData_SelfPlay exercises the self-play collection path.
func TestCollectInspectionData_SelfPlay(t *testing.T) {
	cfg := minimalCfg()
	cfg.SelfPlay = &config.SelfPlayConfig{
		Roles: []config.SelfPlayRoleGroup{
			{ID: "assistant", Provider: "mock-prov"},
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	if !data.SelfPlayEnabled {
		t.Error("expected SelfPlayEnabled to be true")
	}
	if len(data.SelfPlayRoles) == 0 {
		t.Error("expected SelfPlayRoles to be collected")
	}
}

// TestCollectCacheStats exercises CollectCacheStats.
func TestCollectCacheStats(t *testing.T) {
	cfg := minimalCfg()
	stats := inspect.CollectCacheStats(cfg, false)
	if stats == nil {
		t.Fatal("CollectCacheStats returned nil")
	}
	if stats.PromptCache.Size != 1 {
		t.Errorf("PromptCache.Size = %d, want 1", stats.PromptCache.Size)
	}
}

// TestCollectCacheStats_WithSelfPlay exercises the self-play path.
func TestCollectCacheStats_WithSelfPlay(t *testing.T) {
	cfg := minimalCfg()
	cfg.SelfPlay = &config.SelfPlayConfig{
		Roles: []config.SelfPlayRoleGroup{
			{ID: "assistant", Provider: "mock-prov"},
		},
	}
	stats := inspect.CollectCacheStats(cfg, false)
	if stats == nil {
		t.Fatal("CollectCacheStats returned nil")
	}
	if stats.SelfPlayCache.Size == 0 {
		t.Error("expected SelfPlayCache.Size > 0 with self-play enabled")
	}
}

// TestCollectCacheStats_Empty exercises empty config.
func TestCollectCacheStats_Empty(t *testing.T) {
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedPersonas:      map[string]*config.UserPersonaPack{},
	}
	stats := inspect.CollectCacheStats(cfg, false)
	if stats == nil {
		t.Fatal("CollectCacheStats returned nil")
	}
	if stats.PromptCache.Size != 0 {
		t.Errorf("expected empty prompt cache, got %d", stats.PromptCache.Size)
	}
}

// TestCollectConnectivityChecks exercises the connectivity checks.
func TestCollectConnectivityChecks(t *testing.T) {
	data := &inspect.InspectionData{
		Tools: []inspect.ToolInspectData{
			{Name: "my-tool", File: "my-tool.yaml"},
		},
		PromptConfigs: []inspect.PromptInspectData{
			{ID: "p1", AllowedTools: []string{"my-tool"}},
		},
	}
	checks := inspect.CollectConnectivityChecks(data)
	if len(checks) == 0 {
		t.Error("expected connectivity checks to be returned")
	}
	for _, c := range checks {
		if c.Name == "Tools connected to prompts" && !c.Passed {
			t.Errorf("expected 'Tools connected to prompts' to pass, issues: %v", c.Issues)
		}
		if c.Name == "Allowed tools are defined" && !c.Passed {
			t.Errorf("expected 'Allowed tools are defined' to pass, issues: %v", c.Issues)
		}
	}
}

// TestCollectConnectivityChecks_UnusedTool verifies warning when tool defined
// but not allowed on any prompt.
func TestCollectConnectivityChecks_UnusedTool(t *testing.T) {
	data := &inspect.InspectionData{
		Tools: []inspect.ToolInspectData{
			{Name: "unused-tool", File: "unused.yaml"},
		},
		PromptConfigs: []inspect.PromptInspectData{},
	}
	checks := inspect.CollectConnectivityChecks(data)
	var found bool
	for _, c := range checks {
		if c.Name == "Tools connected to prompts" && !c.Passed && c.Warning {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for unused tool")
	}
}

// TestCollectConnectivityChecks_MissingTool verifies error when prompt allows
// a tool that is not defined.
func TestCollectConnectivityChecks_MissingTool(t *testing.T) {
	data := &inspect.InspectionData{
		Tools: []inspect.ToolInspectData{},
		PromptConfigs: []inspect.PromptInspectData{
			{ID: "p1", AllowedTools: []string{"ghost-tool"}},
		},
	}
	checks := inspect.CollectConnectivityChecks(data)
	var found bool
	for _, c := range checks {
		if c.Name == "Allowed tools are defined" && !c.Passed && !c.Warning {
			found = true
		}
	}
	if !found {
		t.Error("expected error for missing tool definition")
	}
}

// TestCollectConnectivityChecks_SelfPlayEnabled exercises the self-play branch.
func TestCollectConnectivityChecks_SelfPlayEnabled(t *testing.T) {
	data := &inspect.InspectionData{
		SelfPlayEnabled: true,
		SelfPlayRoles: []inspect.SelfPlayRoleData{
			{ID: "user", Provider: "prov"},
		},
	}
	checks := inspect.CollectConnectivityChecks(data)
	var found bool
	for _, c := range checks {
		if c.Name == "Self-play roles have valid providers" {
			found = true
		}
	}
	if !found {
		t.Error("expected self-play providers check to be present")
	}
}

// TestRenderText_NilConfig verifies a minimal InspectionData renders without panic.
func TestRenderText_NilConfig(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile:       "test.yaml",
		ValidationPassed: true,
	}
	out := inspect.RenderText(data, inspect.RenderOptions{})
	if !strings.Contains(out, "Validation") {
		t.Errorf("RenderText output missing Validation section; got:\n%s", out)
	}
}

// TestRenderText_FullData exercises most render paths with populated sections.
func TestRenderText_FullData(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile: "example.yaml",
		PromptConfigs: []inspect.PromptInspectData{
			{
				ID:           "p1",
				File:         "p1.yaml",
				TaskType:     "chat",
				Description:  "A prompt",
				Version:      "1.0",
				Variables:    []string{"name"},
				AllowedTools: []string{"search"},
				Validators:   []string{"json"},
				HasMedia:     true,
				VariableValues: map[string]string{
					"name": "World",
				},
			},
		},
		Providers: []inspect.ProviderInspectData{
			{
				ID: "prov1", File: "prov1.yaml", Type: "openai", Model: "gpt-4",
				Group: "main", Temperature: 0.7, MaxTokens: 1024,
			},
		},
		Scenarios: []inspect.ScenarioInspectData{
			{
				ID:                 "sc1",
				File:               "sc1.yaml",
				TaskType:           "chat",
				Mode:               "chat",
				TurnCount:          2,
				Description:        "test scenario",
				HasSelfPlay:        true,
				Streaming:          true,
				AssertionCount:     3,
				ConvAssertionCount: 1,
				Providers:          []string{"prov1"},
			},
		},
		Tools: []inspect.ToolInspectData{
			{
				Name:          "search",
				File:          "search.yaml",
				Description:   "search tool",
				Mode:          "live",
				TimeoutMs:     5000,
				InputParams:   []string{"query"},
				HasMockData:   true,
				HasHTTPConfig: true,
			},
		},
		Personas: []inspect.PersonaInspectData{
			{
				ID:          "alice",
				Description: "Friendly user",
				Goals:       []string{"g1", "g2", "g3", "g4", "g5"},
			},
		},
		SelfPlayRoles: []inspect.SelfPlayRoleData{
			{ID: "user", Persona: "alice", Provider: "prov1"},
		},
		Judges: []inspect.JudgeInspectData{
			{Name: "quality", Provider: "prov1", Model: "gpt-4"},
		},
		Defaults: &inspect.DefaultsInspectData{
			Temperature:   0.7,
			MaxTokens:     2048,
			Seed:          42,
			Concurrency:   4,
			OutputDir:     "out/",
			OutputFormats: []string{"html", "json"},
		},
		ValidationPassed: false,
		ValidationErrors: []string{"missing required field"},
		ValidationChecks: []inspect.ValidationCheckData{
			{Name: "check1", Passed: true},
			{Name: "check2", Passed: false, Warning: true, Issues: []string{"warn issue"}},
			{Name: "check3", Passed: false, Issues: []string{"error issue"}},
		},
		ValidationWarningDetails: []string{
			"no personas defined",   // filtered out
			"custom warning detail", // kept
		},
	}

	out := inspect.RenderText(data, inspect.RenderOptions{Verbose: true})
	if out == "" {
		t.Fatal("RenderText returned empty string")
	}

	for _, want := range []string{"p1", "prov1", "sc1", "search", "alice", "quality", "Validation"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderText missing %q", want)
		}
	}
}

// TestRenderText_ToolModes exercises mock/live/custom tool mode display paths.
func TestRenderText_ToolModes(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile: "x.yaml",
		Tools: []inspect.ToolInspectData{
			{Name: "t1", File: "t1.yaml", Mode: "mock"},
			{Name: "t2", File: "t2.yaml", Mode: "live"},
			{Name: "t3", File: "t3.yaml", Mode: "custom"},
		},
		ValidationPassed: true,
	}
	out := inspect.RenderText(data, inspect.RenderOptions{})
	if out == "" {
		t.Fatal("RenderText returned empty string")
	}
}

// TestRenderText_CollectAndRender collects from a real config and renders.
func TestRenderText_CollectAndRender(t *testing.T) {
	cfg := minimalCfg()
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	data.ValidationPassed = true
	out := inspect.RenderText(data, inspect.RenderOptions{})
	if !strings.Contains(out, "Validation") {
		t.Errorf("output missing Validation section; got:\n%s", out)
	}
}

// TestCollectInspectionData_WithTools exercises the tool collection + parseToolManifest path.
func TestCollectInspectionData_WithTools(t *testing.T) {
	toolYAML := []byte(`
metadata:
  name: my-tool
spec:
  description: does stuff
  mode: mock
  timeout_ms: 3000
  input_schema:
    properties:
      query:
        type: string
  mock_result: "ok"
  http:
    url: https://example.com
`)
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders:     map[string]*config.Provider{},
		LoadedScenarios:     map[string]*config.Scenario{},
		LoadedPersonas:      map[string]*config.UserPersonaPack{},
		LoadedJudges:        map[string]*config.JudgeTarget{},
		LoadedTools: []config.ToolData{
			{FilePath: "my-tool.yaml", Data: toolYAML},
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	if len(data.Tools) == 0 {
		t.Fatal("expected tools to be collected")
	}
	if data.Tools[0].Name != "my-tool" {
		t.Errorf("Tool.Name = %q, want %q", data.Tools[0].Name, "my-tool")
	}
	if !data.Tools[0].HasMockData {
		t.Error("expected HasMockData = true")
	}
	if !data.Tools[0].HasHTTPConfig {
		t.Error("expected HasHTTPConfig = true")
	}
}

// TestCollectInspectionData_WithJudges exercises the judge collection path
// including the judge-provider enrichment path.
func TestCollectInspectionData_WithJudges(t *testing.T) {
	judgeProvider := &config.Provider{
		ID:    "judge-prov",
		Type:  "openai",
		Model: "gpt-4o",
	}
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders: map[string]*config.Provider{
			"judge-prov": judgeProvider,
		},
		LoadedScenarios: map[string]*config.Scenario{},
		LoadedPersonas:  map[string]*config.UserPersonaPack{},
		LoadedJudges: map[string]*config.JudgeTarget{
			"quality": {
				Name:     "quality",
				Provider: judgeProvider,
			},
		},
		LoadedTools: []config.ToolData{},
		Judges: []config.JudgeRef{
			{Name: "quality", Provider: "judge-prov"},
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	if len(data.Judges) == 0 {
		t.Fatal("expected judges to be collected")
	}
	if data.Judges[0].Model != "gpt-4o" {
		t.Errorf("Judge.Model = %q, want gpt-4o", data.Judges[0].Model)
	}
}

// TestCollectInspectionData_JudgeProviderNotInMainList exercises appendJudgeProviders
// when a judge's provider is not already in the provider list.
func TestCollectInspectionData_JudgeProviderNotInMainList(t *testing.T) {
	judgeProvider := &config.Provider{
		ID:    "separate-judge-prov",
		Type:  "openai",
		Model: "gpt-4",
		Defaults: config.ProviderDefaults{
			Temperature: 0.3,
		},
	}
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders: map[string]*config.Provider{
			"separate-judge-prov": judgeProvider,
		},
		LoadedScenarios: map[string]*config.Scenario{},
		LoadedPersonas:  map[string]*config.UserPersonaPack{},
		LoadedJudges: map[string]*config.JudgeTarget{
			"quality": {Name: "quality", Provider: judgeProvider},
		},
		LoadedTools: []config.ToolData{},
		// No Providers file refs — so appendJudgeProviders must add it
		Providers: []config.ProviderRef{},
		Judges: []config.JudgeRef{
			{Name: "quality", Provider: "separate-judge-prov"},
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	// Provider should have been added via appendJudgeProviders
	found := false
	for _, p := range data.Providers {
		if p.ID == "separate-judge-prov" {
			found = true
		}
	}
	if !found {
		t.Error("expected judge provider to be added to providers list")
	}
}

// TestCollectInspectionData_SelfPlayProviderUsage exercises buildProviderUsageMap
// with both judge and self-play role usage.
func TestCollectInspectionData_SelfPlayProviderUsage(t *testing.T) {
	prov := &config.Provider{
		ID:    "shared-prov",
		Type:  "mock",
		Model: "test",
	}
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders:     map[string]*config.Provider{"shared-prov": prov},
		LoadedScenarios:     map[string]*config.Scenario{},
		LoadedPersonas: map[string]*config.UserPersonaPack{
			"bob": {Description: "Bob"},
		},
		LoadedJudges: map[string]*config.JudgeTarget{},
		LoadedTools:  []config.ToolData{},
		Providers:    []config.ProviderRef{{File: "shared-prov.yaml"}},
		Judges:       []config.JudgeRef{{Name: "quality", Provider: "shared-prov"}},
		SelfPlay: &config.SelfPlayConfig{
			Roles: []config.SelfPlayRoleGroup{
				{ID: "user", Provider: "shared-prov"},
			},
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	// Verify the shared provider shows up with both usages
	for _, p := range data.Providers {
		if p.ID == "shared-prov" && len(p.UsedBy) > 0 {
			return
		}
	}
	t.Error("expected shared-prov to have UsedBy entries")
}

// TestCollectInspectionData_ToolEmptyData exercises parseToolManifest with empty data.
func TestCollectInspectionData_ToolEmptyData(t *testing.T) {
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders:     map[string]*config.Provider{},
		LoadedScenarios:     map[string]*config.Scenario{},
		LoadedPersonas:      map[string]*config.UserPersonaPack{},
		LoadedJudges:        map[string]*config.JudgeTarget{},
		LoadedTools: []config.ToolData{
			{FilePath: "empty-tool.yaml", Data: []byte{}}, // empty
		},
	}
	data := inspect.CollectInspectionData(cfg, "/tmp/arena.yaml")
	// Should not panic; tool with no data gets a stub entry
	if len(data.Tools) == 0 {
		t.Error("expected empty-data tool to still produce a ToolInspectData entry")
	}
}

// TestRenderText_VerboseDiffers verifies that verbose mode includes extra detail
// lines absent in the default non-verbose output.
func TestRenderText_VerboseDiffers(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile: "x.yaml",
		PromptConfigs: []inspect.PromptInspectData{
			{
				ID:          "p1",
				File:        "p1.yaml",
				Description: "verbose-only-description",
				Version:     "1.0",
				Variables:   []string{"name"},
			},
		},
		ValidationPassed: true,
	}
	nonVerbose := inspect.RenderText(data, inspect.RenderOptions{})
	verbose := inspect.RenderText(data, inspect.RenderOptions{Verbose: true})

	if strings.Contains(nonVerbose, "verbose-only-description") {
		t.Error("non-verbose output should NOT contain verbose-only description")
	}
	if !strings.Contains(verbose, "verbose-only-description") {
		t.Error("verbose output MUST contain description")
	}
}

// TestRenderText_SectionFilter verifies that --section only shows the requested
// section and suppresses others.
func TestRenderText_SectionFilter(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile: "x.yaml",
		PromptConfigs: []inspect.PromptInspectData{
			{ID: "p1", File: "p1.yaml"},
		},
		Providers: []inspect.ProviderInspectData{
			{ID: "prov1", File: "prov1.yaml"},
		},
		ValidationPassed: true,
	}
	out := inspect.RenderText(data, inspect.RenderOptions{Section: "providers"})

	if !strings.Contains(out, "Providers") {
		t.Error("section=providers output must contain Providers header")
	}
	if strings.Contains(out, "Prompt Configs") {
		t.Error("section=providers output must NOT contain Prompt Configs header")
	}
}

// TestRenderText_StatsSection verifies that --stats includes the cache statistics
// section when CacheStats is non-nil, and omits it otherwise.
func TestRenderText_StatsSection(t *testing.T) {
	data := &inspect.InspectionData{
		ConfigFile:       "x.yaml",
		ValidationPassed: true,
		CacheStats: &inspect.CacheStatsData{
			PromptCache: inspect.CacheInfo{Size: 2},
		},
	}
	withStats := inspect.RenderText(data, inspect.RenderOptions{Stats: true})
	withoutStats := inspect.RenderText(data, inspect.RenderOptions{Stats: false})

	if !strings.Contains(withStats, "Cache Statistics") {
		t.Error("Stats=true output must contain Cache Statistics header")
	}
	if strings.Contains(withoutStats, "Cache Statistics") {
		t.Error("Stats=false output must NOT contain Cache Statistics header")
	}
}
