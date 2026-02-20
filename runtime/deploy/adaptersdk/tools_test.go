package adaptersdk

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

func TestParseDeployToolTargets_ValidConfig(t *testing.T) {
	configJSON := `{
		"provider": "agentcore",
		"tool_targets": {
			"get_weather": {"lambda_arn": "arn:aws:lambda:us-east-1:123:function:weather"},
			"search": {"lambda_arn": "arn:aws:lambda:us-east-1:123:function:search"}
		}
	}`
	targets, err := ParseDeployToolTargets(configJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	// Verify raw JSON is preserved for adapter-specific unmarshaling
	var weatherTarget struct {
		LambdaARN string `json:"lambda_arn"`
	}
	if err := json.Unmarshal(targets["get_weather"], &weatherTarget); err != nil {
		t.Fatalf("failed to unmarshal weather target: %v", err)
	}
	if weatherTarget.LambdaARN != "arn:aws:lambda:us-east-1:123:function:weather" {
		t.Errorf("expected weather lambda ARN, got %s", weatherTarget.LambdaARN)
	}
}

func TestParseDeployToolTargets_NoToolTargets(t *testing.T) {
	configJSON := `{"provider": "agentcore"}`
	targets, err := ParseDeployToolTargets(configJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets != nil {
		t.Errorf("expected nil targets, got %v", targets)
	}
}

func TestParseDeployToolTargets_EmptyString(t *testing.T) {
	targets, err := ParseDeployToolTargets("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets != nil {
		t.Errorf("expected nil targets, got %v", targets)
	}
}

func TestParseDeployToolTargets_InvalidJSON(t *testing.T) {
	_, err := ParseDeployToolTargets("{invalid}")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseDeployToolTargets_InvalidToolTargetsJSON(t *testing.T) {
	configJSON := `{"tool_targets": "not-an-object"}`
	_, err := ParseDeployToolTargets(configJSON)
	if err == nil {
		t.Fatal("expected error for invalid tool_targets JSON")
	}
}

func TestGenerateToolGatewayPlan_ToolsWithTargets(t *testing.T) {
	tools := []ToolInfo{
		{Name: "get_weather", Description: "Get weather", Mode: "live"},
		{Name: "search", Description: "Search", Mode: "live"},
	}
	targets := ToolTargetMap{
		"get_weather": json.RawMessage(`{"lambda_arn": "arn:123"}`),
		"search":      json.RawMessage(`{"lambda_arn": "arn:456"}`),
	}

	changes := GenerateToolGatewayPlan(tools, targets)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	for i, c := range changes {
		if c.Type != "tool_gateway" {
			t.Errorf("change[%d]: expected type tool_gateway, got %s", i, c.Type)
		}
		if c.Action != deploy.ActionCreate {
			t.Errorf("change[%d]: expected action CREATE, got %s", i, c.Action)
		}
	}
	if changes[0].Name != "get_weather" {
		t.Errorf("expected first change name get_weather, got %s", changes[0].Name)
	}
	if changes[1].Name != "search" {
		t.Errorf("expected second change name search, got %s", changes[1].Name)
	}
}

func TestGenerateToolGatewayPlan_ToolsWithoutTargets(t *testing.T) {
	tools := []ToolInfo{
		{Name: "get_weather", Description: "Get weather", Mode: "live"},
		{Name: "search", Description: "Search", Mode: "mock"},
	}
	targets := ToolTargetMap{
		"other_tool": json.RawMessage(`{"lambda_arn": "arn:789"}`),
	}

	changes := GenerateToolGatewayPlan(tools, targets)
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
}

func TestGenerateToolGatewayPlan_PartialMatch(t *testing.T) {
	tools := []ToolInfo{
		{Name: "get_weather", Description: "Get weather", Mode: "live"},
		{Name: "search", Description: "Search", Mode: "live"},
		{Name: "calculate", Description: "Calculate", Mode: "mock"},
	}
	targets := ToolTargetMap{
		"search": json.RawMessage(`{"lambda_arn": "arn:456"}`),
	}

	changes := GenerateToolGatewayPlan(tools, targets)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Name != "search" {
		t.Errorf("expected change name search, got %s", changes[0].Name)
	}
}

func TestGenerateToolGatewayPlan_EmptyInputs(t *testing.T) {
	if changes := GenerateToolGatewayPlan(nil, nil); changes != nil {
		t.Errorf("expected nil for nil inputs, got %v", changes)
	}
	if changes := GenerateToolGatewayPlan([]ToolInfo{}, ToolTargetMap{}); changes != nil {
		t.Errorf("expected nil for empty inputs, got %v", changes)
	}
	tools := []ToolInfo{{Name: "a"}}
	if changes := GenerateToolGatewayPlan(tools, nil); changes != nil {
		t.Errorf("expected nil for nil targets, got %v", changes)
	}
	targets := ToolTargetMap{"a": json.RawMessage(`{}`)}
	if changes := GenerateToolGatewayPlan(nil, targets); changes != nil {
		t.Errorf("expected nil for nil tools, got %v", changes)
	}
}
