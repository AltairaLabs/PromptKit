package guardrails

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// RequiredFieldsHook denies provider responses that do not contain all
// required field strings. It does not support streaming because all
// content must be available to check for field presence.
type RequiredFieldsHook struct {
	requiredFields []string
}

// Compile-time interface check.
var _ hooks.ProviderHook = (*RequiredFieldsHook)(nil)

// NewRequiredFieldsHook creates a guardrail that rejects responses missing
// any of the given field strings.
func NewRequiredFieldsHook(fields []string) *RequiredFieldsHook {
	return &RequiredFieldsHook{requiredFields: fields}
}

// Name returns the guardrail type identifier.
func (h *RequiredFieldsHook) Name() string { return nameRequiredFields }

// BeforeCall is a no-op — field presence is checked after generation.
func (h *RequiredFieldsHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

// AfterCall checks the completed response for required fields.
func (h *RequiredFieldsHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	var missing []string
	for _, field := range h.requiredFields {
		if !strings.Contains(resp.Message.Content, field) {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return hooks.DenyWithMetadata(
			"missing required fields: "+strings.Join(missing, ", "),
			map[string]any{
				"validator_type": nameRequiredFields,
				"missing":        missing,
			},
		)
	}
	return hooks.Allow
}
