package engine

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

const (
	opLessThan            = "less_than"
	opLessThanOrEquals    = "less_than_or_equals"
	opGreaterThan         = "greater_than"
	opGreaterThanOrEquals = "greater_than_or_equals"
)

// evalPredicate evaluates a constrained predicate against scope. Exactly one
// variant is set on p (enforced by composition.Validate); precedence here favors
// composites, then exists, then compare.
func evalPredicate(p *composition.Predicate, scope Scope) (bool, error) {
	if p == nil {
		return false, fmt.Errorf("nil predicate")
	}
	if p.AllOf != nil {
		return evalAllOf(p.AllOf, scope)
	}
	if p.AnyOf != nil {
		return evalAnyOf(p.AnyOf, scope)
	}
	if p.Not != nil {
		ok, err := evalPredicate(p.Not, scope)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
	if p.Exists != nil {
		_, found := resolvePath(p.Path, scope)
		return found == *p.Exists, nil
	}
	return evalCompare(p, scope)
}

func evalAllOf(preds []*composition.Predicate, scope Scope) (bool, error) {
	for _, sub := range preds {
		ok, err := evalPredicate(sub, scope)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func evalAnyOf(preds []*composition.Predicate, scope Scope) (bool, error) {
	for _, sub := range preds {
		ok, err := evalPredicate(sub, scope)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func evalCompare(p *composition.Predicate, scope Scope) (bool, error) {
	actual, found := resolvePath(p.Path, scope)
	if !found {
		// A missing path is not an error for compare: equals/in are false,
		// not_equals/not_in are true. Mirror that with nil actual. Warn, because
		// an unresolvable compare path usually means a misconfigured reference or
		// an upstream step output that wasn't the expected shape (e.g. a provider
		// returned non-JSON for a step with an output_schema), which silently
		// sends a branch down its else path.
		logger.Warn("composition branch predicate path did not resolve; treating value as null",
			"path", p.Path, "op", p.Op)
		actual = nil
	}
	switch p.Op {
	case "equals":
		return jsonEqual(actual, p.Value), nil
	case "not_equals":
		return !jsonEqual(actual, p.Value), nil
	case "in":
		return inList(actual, p.Value)
	case "not_in":
		ok, err := inList(actual, p.Value)
		if err != nil {
			return false, err
		}
		return !ok, nil
	case opLessThan, opLessThanOrEquals, opGreaterThan, opGreaterThanOrEquals:
		return compareOrdered(p.Op, actual, p.Value)
	default:
		return false, fmt.Errorf("unknown predicate op %q", p.Op)
	}
}

// jsonEqual compares two decoded-JSON values. Numbers compare numerically; other
// values use reflect.DeepEqual.
func jsonEqual(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return reflect.DeepEqual(a, b)
}

func inList(actual, value any) (bool, error) {
	list, ok := value.([]any)
	if !ok {
		return false, fmt.Errorf("in/not_in requires a list value, got %T", value)
	}
	for _, item := range list {
		if jsonEqual(actual, item) {
			return true, nil
		}
	}
	return false, nil
}

func compareOrdered(op string, actual, value any) (bool, error) {
	af, aok := toFloat(actual)
	bf, bok := toFloat(value)
	if !aok || !bok {
		return false, fmt.Errorf("ordered comparison %q requires numeric operands, got %T and %T", op, actual, value)
	}
	switch op {
	case opLessThan:
		return af < bf, nil
	case opLessThanOrEquals:
		return af <= bf, nil
	case opGreaterThan:
		return af > bf, nil
	case opGreaterThanOrEquals:
		return af >= bf, nil
	}
	return false, fmt.Errorf("unknown ordered op %q", op)
}

// toFloat coerces JSON-decoded numerics (float64, plus int kinds for safety) to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}
