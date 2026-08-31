package prompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
)

// RFC 0013 governance declarations.
//
// A pack states governance facts about the agent it defines — who is
// accountable, how autonomously it acts, what it was built for, whether it must
// disclose itself as AI. These are human-declared claims. Nothing here enforces
// anything: the spec is explicit that a governance block describes, and
// action_scope "describes consequence; does not gate anything".
//
// What the spec DOES place on a runtime is a resolution rule, and it is the
// whole reason this file exists:
//
//	"Governance facts for this agent, overriding metadata.governance by
//	 per-field replacement: a field present here replaces the pack value for
//	 that field, a field absent inherits. Arrays and extensions replace whole."
//
// That is prose the JSON Schema cannot express, so without it every consumer
// implements the merge itself — and the array rule is the one they get wrong,
// because concatenating is the intuitive guess and the spec says replace.

// Governance is a governance declaration, at pack or agent level.
type Governance = packspec.Governance

// AutonomyLevel values named by RFC 0013. The schema closes this enum, so a
// value outside these four fails validation.
const (
	// AutonomyLevelSuggests — produces output, a human performs any action.
	AutonomyLevelSuggests = "suggests"
	// AutonomyLevelActsWithApproval — acts, but each consequential action is
	// approved first.
	AutonomyLevelActsWithApproval = "acts_with_approval"
	// AutonomyLevelActsWithOversight — acts on its own; a human monitors and
	// can intervene or reverse.
	AutonomyLevelActsWithOversight = "acts_with_oversight"
	// AutonomyLevelActsAutonomously — acts without a human in the loop.
	AutonomyLevelActsAutonomously = "acts_autonomously"
)

// PackGovernance returns the pack-level governance declaration, or nil.
//
// Returns a copy: the result is a statement about the pack, and a caller that
// adjusted it in place would rewrite the loaded pack for everyone else.
func PackGovernance(pack *Pack) *Governance {
	if pack == nil || pack.Metadata == nil {
		return nil
	}
	return copyGovernance(pack.Metadata.Governance)
}

// AgentGovernance returns an agent's own governance declaration as written,
// without inheriting anything from the pack. Use ResolveGovernance for the
// effective values; this is for reporting what the pack actually says.
//
// The error distinguishes "this agent declares no governance" from "there is no
// such agent" — see ResolveGovernance.
func AgentGovernance(pack *Pack, agent string) (*Governance, error) {
	def, err := lookupAgent(pack, agent)
	if err != nil {
		return nil, err
	}
	return copyGovernance(def.Governance), nil
}

// ResolveGovernance returns the effective governance for one agent: the pack
// declaration with the agent's own fields laid over it.
//
// An unknown agent is an error rather than a fallback to the pack values. For
// most lookups a quiet fallback is a convenience; for governance it is a lie —
// a caller asking about "billing-agent" and getting the pack's autonomy level
// because it typed the name wrong would be told the agent needs no approval
// when nothing had been checked at all.
//
// An empty agent name returns the pack-level declaration, so callers that may
// or may not be scoped to an agent need no special case.
func ResolveGovernance(pack *Pack, agent string) (*Governance, error) {
	if agent == "" {
		return PackGovernance(pack), nil
	}

	def, err := lookupAgent(pack, agent)
	if err != nil {
		return nil, err
	}

	base := PackGovernance(pack)
	overlay := def.Governance

	switch {
	case overlay == nil:
		// Absent entirely: inherit the pack declaration wholesale.
		return base, nil
	case base == nil:
		// Nothing to inherit from. The agent's own facts still stand — an
		// undeclared pack does not erase what the agent declares about itself.
		return copyGovernance(overlay), nil
	}

	return overlayGovernance(base, overlay), nil
}

// overlayGovernance applies the spec's per-field replacement. base is already a
// copy, so it is written in place.
//
// "Present" means carrying a value. The generated struct uses plain strings and
// slices with omitempty for every field the spec makes optional, so an absent
// property and an empty one are the same value after a round trip and cannot be
// told apart — except for requires_ai_disclosure, which is *bool precisely so
// that an explicit false is distinguishable from silence. That distinction is
// load-bearing here: an agent that must NOT disclose, under a pack that says it
// must, has to be able to say so.
func overlayGovernance(base, overlay *Governance) *Governance {
	// Scalars: a non-empty value replaces.
	if overlay.IntendedPurpose != "" {
		base.IntendedPurpose = overlay.IntendedPurpose
	}
	if overlay.AutonomyLevel != "" {
		base.AutonomyLevel = overlay.AutonomyLevel
	}
	if overlay.AccountableOwner != "" {
		base.AccountableOwner = overlay.AccountableOwner
	}
	if overlay.OperatorRole != "" {
		base.OperatorRole = overlay.OperatorRole
	}
	if overlay.RiskClassification != "" {
		base.RiskClassification = overlay.RiskClassification
	}

	// The one field that can say "explicitly false" rather than "unset".
	if overlay.RequiresAIDisclosure != nil {
		base.RequiresAIDisclosure = packspec.Ptr(*overlay.RequiresAIDisclosure)
	}

	// Arrays replace whole — they are NOT appended. An agent narrowing the
	// pack's approved environments to one must end up with one, not with the
	// pack's list plus its own.
	if len(overlay.ForeseeableMisuse) > 0 {
		base.ForeseeableMisuse = copyStrings(overlay.ForeseeableMisuse)
	}
	if len(overlay.IntendedDeploymentContexts) > 0 {
		base.IntendedDeploymentContexts = copyStrings(overlay.IntendedDeploymentContexts)
	}
	if len(overlay.Capabilities) > 0 {
		base.Capabilities = copyStrings(overlay.Capabilities)
	}
	if len(overlay.ApprovedEnvironments) > 0 {
		base.ApprovedEnvironments = copyStrings(overlay.ApprovedEnvironments)
	}

	// extensions replaces whole, by the same rule as arrays.
	if len(overlay.Extensions) > 0 {
		base.Extensions = copyAnyMap(overlay.Extensions)
	}

	// vocabularies MERGES, and is the one field here that does.
	//
	// The spec's rule enumerates what replaces whole — "arrays and extensions" —
	// and vocabularies is a container that is not in that list. It is a prefix
	// to IRI map that makes CURIEs resolvable, so replacing it would put an
	// agent's inherited values out of scope: a pack declaring `acme:` and an
	// agent declaring only `other:` would leave the pack's own
	// `risk_classification: acme:tier-3` unresolvable on that agent. Prefix maps
	// accumulate in every other CURIE system for the same reason.
	//
	// An agent still wins on a prefix it redeclares, which is what lets it point
	// a name at a different IRI.
	base.Vocabularies = mergeVocabularies(base.Vocabularies, overlay.Vocabularies)

	return base
}

// lookupAgent finds an agent definition by name.
func lookupAgent(pack *Pack, agent string) (*packspec.AgentDef, error) {
	if pack == nil {
		return nil, fmt.Errorf("governance: no pack")
	}
	if pack.Agents == nil || len(pack.Agents.Members) == 0 {
		return nil, fmt.Errorf("governance: pack declares no agents, so %q is unknown", agent)
	}
	def, ok := pack.Agents.Members[agent]
	if !ok {
		return nil, fmt.Errorf("governance: no agent %q in this pack (have: %s)",
			agent, strings.Join(agentNames(pack), ", "))
	}
	if def == nil {
		return nil, fmt.Errorf("governance: agent %q is declared but empty", agent)
	}
	return def, nil
}

// agentNames lists the pack's agents, sorted, for error messages.
func agentNames(pack *Pack) []string {
	if pack == nil || pack.Agents == nil {
		return nil
	}
	names := make([]string, 0, len(pack.Agents.Members))
	for name := range pack.Agents.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DescribeGovernance renders a governance declaration as a short human-readable
// summary, for logs and for the operator who has to answer "what does this
// thing claim about itself".
//
// Only declared fields appear: an undeclared field is not the same as a default,
// and printing "autonomy: unknown" invites reading absence as a value.
func DescribeGovernance(g *Governance) string {
	if g == nil {
		return ""
	}
	var parts []string
	add := func(label, value string) {
		if value != "" {
			parts = append(parts, label+": "+value)
		}
	}
	add("purpose", g.IntendedPurpose)
	add("autonomy", g.AutonomyLevel)
	add("owner", g.AccountableOwner)
	add("operator role", g.OperatorRole)
	add("risk", g.RiskClassification)
	if g.RequiresAIDisclosure != nil {
		if *g.RequiresAIDisclosure {
			parts = append(parts, "must disclose as AI")
		} else {
			parts = append(parts, "AI disclosure not required")
		}
	}
	add("contexts", strings.Join(g.IntendedDeploymentContexts, "/"))
	add("environments", strings.Join(g.ApprovedEnvironments, "/"))
	add("capabilities", strings.Join(g.Capabilities, "/"))

	return strings.Join(parts, "; ")
}

// mergeVocabularies layers an agent's prefix map over the pack's. Prefixes the
// agent does not mention are inherited; ones it does are rebound to its IRI.
func mergeVocabularies(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(overlay))
	for prefix, iri := range base {
		out[prefix] = iri
	}
	for prefix, iri := range overlay {
		out[prefix] = iri
	}
	return out
}

func copyGovernance(g *Governance) *Governance {
	if g == nil {
		return nil
	}
	out := *g
	out.ForeseeableMisuse = copyStrings(g.ForeseeableMisuse)
	out.IntendedDeploymentContexts = copyStrings(g.IntendedDeploymentContexts)
	out.Capabilities = copyStrings(g.Capabilities)
	out.ApprovedEnvironments = copyStrings(g.ApprovedEnvironments)
	out.Vocabularies = copyStringMap(g.Vocabularies)
	out.Extensions = copyAnyMap(g.Extensions)
	if g.RequiresAIDisclosure != nil {
		out.RequiresAIDisclosure = packspec.Ptr(*g.RequiresAIDisclosure)
	}
	return &out
}

func copyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
