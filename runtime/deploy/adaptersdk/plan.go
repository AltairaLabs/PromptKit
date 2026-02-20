package adaptersdk

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

const noChanges = "No changes"

// ResourcePlan holds the combined plan result.
type ResourcePlan struct {
	Changes []deploy.ResourceChange
	Tools   []ToolInfo
	Targets ToolTargetMap
	Policy  *ToolPolicyInfo
}

// GenerateResourcePlan builds a combined resource plan from PlanRequest fields.
// It extracts agent resources from the pack, tool resources from ArenaConfig,
// filters by tool policy blocklist, and matches tools against deploy targets.
// Adapters can call this directly from their Plan() method for a complete plan,
// or use the individual functions for custom logic.
func GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON string) (*ResourcePlan, error) {
	pack, err := ParsePack([]byte(packJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse pack: %w", err)
	}

	agentChanges := GenerateAgentResourcePlan(pack)

	tools, err := ExtractToolInfo(arenaConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to extract tool info: %w", err)
	}

	policy, err := ExtractToolPolicies(arenaConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to extract tool policies: %w", err)
	}

	filtered := FilterBlocklistedTools(tools, policy)

	targets, err := ParseDeployToolTargets(deployConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse deploy tool targets: %w", err)
	}

	toolChanges := GenerateToolGatewayPlan(filtered, targets)

	var changes []deploy.ResourceChange
	changes = append(changes, agentChanges...)
	changes = append(changes, toolChanges...)

	return &ResourcePlan{
		Changes: changes,
		Tools:   filtered,
		Targets: targets,
		Policy:  policy,
	}, nil
}

// FilterBlocklistedTools removes tools whose names appear in the policy blocklist.
func FilterBlocklistedTools(tools []ToolInfo, policy *ToolPolicyInfo) []ToolInfo {
	if policy == nil || len(policy.Blocklist) == 0 {
		return tools
	}
	blocked := make(map[string]bool, len(policy.Blocklist))
	for _, name := range policy.Blocklist {
		blocked[name] = true
	}
	var filtered []ToolInfo
	for _, t := range tools {
		if !blocked[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// SummarizeChanges generates a human-readable summary of resource changes.
// Example: "3 to create, 1 to update, 1 to delete"
func SummarizeChanges(changes []deploy.ResourceChange) string {
	if len(changes) == 0 {
		return noChanges
	}

	counts := map[deploy.Action]int{}
	for _, c := range changes {
		counts[c.Action]++
	}

	// Fixed order for deterministic output.
	order := []deploy.Action{
		deploy.ActionCreate,
		deploy.ActionUpdate,
		deploy.ActionDelete,
		deploy.ActionNoChange,
	}
	labels := map[deploy.Action]string{
		deploy.ActionCreate:   "to create",
		deploy.ActionUpdate:   "to update",
		deploy.ActionDelete:   "to delete",
		deploy.ActionNoChange: "unchanged",
	}

	var parts []string
	for _, action := range order {
		if n := counts[action]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, labels[action]))
		}
	}

	if len(parts) == 0 {
		return noChanges
	}
	return strings.Join(parts, ", ")
}
