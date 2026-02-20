package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

func TestConfig_JSONRoundTrip(t *testing.T) {
	original := Config{
		PromptConfigs: []PromptConfigRef{
			{ID: "pc1", File: "prompts/main.yaml", Vars: map[string]string{"env": "prod"}},
		},
		Providers: []ProviderRef{
			{File: "providers/openai.yaml", Group: "default"},
		},
		Judges: []JudgeRef{
			{Name: "quality", Provider: "openai", Model: "gpt-4"},
		},
		JudgeDefaults: &JudgeDefaults{
			Prompt:         "default-judge",
			PromptRegistry: "builtin",
		},
		Scenarios: []ScenarioRef{{File: "scenarios/basic.yaml"}},
		Evals:     []EvalRef{{File: "evals/e1.yaml"}},
		Tools:     []ToolRef{{File: "tools/t1.yaml"}},
		MCPServers: []MCPServerConfig{
			{Name: "mcp1", Command: "npx", Args: []string{"-y", "server"}, Env: map[string]string{"KEY": "val"}},
		},
		A2AAgents: []A2AAgentConfig{
			{
				Name: "agent1",
				Card: A2ACardConfig{
					Name:        "Agent One",
					Description: "A test agent",
					Skills:      []A2ASkillConfig{{ID: "s1", Name: "Skill1", Description: "desc"}},
				},
				Responses: []A2AResponseRule{
					{
						Skill:    "s1",
						Match:    &A2AMatchConfig{Contains: "hello"},
						Response: &A2AResponseConfig{Parts: []A2APartConfig{{Text: "hi"}}},
					},
				},
			},
		},
		StateStore: &StateStoreConfig{
			Type:  "redis",
			Redis: &RedisConfig{Address: "localhost:6379", Password: "secret", Database: 1, TTL: "24h", Prefix: "pk"},
		},
		Defaults: Defaults{
			Temperature: 0.7,
			MaxTokens:   1024,
			Seed:        42,
			Concurrency: 4,
			ConfigDir:   "/tmp/configs",
			FailOn:      []string{"error"},
			Verbose:     true,
		},
		SelfPlay: &SelfPlayConfig{
			Enabled:  true,
			Personas: []PersonaRef{{File: "personas/p1.yaml"}},
			Roles:    []SelfPlayRoleGroup{{ID: "r1", Provider: "openai"}},
		},
		PackFile: "pack.json",
		// Inline specs
		ProviderSpecs: map[string]*Provider{
			"inline-p": {ID: "inline-p", Type: "openai", Model: "gpt-4o"},
		},
		ScenarioSpecs: map[string]*Scenario{
			"inline-s": {ID: "inline-s", TaskType: "chat", Description: "Inline scenario"},
		},
		EvalSpecs: map[string]*Eval{
			"inline-e": {ID: "inline-e", Description: "Inline eval"},
		},
		ToolSpecs: map[string]*ToolSpec{
			"inline-t": {Name: "inline-t", Description: "Inline tool", Mode: "mock"},
		},
		JudgeSpecs: map[string]*JudgeSpec{
			"inline-j": {Provider: "openai", Model: "gpt-4o-mini"},
		},
		PromptSpecs: map[string]*prompt.Spec{
			"chat": {TaskType: "chat", SystemTemplate: "Hello"},
		},
		Deploy: &DeployConfig{
			Provider: "agentcore",
			Config:   map[string]interface{}{"region": "us-east-1"},
		},
		// Loaded resources
		ProviderGroups:       map[string]string{"openai": "default"},
		ProviderCapabilities: map[string][]string{"openai": {"text", "streaming"}},
		LoadedProviders: map[string]*Provider{
			"openai": {ID: "openai", Type: "openai", Model: "gpt-4"},
		},
		LoadedJudges: map[string]*JudgeTarget{
			"quality": {Name: "quality", Model: "gpt-4"},
		},
		LoadedTools: []ToolData{
			{FilePath: "tools/t1.yaml", Data: []byte(`{"name":"tool1"}`)},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(&original)
	require.NoError(t, err)

	// Unmarshal back
	var restored Config
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify top-level ref fields
	assert.Equal(t, original.PromptConfigs, restored.PromptConfigs)
	assert.Equal(t, original.Providers, restored.Providers)
	assert.Equal(t, original.Judges, restored.Judges)
	assert.Equal(t, original.JudgeDefaults, restored.JudgeDefaults)
	assert.Equal(t, original.Scenarios, restored.Scenarios)
	assert.Equal(t, original.Evals, restored.Evals)
	assert.Equal(t, original.Tools, restored.Tools)
	assert.Equal(t, original.MCPServers, restored.MCPServers)
	assert.Equal(t, original.PackFile, restored.PackFile)
	assert.Equal(t, original.Deploy.Provider, restored.Deploy.Provider)

	// Verify loaded resources survive round-trip
	assert.Equal(t, original.ProviderGroups, restored.ProviderGroups)
	assert.Equal(t, original.ProviderCapabilities, restored.ProviderCapabilities)
	require.NotNil(t, restored.LoadedProviders["openai"])
	assert.Equal(t, "gpt-4", restored.LoadedProviders["openai"].Model)
	require.NotNil(t, restored.LoadedJudges["quality"])
	assert.Equal(t, "gpt-4", restored.LoadedJudges["quality"].Model)
	require.Len(t, restored.LoadedTools, 1)
	assert.Equal(t, "tools/t1.yaml", restored.LoadedTools[0].FilePath)

	// Verify defaults round-trip
	assert.InDelta(t, float32(0.7), restored.Defaults.Temperature, 0.001)
	assert.Equal(t, 1024, restored.Defaults.MaxTokens)
	assert.Equal(t, 42, restored.Defaults.Seed)

	// Verify inline specs round-trip
	require.NotNil(t, restored.ProviderSpecs["inline-p"])
	assert.Equal(t, "gpt-4o", restored.ProviderSpecs["inline-p"].Model)
	require.NotNil(t, restored.ScenarioSpecs["inline-s"])
	assert.Equal(t, "chat", restored.ScenarioSpecs["inline-s"].TaskType)
	require.NotNil(t, restored.EvalSpecs["inline-e"])
	assert.Equal(t, "Inline eval", restored.EvalSpecs["inline-e"].Description)
	require.NotNil(t, restored.ToolSpecs["inline-t"])
	assert.Equal(t, "mock", restored.ToolSpecs["inline-t"].Mode)
	require.NotNil(t, restored.JudgeSpecs["inline-j"])
	assert.Equal(t, "openai", restored.JudgeSpecs["inline-j"].Provider)
	require.NotNil(t, restored.PromptSpecs["chat"])
	assert.Equal(t, "Hello", restored.PromptSpecs["chat"].SystemTemplate)
}

func TestConfig_JSONExcludesTransientFields(t *testing.T) {
	cfg := Config{
		Providers:      []ProviderRef{{File: "p.yaml"}},
		SkipPackEvals:  true,
		EvalTypeFilter: []string{"safety"},
		ConfigDir:      "/some/dir",
	}

	data, err := json.Marshal(&cfg)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "skip_pack_evals")
	assert.NotContains(t, jsonStr, "eval_type_filter")
	assert.NotContains(t, jsonStr, "config_dir")
	// But providers should be present
	assert.Contains(t, jsonStr, "providers")
}

func TestConfig_JSONFieldNamesMatchYAML(t *testing.T) {
	// Verify that for all fields with both yaml and json tags, the base names match
	configType := reflect.TypeOf(Config{})

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		yamlTag := field.Tag.Get("yaml")
		jsonTag := field.Tag.Get("json")

		if yamlTag == "" || jsonTag == "" {
			continue
		}
		if yamlTag == "-" || jsonTag == "-" {
			continue
		}

		yamlName := strings.Split(yamlTag, ",")[0]
		jsonName := strings.Split(jsonTag, ",")[0]

		// Fields with yaml:"-" that have json names are loaded-only fields — skip comparison
		if yamlName == "-" {
			continue
		}

		assert.Equal(t, yamlName, jsonName,
			"field %s: yaml tag %q and json tag %q have different base names",
			field.Name, yamlTag, jsonTag)
	}
}
