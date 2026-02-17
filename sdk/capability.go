package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// Capability represents a platform feature that provides namespaced tools.
// Capabilities are auto-inferred from pack structure or explicitly added
// via WithCapability.
type Capability interface {
	// Name returns the capability identifier (e.g., "workflow", "a2a").
	Name() string

	// Init initializes the capability with pack context.
	Init(ctx CapabilityContext) error

	// RegisterTools registers the capability's tools into the registry.
	RegisterTools(registry *tools.Registry)

	// Close releases any resources held by the capability.
	Close() error
}

// StatefulCapability can update tools dynamically (e.g., after workflow state changes).
type StatefulCapability interface {
	Capability
	RefreshTools(registry *tools.Registry)
}

// CapabilityContext provides read-only access to pack and config during Init.
type CapabilityContext struct {
	Pack       *pack.Pack
	PromptName string
}

// inferCapabilities inspects pack structure and returns auto-detected capabilities.
func inferCapabilities(p *pack.Pack) []Capability {
	var caps []Capability
	if p.Workflow != nil {
		caps = append(caps, NewWorkflowCapability())
	}
	if p.Agents != nil && len(p.Agents.Members) > 0 {
		caps = append(caps, NewA2ACapability())
	}
	return caps
}

// mergeCapabilities deduplicates by Name(), explicit takes precedence over inferred.
func mergeCapabilities(explicit, inferred []Capability) []Capability {
	seen := make(map[string]bool, len(explicit))
	result := make([]Capability, 0, len(explicit)+len(inferred))

	for _, cap := range explicit {
		seen[cap.Name()] = true
		result = append(result, cap)
	}
	for _, cap := range inferred {
		if !seen[cap.Name()] {
			result = append(result, cap)
		}
	}
	return result
}
