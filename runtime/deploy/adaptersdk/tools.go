package adaptersdk

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// ToolInfo holds tool metadata extracted from ArenaConfig for deploy planning.
type ToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Mode        string      `json:"mode"` // "mock", "live", "mcp", "a2a"
	HasSchema   bool        `json:"has_schema"`
	InputSchema interface{} `json:"input_schema,omitempty"`
	HTTPURL     string      `json:"http_url,omitempty"`
	HTTPMethod  string      `json:"http_method,omitempty"`
}

// ToolTargetMap is an opaque map of tool name → adapter-specific target config.
// Each adapter defines its own target schema; the SDK only tracks which tools have targets.
type ToolTargetMap map[string]json.RawMessage

// ToolPolicyInfo holds tool policy from scenarios for deploy planning.
type ToolPolicyInfo struct {
	Blocklist []string `json:"blocklist,omitempty"`
}

// ParseDeployToolTargets extracts the tool_targets map from the opaque deploy config JSON.
// Returns raw JSON per tool so each adapter can unmarshal into its own target type.
func ParseDeployToolTargets(deployConfigJSON string) (ToolTargetMap, error) {
	if deployConfigJSON == "" {
		return nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(deployConfigJSON), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse deploy config: %w", err)
	}
	targetsRaw, ok := raw["tool_targets"]
	if !ok {
		return nil, nil
	}
	var targets ToolTargetMap
	if err := json.Unmarshal(targetsRaw, &targets); err != nil {
		return nil, fmt.Errorf("failed to parse tool_targets: %w", err)
	}
	return targets, nil
}

// GenerateToolGatewayPlan creates resource changes for tools that have target mappings.
func GenerateToolGatewayPlan(tools []ToolInfo, targets ToolTargetMap) []deploy.ResourceChange {
	if len(tools) == 0 || len(targets) == 0 {
		return nil
	}
	var changes []deploy.ResourceChange
	for _, tool := range tools {
		if _, hasTarget := targets[tool.Name]; !hasTarget {
			continue
		}
		changes = append(changes, deploy.ResourceChange{
			Type:   "tool_gateway",
			Name:   tool.Name,
			Action: deploy.ActionCreate,
			Detail: fmt.Sprintf("Create gateway target for tool %s", tool.Name),
		})
	}
	return changes
}
