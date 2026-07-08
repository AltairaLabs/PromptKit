package hooks_test

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// modelDenylist is a minimal hooks.ProviderHook that blocks calls to a
// specific model. Real hooks (e.g. runtime/hooks/guardrails) do content
// moderation, PII redaction, and similar — this shows the shape any
// ProviderHook implementation takes.
type modelDenylist struct {
	banned string
}

func (d modelDenylist) Name() string { return "model-denylist" }

func (d modelDenylist) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	if req.Model == d.banned {
		return hooks.Deny("model is not allowed")
	}
	return hooks.Allow
}

func (d modelDenylist) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// ExampleRegistry_RunBeforeProviderCall shows registering a ProviderHook and
// running it before an LLM call. Hooks run in registration order; the first
// denial short-circuits the chain and its Decision is returned.
func ExampleRegistry_RunBeforeProviderCall() {
	reg := hooks.NewRegistry(hooks.WithProviderHook(modelDenylist{banned: "banned-model"}))

	decision := reg.RunBeforeProviderCall(context.Background(), &hooks.ProviderRequest{Model: "banned-model"})
	fmt.Println(decision.Allow, decision.Reason)
	// Output: false model is not allowed
}
