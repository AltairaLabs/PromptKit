package evals

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ValidateHandlerParams runs the load-time param checks for one declared check:
// the handler's own ValidateParams when it implements ParamValidator, then,
// when it implements ParamNamer, a check that every key is one it reads.
// evalType is the name the check was declared under (it may be an alias) and
// is used in the error. Params must already be normalized (ApplyDefaults and
// NormalizeParams).
//
// allowed names keys the caller consumes itself and the handler need not list,
// such as a guardrail's direction and message. min_score and max_score are
// always allowed: they are the wrappers' thresholds, and the handlers that must
// not see them reject them with their own message.
func ValidateHandlerParams(
	evalType string, handler EvalTypeHandler, params map[string]any, allowed ...string,
) error {
	if pv, ok := handler.(ParamValidator); ok {
		if err := pv.ValidateParams(params); err != nil {
			return err
		}
	}
	return checkUnknownParams(evalType, handler, params, allowed)
}

// checkUnknownParams reports every key in params the handler does not read.
// A handler that does not implement ParamNamer is not checked.
func checkUnknownParams(evalType string, handler EvalTypeHandler, params map[string]any, allowed []string) error {
	pn, ok := handler.(ParamNamer)
	if !ok {
		return nil
	}
	known := pn.ParamNames()
	var unknown []string
	for key := range params {
		if slices.Contains(known, key) || slices.Contains(allowed, key) ||
			slices.Contains(scoreThresholdParamKeys, key) {
			continue
		}
		unknown = append(unknown, fmt.Sprintf("%q", key))
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	name := evalType
	if canonical := handler.Type(); canonical != evalType {
		name = fmt.Sprintf("%s (%s)", evalType, canonical)
	}
	noun := "param"
	if len(unknown) > 1 {
		noun = "params"
	}
	msg := fmt.Sprintf("unknown %s %s for %s", noun, strings.Join(unknown, ", "), name)
	if len(known) == 0 {
		return fmt.Errorf("%s, which takes no params", msg)
	}
	sorted := append([]string{}, known...)
	sort.Strings(sorted)
	return fmt.Errorf("%s; valid params: %s", msg, strings.Join(sorted, ", "))
}
