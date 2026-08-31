package prompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
)

// RFC 0012 provider requirements.
//
// A pack declares the LOGICAL providers it needs — by key and role — and the
// host resolves each to a concrete provider. The spec is deliberately narrow
// about what a runtime owes: endpoints, credentials and model identifiers are
// explicit non-goals, and there is exactly one behavioral requirement:
//
//	"A consuming runtime/deployer SHOULD treat an unsatisfied `required`
//	 requirement as an error and an unsatisfied optional requirement as a
//	 warning."
//
// Everything here serves that sentence. The declaration was previously dropped
// on load — the schema accepted `requires`, no Go field held it, and nothing
// read it — so a pack that stated its dependencies was no better off than one
// that did not.

// Requires is the pack-level requirements block.
type Requires = packspec.PackRequires

// ProviderRequirement is a single logical provider dependency.
type ProviderRequirement = packspec.ProviderRequirement

// Reserved requirement keys and roles named by RFC 0012.
const (
	// RequirementKeyDefault is reserved for the primary LLM.
	RequirementKeyDefault = "default"
	// RequirementRoleLLM is the role a bare string shorthand expands to.
	RequirementRoleLLM = "llm"
	// AnyKey marks a role the host can satisfy under any key, for providers
	// supplied without an identifier to match on.
	AnyKey = "*"
)

// ResolvedRequirement is a requirement with the spec's defaults applied, so
// consumers never have to know which fields were written and which were
// implied.
type ResolvedRequirement struct {
	Key         string
	Role        string
	Description string
	Required    bool
}

// ResolveRequirements expands a pack's requirements into their full form and
// reports the spec's structural rules.
//
// Two rules the JSON Schema cannot express, both stated in prose by RFC 0012:
//
//   - a bare string is shorthand for {key: <string>, role: "llm", required: true}
//   - key values MUST be unique, key being the sole discriminator between
//     requirements
//
// A duplicate key is an error rather than a last-one-wins merge: two entries
// with one key means the author intended two providers and will get one, which
// is the kind of silent loss this whole area keeps producing.
func ResolveRequirements(p *Pack) ([]ResolvedRequirement, error) {
	if p == nil || p.Requires == nil || len(p.Requires.Providers) == 0 {
		return nil, nil
	}

	out := make([]ResolvedRequirement, 0, len(p.Requires.Providers))
	seen := make(map[string]bool, len(p.Requires.Providers))

	for i, raw := range p.Requires.Providers {
		r, err := resolveOne(raw, i)
		if err != nil {
			return nil, err
		}
		if seen[r.Key] {
			return nil, fmt.Errorf(
				"requires.providers: duplicate key %q — key is the sole discriminator "+
					"between requirements, so two providers of the same role need two keys", r.Key)
		}
		seen[r.Key] = true
		out = append(out, r)
	}
	return out, nil
}

func resolveOne(raw *ProviderRequirement, index int) (ResolvedRequirement, error) {
	// Shorthand: a bare string names the key and implies a required llm.
	if raw == nil {
		return ResolvedRequirement{}, fmt.Errorf("requires.providers[%d]: null entry", index)
	}
	if raw.Shorthand != "" {
		return ResolvedRequirement{
			Key:      raw.Shorthand,
			Role:     RequirementRoleLLM,
			Required: true,
		}, nil
	}
	if raw.Key == "" || raw.Role == "" {
		return ResolvedRequirement{}, fmt.Errorf(
			"requires.providers[%d]: key and role are both required in the object form "+
				"(use the string shorthand for a plain llm requirement)", index)
	}
	return ResolvedRequirement{
		Key:         raw.Key,
		Role:        raw.Role,
		Description: raw.Description,
		// required defaults to TRUE when omitted, so nil means required.
		Required: packspec.Deref(raw.Required, true),
	}, nil
}

// ProviderInventory is what a host can supply, as role -> available keys.
type ProviderInventory map[string][]string

// Unsatisfied splits a pack's requirements into those the inventory cannot
// satisfy, separating required from optional so the caller can apply the
// spec's error-versus-warning distinction.
//
// Matching is on key AND role: a requirement for an `embedding` named "judge"
// is not satisfied by an `llm` named "judge". Resolution beyond that — which
// concrete model, from where — is explicitly the host's business, not the
// spec's.
//
// A host may list AnyKey for a role to mean "one provider of this role is
// wired, but it has no name to match on". That covers providers supplied
// programmatically rather than by key; without it the check rejects a host that
// has wired a working provider, and a false failure at Open is worse than no
// check, because it blocks a correct setup rather than an incorrect one.
func Unsatisfied(reqs []ResolvedRequirement, have ProviderInventory) (required, optional []ResolvedRequirement) {
	for _, r := range reqs {
		keys := have[r.Role]
		if slicesContains(keys, r.Key) || slicesContains(keys, AnyKey) {
			continue
		}
		if r.Required {
			required = append(required, r)
		} else {
			optional = append(optional, r)
		}
	}
	return required, optional
}

func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// DescribeUnsatisfied renders requirements for an error or log message, naming
// what the pack asked for and why, so an operator can act without opening the
// pack. The description exists in the spec for exactly this purpose.
func DescribeUnsatisfied(reqs []ResolvedRequirement) string {
	if len(reqs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(reqs))
	for _, r := range reqs {
		s := fmt.Sprintf("%s (role %s)", r.Key, r.Role)
		if r.Description != "" {
			s += ": " + r.Description
		}
		parts = append(parts, s)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}
