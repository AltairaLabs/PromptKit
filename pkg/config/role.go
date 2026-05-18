package config

import "fmt"

// Role values for the Provider.Role field. Defaults to LLM when unset
// so existing provider yamls (which predate the field) keep working
// with no migration.
//
// Renamed from "capability" 2026-05-18. The field always meant "what
// role does this provider fill in the arena" (llm/tts/stt), not "what
// can this provider do" — the older name collided with the per-model
// feature-flag list (Provider.Capabilities). One word per concept now.
const (
	RoleLLM = "llm"
	RoleTTS = "tts"
	RoleSTT = "stt"
)

// knownRoles is the set accepted by ValidateRole. An empty string is
// also accepted (treated as LLM by GetRole).
var knownRoles = map[string]struct{}{
	RoleLLM: {},
	RoleTTS: {},
	RoleSTT: {},
}

// GetRole returns the provider's role, defaulting to "llm".
func (p *Provider) GetRole() string {
	if p == nil || p.Role == "" {
		return RoleLLM
	}
	return p.Role
}

// ValidateRole returns an error if Role is set to an unrecognized
// value. Empty is treated as LLM and accepted.
func (p *Provider) ValidateRole() error {
	if p == nil || p.Role == "" {
		return nil
	}
	if _, ok := knownRoles[p.Role]; !ok {
		return fmt.Errorf("unknown provider role %q (valid: %s, %s, %s)",
			p.Role, RoleLLM, RoleTTS, RoleSTT)
	}
	return nil
}
