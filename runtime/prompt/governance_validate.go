package prompt

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// RFC 0016 validation rules.
//
// RFC 0016 defines references between governance declarations and the rest of
// the pack that JSON Schema cannot express: an obligation control names a
// validator or an eval by id, a review names the obligations it satisfies. A
// pack whose references do not resolve is invalid, the same way a workflow
// state naming a missing prompt is invalid. These are pack-validity checks, not
// policy: nothing here decides whether an obligation applies or is met.
//
// Rule 9 (warn when a review owner resolves to a named individual) needs a
// directory PromptKit does not have, so it is not checked.

// GovernanceValidationResult carries the outcome of ValidateGovernance. An
// error makes the pack invalid; a warning does not.
type GovernanceValidationResult struct {
	Errors   []string
	Warnings []string
}

func (r *GovernanceValidationResult) errorf(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

func (r *GovernanceValidationResult) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// cadencePattern is the schema's pattern for reviews[].cadence (RFC 0016 rule
// 8): an ISO 8601 duration that is not the empty duration "P". The schema
// enforces it too; this copy covers packs loaded with schema validation
// skipped. TestCadencePatternMatchesTheEmbeddedSchema keeps the two equal.
const cadencePattern = `^P(?:(?:\d+Y(?:\d+M)?(?:\d+W)?(?:\d+D)?|\d+M(?:\d+W)?(?:\d+D)?|\d+W(?:\d+D)?|\d+D)` +
	`(?:T(?:\d+H(?:\d+M)?(?:\d+S)?|\d+M(?:\d+S)?|\d+S))?|T(?:\d+H(?:\d+M)?(?:\d+S)?|\d+M(?:\d+S)?|\d+S))$`

var cadenceRe = regexp.MustCompile(cadencePattern)

// wellKnownPrefixes are the CURIE prefixes RFC 0013 and RFC 0016 define, which
// a pack may use without declaring them in vocabularies.
var wellKnownPrefixes = map[string]bool{
	"dpv": true, "eu-aiact": true, "ai": true,
	"pd": true, "risk": true, "tech": true,
	"legal-eu-gdpr": true, "legal-us": true,
	"sector-health": true, "sector-finance": true, "sector-education": true,
	"sector-law": true, "sector-publicservices": true, "sector-infra": true,
	"hipaa": true, "pp": true,
}

// governanceFields is every property of a governance object, by JSON name.
// Read from the generated type so a property a later spec release adds is
// known here without a code change.
var governanceFields = func() map[string]bool {
	fields := map[string]bool{}
	t := reflect.TypeOf(packspec.Governance{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}()

const packGovernanceLoc = "metadata.governance"

// ValidateGovernance checks the RFC 0016 rules JSON Schema cannot express.
//
// Rules 2, 4, 5 and 8 apply to each governance object as written: the pack's
// and each agent's. Rules 6 and 7 apply to each effective object — the pack's,
// and each agent's resolved against it — because a control or review can
// legitimately rely on a field or obligation the agent inherits. Rule 10's
// undeclared-prefix check is a warning, never an error.
//
// A pack declaring no governance produces an empty result unless two of its
// validators share an id, which rule 5 forbids whether or not anything
// references them.
func (p *Pack) ValidateGovernance() *GovernanceValidationResult {
	res := &GovernanceValidationResult{}
	if p == nil {
		return res
	}

	validatorIDs := p.collectValidatorIDs(res)
	evalIDs := p.collectEvalIDs()

	var packGov *Governance
	if p.Metadata != nil {
		packGov = p.Metadata.Governance
	}
	if packGov != nil {
		validateDeclaredGovernance(res, packGovernanceLoc, packGov, validatorIDs, evalIDs)
		validateEffectiveGovernance(res, packGovernanceLoc, packGov, true)
		warnUndeclaredPrefixes(res, packGovernanceLoc, packGov, packGov.Vocabularies)
	}

	for _, name := range agentNames(p) {
		def := p.Agents.Members[name]
		if def == nil || def.Governance == nil {
			// Nothing of its own: the effective object is the pack's, which
			// is already checked.
			continue
		}
		loc := fmt.Sprintf("agents.members[%q].governance", name)
		validateDeclaredGovernance(res, loc, def.Governance, validatorIDs, evalIDs)

		effective, err := ResolveGovernance(p, name)
		if err != nil {
			continue
		}
		// Rules 6 and 7 only re-read what the agent changes. Obligations and
		// reviews it inherits are the pack's, already checked against a
		// declared-field set the agent can only grow, so re-checking them
		// would repeat the pack's errors once per agent.
		own := def.Governance
		if len(own.Obligations) > 0 || len(own.Reviews) > 0 {
			validateEffectiveGovernance(res, loc+" (effective)", effective, len(own.Obligations) > 0)
		}
		warnUndeclaredPrefixes(res, loc, def.Governance, effective.Vocabularies)
	}

	return res
}

// collectValidatorIDs gathers every Validator.id in the pack, reporting an id
// declared more than once (rule 5: unique across the pack).
func (p *Pack) collectValidatorIDs(res *GovernanceValidationResult) map[string]bool {
	ids := map[string]bool{}
	seenAt := map[string]string{}
	for _, promptName := range sortedKeys(p.Prompts) {
		pr := p.Prompts[promptName]
		if pr == nil {
			continue
		}
		for i, v := range pr.Validators {
			if v == nil || v.ID == "" {
				continue
			}
			loc := fmt.Sprintf("prompts[%q].validators[%d]", promptName, i)
			if first, dup := seenAt[v.ID]; dup {
				res.errorf("%s: validator id %q is already declared at %s; validator ids must be unique across the pack",
					loc, v.ID, first)
				continue
			}
			seenAt[v.ID] = loc
			ids[v.ID] = true
		}
	}
	return ids
}

// collectEvalIDs gathers the ids of the pack's evals and every prompt's evals,
// the two places rule 4 lets a reference resolve.
func (p *Pack) collectEvalIDs() map[string]bool {
	ids := map[string]bool{}
	for _, e := range p.Evals {
		if e != nil {
			ids[e.ID] = true
		}
	}
	for _, pr := range p.Prompts {
		if pr == nil {
			continue
		}
		for _, e := range pr.Evals {
			if e != nil {
				ids[e.ID] = true
			}
		}
	}
	return ids
}

// validateDeclaredGovernance applies the rules about a governance object as
// written: unique ids (2), resolvable eval and validator references (4, 5),
// and a valid review cadence (8).
func validateDeclaredGovernance(
	res *GovernanceValidationResult, loc string, g *Governance, validatorIDs, evalIDs map[string]bool,
) {
	seen := map[string]bool{}
	for i, o := range g.Obligations {
		if o == nil {
			continue
		}
		oloc := fmt.Sprintf("%s.obligations[%d]", loc, i)
		if seen[o.ID] {
			res.errorf("%s: obligation id %q is declared more than once in this governance object", oloc, o.ID)
		}
		seen[o.ID] = true
		validateControlRefs(res, oloc, o.Controls, validatorIDs, evalIDs)
	}

	seen = map[string]bool{}
	for i, r := range g.Reviews {
		if r == nil {
			continue
		}
		rloc := fmt.Sprintf("%s.reviews[%d]", loc, i)
		if seen[r.ID] {
			res.errorf("%s: review id %q is declared more than once in this governance object", rloc, r.ID)
		}
		seen[r.ID] = true
		if r.Eval != "" && !evalIDs[r.Eval] {
			res.errorf("%s: eval %q does not resolve to an eval in the pack's or a prompt's evals", rloc, r.Eval)
		}
		if !cadenceRe.MatchString(r.Cadence) {
			res.errorf("%s: cadence %q is not a non-empty ISO 8601 duration", rloc, r.Cadence)
		}
	}
}

// validateControlRefs checks an obligation's eval and validator controls
// resolve (rules 4 and 5).
func validateControlRefs(
	res *GovernanceValidationResult, oloc string, controls []*packspec.ObligationControl,
	validatorIDs, evalIDs map[string]bool,
) {
	for j, c := range controls {
		if c == nil {
			continue
		}
		cloc := fmt.Sprintf("%s.controls[%d]", oloc, j)
		if c.Eval != "" && !evalIDs[c.Eval] {
			res.errorf("%s: eval %q does not resolve to an eval in the pack's or a prompt's evals", cloc, c.Eval)
		}
		if c.Validator != "" && !validatorIDs[c.Validator] {
			res.errorf("%s: validator %q does not resolve to a validator id on any prompt", cloc, c.Validator)
		}
	}
}

// validateEffectiveGovernance applies the rules that read the effective
// object: a field control names a declared governance property (6), and a
// review's satisfies names an obligation in it (7). checkFields false skips
// rule 6, for obligations already checked where they were declared.
func validateEffectiveGovernance(res *GovernanceValidationResult, loc string, g *Governance, checkFields bool) {
	declared := declaredGovernanceFields(g)
	obligationIDs := map[string]bool{}
	for i, o := range g.Obligations {
		if o == nil {
			continue
		}
		obligationIDs[o.ID] = true
		if !checkFields {
			continue
		}
		validateFieldControls(res, fmt.Sprintf("%s.obligations[%d]", loc, i), o.Controls, declared)
	}

	for i, r := range g.Reviews {
		if r == nil {
			continue
		}
		for _, id := range r.Satisfies {
			if !obligationIDs[id] {
				res.errorf("%s.reviews[%d]: satisfies %q, which is not an obligation id here", loc, i, id)
			}
		}
	}
}

// validateFieldControls checks each field control names a governance property
// that the effective object declares (rule 6).
func validateFieldControls(
	res *GovernanceValidationResult, oloc string, controls []*packspec.ObligationControl, declared map[string]bool,
) {
	for j, c := range controls {
		if c == nil || c.Field == "" {
			continue
		}
		cloc := fmt.Sprintf("%s.controls[%d]", oloc, j)
		switch {
		case !governanceFields[c.Field]:
			res.errorf("%s: field %q is not a governance property", cloc, c.Field)
		case !declared[c.Field]:
			res.errorf("%s: field %q is not declared, so it cannot discharge the obligation", cloc, c.Field)
		}
	}
}

// declaredGovernanceFields reports which properties of g carry a value. It
// goes through JSON so "declared" means exactly what the overlay rule means by
// present: the generated type omits empty optional fields, and keeps an
// explicit requires_ai_disclosure: false.
func declaredGovernanceFields(g *Governance) map[string]bool {
	declared := map[string]bool{}
	data, err := json.Marshal(g)
	if err != nil {
		return declared
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return declared
	}
	for name := range fields {
		declared[name] = true
	}
	return declared
}

// warnUndeclaredPrefixes applies rule 10 to the term slots RFC 0016 adds: a
// value containing a colon is a CURIE, and one whose prefix is neither
// well-known nor in vocabularies is a warning. A value without a colon is a
// free string, and an absolute IRI is not a CURIE.
func warnUndeclaredPrefixes(res *GovernanceValidationResult, loc string, g *Governance, vocab map[string]string) {
	check := func(slot, value string) {
		prefix, ok := curiePrefix(value)
		if !ok || wellKnownPrefixes[prefix] {
			return
		}
		if _, declared := vocab[prefix]; declared {
			return
		}
		res.warnf("%s.%s: term %q uses prefix %q, which is neither well-known nor declared in vocabularies",
			loc, slot, value, prefix)
	}

	for i, o := range g.Obligations {
		if o == nil {
			continue
		}
		oloc := fmt.Sprintf("obligations[%d]", i)
		check(oloc+".obligation", o.Obligation)
		if o.AppliesTo != nil {
			check(oloc+".applies_to.capability", o.AppliesTo.Capability)
			check(oloc+".applies_to.data_class", o.AppliesTo.DataClass)
			check(oloc+".applies_to.risk_classification", o.AppliesTo.RiskClassification)
		}
	}
	for i, r := range g.Reviews {
		if r != nil {
			check(fmt.Sprintf("reviews[%d].type", i), r.Type)
		}
	}
}

// curiePrefixRe is a CURIE prefix: an NCName without the Unicode ranges. A
// colon after anything else ("Annual review: bias") is prose, not a CURIE.
var curiePrefixRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// curiePrefix returns the prefix of a CURIE value. ok is false for a free
// string (no colon, or a colon not preceded by a valid prefix) and for an
// absolute IRI ("https://…", "urn:…").
func curiePrefix(value string) (prefix string, ok bool) {
	prefix, rest, found := strings.Cut(value, ":")
	if !found || !curiePrefixRe.MatchString(prefix) {
		return "", false
	}
	if strings.HasPrefix(rest, "//") || strings.EqualFold(prefix, "urn") {
		return "", false
	}
	return prefix, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
