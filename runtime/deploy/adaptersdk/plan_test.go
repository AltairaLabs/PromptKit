package adaptersdk

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// --- GenerateResourcePlan tests ---

func TestGenerateResourcePlan_MultiAgentWithToolsAndTargets(t *testing.T) {
	packJSON := `{
		"prompts": {
			"router": {"description": "Routes requests"},
			"worker": {"description": "Does work"}
		},
		"agents": {
			"entry": "router",
			"members": {
				"router": {"description": "Routes requests"},
				"worker": {"description": "Does work"}
			}
		}
	}`
	arenaConfigJSON := `{
		"tool_specs": {
			"get_weather": {"description": "Get weather", "mode": "live", "input_schema": {"type": "object"}}
		}
	}`
	deployConfigJSON := `{
		"tool_targets": {
			"get_weather": {"lambda_arn": "arn:aws:lambda:us-east-1:123:function:weather"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect: 2 agent_runtime + 2 a2a_endpoint + 1 gateway + 1 tool_gateway = 6
	if len(plan.Changes) != 6 {
		t.Fatalf("expected 6 changes, got %d", len(plan.Changes))
	}

	typeCounts := map[string]int{}
	for _, c := range plan.Changes {
		typeCounts[c.Type]++
	}
	if typeCounts["agent_runtime"] != 2 {
		t.Errorf("expected 2 agent_runtime, got %d", typeCounts["agent_runtime"])
	}
	if typeCounts["a2a_endpoint"] != 2 {
		t.Errorf("expected 2 a2a_endpoint, got %d", typeCounts["a2a_endpoint"])
	}
	if typeCounts["gateway"] != 1 {
		t.Errorf("expected 1 gateway, got %d", typeCounts["gateway"])
	}
	if typeCounts["tool_gateway"] != 1 {
		t.Errorf("expected 1 tool_gateway, got %d", typeCounts["tool_gateway"])
	}

	if len(plan.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(plan.Tools))
	}
	if len(plan.Targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(plan.Targets))
	}
}

func TestGenerateResourcePlan_SingleAgentWithTools(t *testing.T) {
	packJSON := `{
		"prompts": {
			"main": {"description": "Main agent"}
		}
	}`
	arenaConfigJSON := `{
		"tool_specs": {
			"search": {"description": "Search", "mode": "live", "input_schema": {"type": "object"}}
		}
	}`
	deployConfigJSON := `{
		"tool_targets": {
			"search": {"endpoint": "https://api.example.com/search"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Single-agent: no agent resources, only tool_gateway
	if len(plan.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(plan.Changes))
	}
	if plan.Changes[0].Type != "tool_gateway" {
		t.Errorf("expected tool_gateway, got %s", plan.Changes[0].Type)
	}
}

func TestGenerateResourcePlan_BlocklistedToolsFiltered(t *testing.T) {
	packJSON := `{"prompts": {"main": {"description": "Main"}}}`
	arenaConfigJSON := `{
		"tool_specs": {
			"allowed_tool": {"description": "Allowed", "mode": "live"},
			"blocked_tool": {"description": "Blocked", "mode": "live"}
		},
		"loaded_scenarios": {
			"test": {
				"tool_policy": {
					"blocklist": ["blocked_tool"]
				}
			}
		}
	}`
	deployConfigJSON := `{
		"tool_targets": {
			"allowed_tool": {"endpoint": "https://a.com"},
			"blocked_tool": {"endpoint": "https://b.com"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// blocked_tool should be filtered — only allowed_tool gets a gateway
	if len(plan.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(plan.Changes))
	}
	if plan.Changes[0].Name != "allowed_tool" {
		t.Errorf("expected allowed_tool, got %s", plan.Changes[0].Name)
	}

	// Tools list should also be filtered
	if len(plan.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(plan.Tools))
	}
	if plan.Tools[0].Name != "allowed_tool" {
		t.Errorf("expected allowed_tool in tools, got %s", plan.Tools[0].Name)
	}

	// Policy should still be available
	if plan.Policy == nil {
		t.Fatal("expected non-nil policy")
	}
	if len(plan.Policy.Blocklist) != 1 || plan.Policy.Blocklist[0] != "blocked_tool" {
		t.Errorf("expected blocklist [blocked_tool], got %v", plan.Policy.Blocklist)
	}
}

func TestGenerateResourcePlan_EmptyInputs(t *testing.T) {
	// Empty pack JSON returns error
	_, err := GenerateResourcePlan("", "", "")
	if err == nil {
		t.Fatal("expected error for empty pack JSON")
	}

	// Invalid pack JSON returns error
	_, err = GenerateResourcePlan("{invalid}", "", "")
	if err == nil {
		t.Fatal("expected error for invalid pack JSON")
	}

	// Valid pack but empty arena/deploy config — graceful
	plan, err := GenerateResourcePlan(`{"prompts": {"main": {}}}`, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(plan.Changes))
	}
	if plan.Tools != nil {
		t.Errorf("expected nil tools, got %v", plan.Tools)
	}
	if plan.Targets != nil {
		t.Errorf("expected nil targets, got %v", plan.Targets)
	}
	if plan.Policy != nil {
		t.Errorf("expected nil policy, got %v", plan.Policy)
	}
}

func TestGenerateResourcePlan_NoToolTargets(t *testing.T) {
	packJSON := `{
		"prompts": {
			"router": {"description": "Routes"},
			"worker": {"description": "Works"}
		},
		"agents": {
			"entry": "router",
			"members": {
				"router": {"description": "Routes"},
				"worker": {"description": "Works"}
			}
		}
	}`
	arenaConfigJSON := `{
		"tool_specs": {
			"search": {"description": "Search", "mode": "live"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only agent resources (no tool targets → no tool_gateway)
	for _, c := range plan.Changes {
		if c.Type == "tool_gateway" {
			t.Error("expected no tool_gateway changes when no targets")
		}
	}
	// Should have 5 agent resources: 2 runtime + 2 endpoint + 1 gateway
	if len(plan.Changes) != 5 {
		t.Errorf("expected 5 agent changes, got %d", len(plan.Changes))
	}
}

// --- FilterBlocklistedTools tests ---

func TestFilterBlocklistedTools_FiltersMatchingNames(t *testing.T) {
	tools := []ToolInfo{
		{Name: "allowed"},
		{Name: "blocked_a"},
		{Name: "also_allowed"},
		{Name: "blocked_b"},
	}
	policy := &ToolPolicyInfo{
		Blocklist: []string{"blocked_a", "blocked_b"},
	}

	filtered := FilterBlocklistedTools(tools, policy)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
	if filtered[0].Name != "allowed" {
		t.Errorf("expected allowed, got %s", filtered[0].Name)
	}
	if filtered[1].Name != "also_allowed" {
		t.Errorf("expected also_allowed, got %s", filtered[1].Name)
	}
}

func TestFilterBlocklistedTools_NilPolicy(t *testing.T) {
	tools := []ToolInfo{{Name: "a"}, {Name: "b"}}
	filtered := FilterBlocklistedTools(tools, nil)
	if len(filtered) != 2 {
		t.Errorf("expected 2 tools unchanged, got %d", len(filtered))
	}
}

func TestFilterBlocklistedTools_EmptyBlocklist(t *testing.T) {
	tools := []ToolInfo{{Name: "a"}, {Name: "b"}}
	policy := &ToolPolicyInfo{Blocklist: []string{}}
	filtered := FilterBlocklistedTools(tools, policy)
	if len(filtered) != 2 {
		t.Errorf("expected 2 tools unchanged, got %d", len(filtered))
	}
}

// --- SummarizeChanges tests ---

func TestSummarizeChanges_MixedActions(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "a", Action: deploy.ActionCreate},
		{Type: "a2a_endpoint", Name: "b", Action: deploy.ActionCreate},
		{Type: "tool_gateway", Name: "c", Action: deploy.ActionCreate},
		{Type: "agent_runtime", Name: "d", Action: deploy.ActionUpdate},
		{Type: "gateway", Name: "e", Action: deploy.ActionDelete},
	}
	summary := SummarizeChanges(changes)
	expected := "3 to create, 1 to update, 1 to delete"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

func TestSummarizeChanges_SingleAction(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "a", Action: deploy.ActionCreate},
		{Type: "a2a_endpoint", Name: "b", Action: deploy.ActionCreate},
	}
	summary := SummarizeChanges(changes)
	expected := "2 to create"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

func TestSummarizeChanges_Empty(t *testing.T) {
	summary := SummarizeChanges(nil)
	if summary != "No changes" {
		t.Errorf("expected %q, got %q", "No changes", summary)
	}
	summary = SummarizeChanges([]deploy.ResourceChange{})
	if summary != "No changes" {
		t.Errorf("expected %q, got %q", "No changes", summary)
	}
}

func TestSummarizeChanges_IncludesNoChange(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "a", Action: deploy.ActionCreate},
		{Type: "a2a_endpoint", Name: "b", Action: deploy.ActionNoChange},
		{Type: "gateway", Name: "c", Action: deploy.ActionNoChange},
	}
	summary := SummarizeChanges(changes)
	expected := "1 to create, 2 unchanged"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

func TestSummarizeChanges_OnlyNoChange(t *testing.T) {
	changes := []deploy.ResourceChange{
		{Type: "agent_runtime", Name: "a", Action: deploy.ActionNoChange},
	}
	summary := SummarizeChanges(changes)
	expected := "1 unchanged"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

// --- Integration: GenerateResourcePlan + SummarizeChanges ---

func TestGenerateResourcePlan_SummarizeIntegration(t *testing.T) {
	packJSON := `{
		"prompts": {
			"router": {"description": "Routes"},
			"worker": {"description": "Works"}
		},
		"agents": {
			"entry": "router",
			"members": {
				"router": {},
				"worker": {}
			}
		}
	}`
	deployConfigJSON := `{
		"tool_targets": {
			"search": {"endpoint": "https://api.example.com"}
		}
	}`
	arenaConfigJSON := `{
		"tool_specs": {
			"search": {"description": "Search", "mode": "live"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summary := SummarizeChanges(plan.Changes)
	expected := "6 to create"
	if summary != expected {
		t.Errorf("expected %q, got %q", expected, summary)
	}
}

// --- GenerateResourcePlan error cases ---

func TestGenerateResourcePlan_InvalidArenaConfig(t *testing.T) {
	packJSON := `{"prompts": {"main": {}}}`
	_, err := GenerateResourcePlan(packJSON, "{invalid}", "")
	if err == nil {
		t.Fatal("expected error for invalid arena config JSON")
	}
}

func TestGenerateResourcePlan_InvalidDeployConfig(t *testing.T) {
	packJSON := `{"prompts": {"main": {}}}`
	_, err := GenerateResourcePlan(packJSON, "", "{invalid}")
	if err == nil {
		t.Fatal("expected error for invalid deploy config JSON")
	}
}

// Verify Targets field contains raw JSON
func TestGenerateResourcePlan_TargetsPreserveRawJSON(t *testing.T) {
	packJSON := `{"prompts": {"main": {}}}`
	arenaConfigJSON := `{
		"tool_specs": {
			"my_tool": {"description": "A tool", "mode": "live"}
		}
	}`
	deployConfigJSON := `{
		"tool_targets": {
			"my_tool": {"custom_field": "custom_value"}
		}
	}`

	plan, err := GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, ok := plan.Targets["my_tool"]
	if !ok {
		t.Fatal("expected my_tool in targets")
	}
	var target struct {
		CustomField string `json:"custom_field"`
	}
	if err := json.Unmarshal(raw, &target); err != nil {
		t.Fatalf("failed to unmarshal target: %v", err)
	}
	if target.CustomField != "custom_value" {
		t.Errorf("expected custom_value, got %s", target.CustomField)
	}
}
