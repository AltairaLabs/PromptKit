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
// reference. Returns nil when the prompt declares none.
func checkProviderKeys(p *pack.Pack, prompt *pack.Prompt, cfg *config) error {
	refs := collectProviderKeyRefs(prompt, cfg)
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

// collectProviderKeyRefs walks the prompt's validators and evals for checks
// that name a provider.
func collectProviderKeyRefs(prompt *pack.Prompt, cfg *config) []providerKeyRef {
	if prompt == nil {
		return nil
	}
	registry := cfg.evalRegistry
	if registry == nil {
		registry = evals.NewEvalTypeRegistry()
	}

	var refs []providerKeyRef
	add := func(checkType string, params map[string]any) {
		key, _ := params[handlers.ProviderParam].(string)
		if key == "" {
			return
		}
		handler, err := registry.Get(checkType)
		if err != nil {
			// An unknown type is reported by the guardrail/eval compile paths
			// with a better message than anything this could say.
			return
		}
		switch {
		case handlers.RequiresJudge(handler):
			refs = append(refs, providerKeyRef{check: checkType, key: key,
				resolve: func(b evals.ProviderBinding) error { _, err := b.LLM(key); return err }})
		case handlers.RequiresClassifier(handler):
			refs = append(refs, providerKeyRef{check: checkType, key: key,
				resolve: func(b evals.ProviderBinding) error { _, err := b.Classifier(key); return err }})
		}
	}

	for _, v := range prompt.Validators {
		if v.Enabled != nil && !*v.Enabled {
			continue
		}
		add(v.Type, v.Params)
	}
	for _, e := range prompt.Evals {
		add(e.Type, e.Params)
	}
	return refs
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
