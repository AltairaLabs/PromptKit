// Package consent provides consent simulation for PromptArena scenarios.
// It implements a hooks.ToolHook that intercepts client-side tool execution
// and simulates consent grant, denial, or timeout based on scenario overrides.
package consent

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

type contextKey int

const (
	consentOverridesKey contextKey = iota
	toolRegistryKey
)

// SimulationHook implements hooks.ToolHook to simulate consent decisions
// for client-side tools during PromptArena scenario execution.
type SimulationHook struct{}

// NewSimulationHook creates a new consent simulation hook.
func NewSimulationHook() *SimulationHook {
	return &SimulationHook{}
}

// Name returns the hook name.
func (h *SimulationHook) Name() string {
	return "consent-simulation"
}

// BeforeExecution checks consent overrides and blocks denied/timed-out tools.
func (h *SimulationHook) BeforeExecution(ctx context.Context, req hooks.ToolRequest) hooks.Decision {
	overrides := OverridesFromContext(ctx)
	if overrides == nil {
		return hooks.Allow
	}

	action, ok := overrides[req.Name]
	if !ok {
		return hooks.Allow
	}

	switch action {
	case "deny":
		reason := buildDenyMessage(req.Name, h.lookupDeclineStrategy(ctx, req.Name))
		return hooks.DenyWithMetadata(reason, map[string]any{
			"consent_status": "denied",
		})

	case "timeout":
		return hooks.DenyWithMetadata(
			fmt.Sprintf("Tool %s timed out waiting for user consent", req.Name),
			map[string]any{"consent_status": "timeout"},
		)

	default: // "grant" or any other value
		return hooks.Decision{Allow: true, Metadata: map[string]any{
			"consent_status": "granted",
		}}
	}
}

// AfterExecution is a no-op — always allows.
func (h *SimulationHook) AfterExecution(_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

// lookupDeclineStrategy retrieves the decline strategy from the tool descriptor.
func (h *SimulationHook) lookupDeclineStrategy(ctx context.Context, toolName string) string {
	registry := ToolRegistryFromContext(ctx)
	if registry == nil {
		return ""
	}

	descriptor := registry.Get(toolName)
	if descriptor == nil || descriptor.ClientConfig == nil || descriptor.ClientConfig.Consent == nil {
		return ""
	}

	return descriptor.ClientConfig.Consent.DeclineStrategy
}

// buildDenyMessage returns the appropriate denial message based on decline strategy.
func buildDenyMessage(toolName, strategy string) string {
	switch strategy {
	case tools.DeclineStrategyFallback:
		return fmt.Sprintf("Tool %s unavailable: user declined consent. Use alternative approach.", toolName)
	case tools.DeclineStrategyRetry:
		return fmt.Sprintf("Tool %s denied: user declined consent. Try a different approach.", toolName)
	default: // "error" or unset
		return fmt.Sprintf("Tool %s denied: user declined consent", toolName)
	}
}

// WithConsentOverrides stores consent overrides in the context.
func WithConsentOverrides(ctx context.Context, overrides map[string]string) context.Context {
	return context.WithValue(ctx, consentOverridesKey, overrides)
}

// OverridesFromContext retrieves consent overrides from the context.
func OverridesFromContext(ctx context.Context) map[string]string {
	v, _ := ctx.Value(consentOverridesKey).(map[string]string)
	return v
}

// WithToolRegistry stores a tool registry in the context.
func WithToolRegistry(ctx context.Context, registry *tools.Registry) context.Context {
	return context.WithValue(ctx, toolRegistryKey, registry)
}

// ToolRegistryFromContext retrieves a tool registry from the context.
func ToolRegistryFromContext(ctx context.Context) *tools.Registry {
	v, _ := ctx.Value(toolRegistryKey).(*tools.Registry)
	return v
}
