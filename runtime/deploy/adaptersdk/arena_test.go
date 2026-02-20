package adaptersdk

import (
	"encoding/json"
	"testing"
)

func TestExtractToolInfo_FromToolSpecs(t *testing.T) {
	arenaJSON := `{
		"tool_specs": {
			"get_weather": {
				"description": "Get weather forecast",
				"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}},
				"mode": "live",
				"http": {"url": "https://api.weather.com/v1", "method": "POST"}
			},
			"calculate": {
				"description": "Perform calculations",
				"input_schema": {"type": "object"},
				"mode": "mock"
			}
		}
	}`
	tools, err := ExtractToolInfo(arenaJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Tools should be sorted alphabetically
	if tools[0].Name != "calculate" {
		t.Errorf("expected first tool calculate, got %s", tools[0].Name)
	}
	if tools[1].Name != "get_weather" {
		t.Errorf("expected second tool get_weather, got %s", tools[1].Name)
	}

	// Check get_weather details
	weather := tools[1]
	if weather.Description != "Get weather forecast" {
		t.Errorf("unexpected description: %s", weather.Description)
	}
	if weather.Mode != "live" {
		t.Errorf("unexpected mode: %s", weather.Mode)
	}
	if !weather.HasSchema {
		t.Error("expected HasSchema to be true")
	}
	if weather.HTTPURL != "https://api.weather.com/v1" {
		t.Errorf("unexpected HTTP URL: %s", weather.HTTPURL)
	}
	if weather.HTTPMethod != "POST" {
		t.Errorf("unexpected HTTP method: %s", weather.HTTPMethod)
	}

	// Check calculate details
	calc := tools[0]
	if calc.Mode != "mock" {
		t.Errorf("unexpected mode: %s", calc.Mode)
	}
	if calc.HTTPURL != "" {
		t.Errorf("expected empty HTTP URL, got %s", calc.HTTPURL)
	}
}

func TestExtractToolInfo_FromLoadedTools(t *testing.T) {
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: search_docs
spec:
  name: search_docs
  description: Search documentation
  input_schema:
    type: object
    properties:
      query:
        type: string
  mode: live
  http:
    url: https://api.docs.com/search
    method: GET`

	arenaJSON, _ := json.Marshal(map[string]interface{}{
		"loaded_tools": []map[string]interface{}{
			{
				"file_path": "tools/search.tool.yaml",
				"data":      []byte(toolYAML),
			},
		},
	})

	tools, err := ExtractToolInfo(string(arenaJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "search_docs" {
		t.Errorf("expected name search_docs, got %s", tools[0].Name)
	}
	if tools[0].Description != "Search documentation" {
		t.Errorf("unexpected description: %s", tools[0].Description)
	}
	if tools[0].Mode != "live" {
		t.Errorf("unexpected mode: %s", tools[0].Mode)
	}
	if !tools[0].HasSchema {
		t.Error("expected HasSchema to be true")
	}
	if tools[0].HTTPURL != "https://api.docs.com/search" {
		t.Errorf("unexpected HTTP URL: %s", tools[0].HTTPURL)
	}
}

func TestExtractToolInfo_Empty(t *testing.T) {
	tools, err := ExtractToolInfo("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tools != nil {
		t.Errorf("expected nil, got %v", tools)
	}
}

func TestExtractToolInfo_InvalidJSON(t *testing.T) {
	_, err := ExtractToolInfo("{invalid}")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractToolInfo_Deduplication(t *testing.T) {
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_weather
spec:
  name: get_weather
  description: From loaded tools
  mode: mock`

	// Build JSON with both inline spec and loaded tool for same name
	arenaJSON, _ := json.Marshal(map[string]interface{}{
		"tool_specs": map[string]interface{}{
			"get_weather": map[string]interface{}{
				"description":  "From inline spec",
				"input_schema": nil,
				"mode":         "live",
			},
		},
		"loaded_tools": []map[string]interface{}{
			{
				"file_path": "tools/weather.tool.yaml",
				"data":      []byte(toolYAML),
			},
		},
	})

	tools, err := ExtractToolInfo(string(arenaJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (deduplicated), got %d", len(tools))
	}
	// Inline spec takes precedence
	if tools[0].Description != "From inline spec" {
		t.Errorf("expected inline spec description, got %s", tools[0].Description)
	}
	if tools[0].Mode != "live" {
		t.Errorf("expected inline spec mode, got %s", tools[0].Mode)
	}
}

func TestExtractToolInfo_MetadataNameFallback(t *testing.T) {
	// Tool with name only in metadata, not in spec
	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: from_metadata
spec:
  description: Uses metadata name
  mode: mock`

	arenaJSON, _ := json.Marshal(map[string]interface{}{
		"loaded_tools": []map[string]interface{}{
			{
				"data": []byte(toolYAML),
			},
		},
	})

	tools, err := ExtractToolInfo(string(arenaJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "from_metadata" {
		t.Errorf("expected name from_metadata, got %s", tools[0].Name)
	}
}

func TestExtractToolInfo_SkipsInvalidToolData(t *testing.T) {
	arenaJSON, _ := json.Marshal(map[string]interface{}{
		"loaded_tools": []map[string]interface{}{
			{"data": []byte("not: valid: yaml: [")},
			{"file_path": "empty.yaml"}, // no data
		},
		"tool_specs": map[string]interface{}{
			"valid_tool": map[string]interface{}{
				"description": "A valid tool",
				"mode":        "mock",
			},
		},
	})

	tools, err := ExtractToolInfo(string(arenaJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (skipped invalid), got %d", len(tools))
	}
	if tools[0].Name != "valid_tool" {
		t.Errorf("expected valid_tool, got %s", tools[0].Name)
	}
}

func TestExtractToolPolicies_BlocklistMerge(t *testing.T) {
	arenaJSON := `{
		"loaded_scenarios": {
			"scenario1": {
				"tool_policy": {
					"blocklist": ["dangerous_tool", "admin_tool"]
				}
			},
			"scenario2": {
				"tool_policy": {
					"blocklist": ["admin_tool", "debug_tool"]
				}
			}
		}
	}`
	policy, err := ExtractToolPolicies(arenaJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
	// Merged and sorted: admin_tool, dangerous_tool, debug_tool
	if len(policy.Blocklist) != 3 {
		t.Fatalf("expected 3 blocklist entries, got %d", len(policy.Blocklist))
	}
	expected := []string{"admin_tool", "dangerous_tool", "debug_tool"}
	for i, e := range expected {
		if policy.Blocklist[i] != e {
			t.Errorf("blocklist[%d]: expected %s, got %s", i, e, policy.Blocklist[i])
		}
	}
}

func TestExtractToolPolicies_NoPolicies(t *testing.T) {
	arenaJSON := `{
		"loaded_scenarios": {
			"scenario1": {},
			"scenario2": {"tool_policy": {}}
		}
	}`
	policy, err := ExtractToolPolicies(arenaJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != nil {
		t.Errorf("expected nil policy, got %v", policy)
	}
}

func TestExtractToolPolicies_Empty(t *testing.T) {
	policy, err := ExtractToolPolicies("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != nil {
		t.Errorf("expected nil, got %v", policy)
	}
}

func TestExtractToolPolicies_InvalidJSON(t *testing.T) {
	_, err := ExtractToolPolicies("{bad}")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIntegration_EndToEndToolGatewayPlanning(t *testing.T) {
	// Build ArenaConfig with tools
	arenaJSON := `{
		"tool_specs": {
			"get_weather": {
				"description": "Get weather",
				"input_schema": {"type": "object"},
				"mode": "live",
				"http": {"url": "https://api.weather.com", "method": "POST"}
			},
			"search": {
				"description": "Search docs",
				"input_schema": {"type": "object"},
				"mode": "live"
			},
			"mock_tool": {
				"description": "A mock tool",
				"mode": "mock"
			}
		}
	}`

	// Build deploy config with tool targets (only for some tools)
	deployJSON := `{
		"provider": "agentcore",
		"tool_targets": {
			"get_weather": {"lambda_arn": "arn:aws:lambda:us-east-1:123:function:weather"},
			"search": {"lambda_arn": "arn:aws:lambda:us-east-1:123:function:search"}
		}
	}`

	// Step 1: Extract tool info
	tools, err := ExtractToolInfo(arenaJSON)
	if err != nil {
		t.Fatalf("ExtractToolInfo error: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Step 2: Parse tool targets
	targets, err := ParseDeployToolTargets(deployJSON)
	if err != nil {
		t.Fatalf("ParseDeployToolTargets error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	// Step 3: Generate tool gateway plan
	changes := GenerateToolGatewayPlan(tools, targets)
	if len(changes) != 2 {
		t.Fatalf("expected 2 tool_gateway changes, got %d", len(changes))
	}

	// Verify: only get_weather and search get gateway resources (not mock_tool)
	names := map[string]bool{}
	for _, c := range changes {
		names[c.Name] = true
		if c.Type != "tool_gateway" {
			t.Errorf("expected type tool_gateway, got %s", c.Type)
		}
	}
	if !names["get_weather"] {
		t.Error("expected get_weather in changes")
	}
	if !names["search"] {
		t.Error("expected search in changes")
	}
	if names["mock_tool"] {
		t.Error("mock_tool should not be in changes")
	}
}
