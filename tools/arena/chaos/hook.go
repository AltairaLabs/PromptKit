// Package chaos provides fault injection for PromptArena resilience testing.
// It implements a hooks.ToolHook that intercepts tool execution and injects
// failures (errors, timeouts, latency) based on scenario chaos configuration.
package chaos

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

type contextKey int

const chaosConfigKey contextKey = iota

// Hook implements hooks.ToolHook to inject failures into tool execution.
type Hook struct{}

// NewHook creates a new chaos injection hook.
func NewHook() *Hook {
	return &Hook{}
}

// Name returns the hook name.
func (h *Hook) Name() string {
	return "chaos-injection"
}

// BeforeExecution checks chaos config and injects tool failures.
func (h *Hook) BeforeExecution(ctx context.Context, req hooks.ToolRequest) hooks.Decision {
	cfg := ConfigFromContext(ctx)
	if cfg == nil {
		return hooks.Allow
	}

	for _, rule := range cfg.ToolFailures {
		if rule.Tool != req.Name {
			continue
		}
		if !shouldFire(rule.Probability) {
			continue
		}
		return applyFailure(req.Name, rule)
	}

	return hooks.Allow
}

// AfterExecution is a no-op — always allows.
func (h *Hook) AfterExecution(_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

// applyFailure creates the appropriate failure decision for the given mode.
func applyFailure(toolName string, rule config.ChaosToolFailure) hooks.Decision {
	msg := rule.Message
	meta := map[string]any{
		"chaos_injected": true,
		"chaos_mode":     rule.Mode,
		"chaos_tool":     toolName,
	}

	switch rule.Mode {
	case "error":
		if msg == "" {
			msg = fmt.Sprintf("Chaos: tool %s failed with injected error", toolName)
		}
		return hooks.DenyWithMetadata(msg, meta)

	case "timeout":
		if msg == "" {
			msg = fmt.Sprintf("Chaos: tool %s timed out (injected)", toolName)
		}
		meta["chaos_timeout"] = true
		return hooks.DenyWithMetadata(msg, meta)

	case "slow":
		// For "slow" mode, we add latency but still allow the call
		const slowDelaySeconds = 2
		time.Sleep(slowDelaySeconds * time.Second)
		meta["chaos_delay_ms"] = slowDelaySeconds * 1000 //nolint:mnd // convert seconds to ms
		return hooks.Decision{Allow: true, Metadata: meta}

	default:
		if msg == "" {
			msg = fmt.Sprintf("Chaos: tool %s failed (mode=%s)", toolName, rule.Mode)
		}
		return hooks.DenyWithMetadata(msg, meta)
	}
}

// shouldFire returns true if the failure should be triggered based on probability.
func shouldFire(probability float64) bool {
	if probability <= 0 {
		return true // default: always fire (0 means unset)
	}
	if probability >= 1.0 {
		return true
	}
	return rand.Float64() < probability //nolint:gosec // chaos testing doesn't need crypto rand
}

// WithConfig stores chaos config in the context.
func WithConfig(ctx context.Context, cfg *config.ChaosConfig) context.Context {
	return context.WithValue(ctx, chaosConfigKey, cfg)
}

// ConfigFromContext retrieves chaos config from the context.
func ConfigFromContext(ctx context.Context) *config.ChaosConfig {
	v, _ := ctx.Value(chaosConfigKey).(*config.ChaosConfig)
	return v
}
