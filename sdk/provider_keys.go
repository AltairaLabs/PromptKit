package sdk

import (
	"errors"
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// Checks name the providers they need by the LOGICAL keys their own pack
// declares in `requires`. Everything in this file answers one question at
// load: can every one of those names be honored, by this pack and this host,
// before a single turn runs?
//
// Three ways it can fail, and they belong to different people:
//
//   - the name is not in the pack's requires block — the PACK author's error,
//     usually a typo, and nothing the host can fix;
//   - the host bound nothing to it — the HOST's error, and the requirements
//     gate already says so for declared requirements;
//   - the host bound something that cannot do the job — also the host's, and
//     the one that used to be invisible. A check whose key resolves to an
//     embedding provider is not "unconfigured", it is misconfigured, and
//     saying so is the difference between a five-minute fix and an afternoon.

// checkProviderKeys validates every logical provider name the prompt's checks
// reference, including the pack-level evals that run on its turns. Returns nil
// when none names a provider.
func checkProviderKeys(p *pack.Pack, prompt *pack.Prompt, cfg *config) error {
	refs := collectProviderKeyRefs(p, prompt, cfg)
	if len(refs) == 0 {
		return nil
	}

	declared, err := declaredRequirementKeys(p)
	if err != nil {
		return err
	}

	binding := newHostBinding(cfg)
	var problems []string

	for _, ref := range refs {
		if _, ok := declared[ref.key]; !ok {
			problems = append(problems, fmt.Sprintf(
				"check %q names provider %q, which the pack does not declare in requires "+
					"(declared: %s)", ref.check, ref.key, describeKeys(declared)))
			continue
		}
		if binding == nil {
			problems = append(problems, fmt.Sprintf(
				"check %q names provider %q and this conversation has no providers wired at all",
				ref.check, ref.key))
			continue
		}
		if err := ref.resolve(binding); err != nil {
			problems = append(problems, fmt.Sprintf(
				"check %q: provider %s", ref.check, evals.DescribeUnresolved(ref.key, err)))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w — pack checks cannot resolve their providers:\n  - %s",
			errProviderKeys, joinProblems(problems))
	}
	return nil
}

// providerKeyRef is one check's reference to one logical provider name, plus
// how to test that the binding can honor it — which differs by what the check
// needs, and is the whole point of checking at all.
type providerKeyRef struct {
	check   string
	key     string
	resolve func(evals.ProviderBinding) error
}

// collectProviderKeyRefs walks the prompt's validators and the evals that run
// on its turns for checks that name a provider. The evals are the pack-level
// and prompt-level ones merged the way the eval middleware merges them, so a
// prompt-level eval that overrides a pack-level one by ID is the one checked.
func collectProviderKeyRefs(p *pack.Pack, prompt *pack.Prompt, cfg *config) []providerKeyRef {
	checks := checksOnTurn(p, prompt)
	if len(checks) == 0 {
		return nil
	}
	registry := cfg.evalRegistry
	if registry == nil {
		registry = evals.NewEvalTypeRegistry()
	}
	var refs []providerKeyRef
	for _, c := range checks {
		if ref, ok := providerKeyRefFor(registry, c.checkType, c.params); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

// turnCheck is one validator or eval: its type and params.
type turnCheck struct {
	checkType string
	params    map[string]any
}

// checksOnTurn lists the enabled validators of the prompt and the evals that
// run on its turns (pack-level merged with prompt-level). Nil entries are
// skipped.
func checksOnTurn(p *pack.Pack, prompt *pack.Prompt) []turnCheck {
	var packEvals, promptEvals []evals.EvalDef
	var out []turnCheck
	if p != nil {
		packEvals = evals.Values(p.Evals)
	}
	if prompt != nil {
		promptEvals = evals.Values(prompt.Evals)
		for _, v := range prompt.Validators {
			if v == nil || (v.Enabled != nil && !*v.Enabled) {
				continue
			}
			out = append(out, turnCheck{checkType: v.Type, params: v.Params})
		}
	}
	merged := evals.ResolveEvals(packEvals, promptEvals)
	for i := range merged {
		out = append(out, turnCheck{checkType: merged[i].Type, params: merged[i].Params})
	}
	return out
}

// providerKeyRefFor returns the reference a check makes to a provider, and
// false when it names none or needs none.
func providerKeyRefFor(
	registry *evals.EvalTypeRegistry, checkType string, params map[string]any,
) (providerKeyRef, bool) {
	key, _ := params[handlers.ProviderParam].(string)
	if key == "" {
		return providerKeyRef{}, false
	}
	handler, err := registry.Get(checkType)
	if err != nil {
		// An unknown type is reported by the guardrail/eval compile paths
		// with a better message than anything this could say.
		return providerKeyRef{}, false
	}
	switch {
	case handlers.RequiresJudge(handler):
		return providerKeyRef{check: checkType, key: key,
			resolve: func(b evals.ProviderBinding) error { _, err := b.LLM(key); return err }}, true
	case handlers.RequiresClassifier(handler):
		return providerKeyRef{check: checkType, key: key,
			resolve: func(b evals.ProviderBinding) error { _, err := b.Classifier(key); return err }}, true
	}
	return providerKeyRef{}, false
}

// declaredRequirementKeys returns the logical names the pack declares.
func declaredRequirementKeys(p *pack.Pack) (map[string]rtprompt.ResolvedRequirement, error) {
	reqs, err := rtprompt.ResolveRequirements(p)
	if err != nil {
		return nil, fmt.Errorf("pack requires: %w", err)
	}
	out := make(map[string]rtprompt.ResolvedRequirement, len(reqs))
	for _, r := range reqs {
		out[r.Key] = r
	}
	return out, nil
}

func describeKeys(declared map[string]rtprompt.ResolvedRequirement) string {
	if len(declared) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return joinProblems(keys)
}

func joinProblems(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += "\n  - "
		}
		out += item
	}
	return out
}

// errProviderKeys is returned for any failure in this file, so callers can tell
// a provider-binding problem from other load failures.
var errProviderKeys = errors.New("check provider binding")
