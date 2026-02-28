package evals

import (
	"fmt"
	"regexp"
)

// prometheusNameRe matches valid Prometheus metric names.
var prometheusNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// ValidateEvals validates a slice of EvalDef for correctness.
// The scope parameter is used in error messages (e.g. "pack", "prompt:foo").
// It checks:
//   - IDs are non-empty and unique within the slice
//   - Type is non-empty
//   - Trigger is a valid value
//   - sample_percentage (if set) is in [0, 100]
//   - Metric name matches Prometheus naming regex
//   - Metric type is one of gauge/counter/histogram/boolean
func ValidateEvals(defs []EvalDef, scope string) []string {
	var errs []string
	seen := make(map[string]bool, len(defs))

	for i, def := range defs {
		prefix := fmt.Sprintf("%s evals[%d]", scope, i)
		prefix, idErrs := validateEvalID(&def, prefix, scope, i, seen)
		errs = append(errs, idErrs...)
		errs = append(errs, validateEvalFields(&def, prefix)...)
	}

	return errs
}

// validateEvalID checks the eval ID for presence and uniqueness.
// It returns the (possibly updated) prefix and any errors found.
func validateEvalID(
	def *EvalDef, prefix, scope string, idx int, seen map[string]bool,
) (updatedPrefix string, errs []string) {
	if def.ID == "" {
		return prefix, []string{
			fmt.Sprintf("%s: id is required", prefix),
		}
	}

	updatedPrefix = fmt.Sprintf("%s evals[%d] (id=%q)", scope, idx, def.ID)
	if seen[def.ID] {
		errs = append(errs, fmt.Sprintf(
			"%s: duplicate eval id %q", updatedPrefix, def.ID,
		))
	}
	seen[def.ID] = true
	return updatedPrefix, errs
}

// validateEvalFields checks type, trigger, sample_percentage, and metric.
func validateEvalFields(def *EvalDef, prefix string) []string {
	var errs []string

	if def.Type == "" {
		errs = append(errs, fmt.Sprintf("%s: type is required", prefix))
	}

	if def.Trigger == "" {
		errs = append(errs, fmt.Sprintf(
			"%s: trigger is required", prefix,
		))
	} else if !ValidTriggers[def.Trigger] {
		errs = append(errs, fmt.Sprintf(
			"%s: invalid trigger %q", prefix, def.Trigger,
		))
	}

	if def.SamplePercentage != nil {
		pct := *def.SamplePercentage
		if pct < 0 || pct > 100 {
			errs = append(errs, fmt.Sprintf(
				"%s: sample_percentage must be between 0 and 100, got %g",
				prefix, pct,
			))
		}
	}

	if def.Metric != nil {
		errs = append(errs, validateMetric(def.Metric, prefix)...)
	}

	return errs
}

// ValidateEvalTypes checks that every EvalDef's Type has a registered handler
// in the given registry. Returns a list of human-readable error strings for
// any unknown types. This is safe to call from any package that has access
// to both the defs and the registry.
func ValidateEvalTypes(defs []EvalDef, registry *EvalTypeRegistry) []string {
	var errs []string
	for i := range defs {
		if defs[i].Type != "" && !registry.Has(defs[i].Type) {
			errs = append(errs, fmt.Sprintf(
				"eval %q: unknown type %q (registered types: %v)",
				defs[i].ID, defs[i].Type, registry.Types(),
			))
		}
	}
	return errs
}

// validateMetric validates a MetricDef within an eval.
func validateMetric(m *MetricDef, prefix string) []string {
	var errs []string

	if m.Name == "" {
		errs = append(errs, fmt.Sprintf(
			"%s: metric.name is required", prefix,
		))
	} else if !prometheusNameRe.MatchString(m.Name) {
		errs = append(errs, fmt.Sprintf(
			"%s: metric.name %q must match Prometheus naming: %s",
			prefix, m.Name, prometheusNameRe.String(),
		))
	}

	if m.Type == "" {
		errs = append(errs, fmt.Sprintf(
			"%s: metric.type is required", prefix,
		))
	} else if !ValidMetricTypes[m.Type] {
		errs = append(errs, fmt.Sprintf(
			"%s: invalid metric.type %q", prefix, m.Type,
		))
	}

	if m.Range != nil && m.Range.Min != nil && m.Range.Max != nil {
		if *m.Range.Min > *m.Range.Max {
			errs = append(errs, fmt.Sprintf(
				"%s: metric.range.min (%g) must be <= range.max (%g)",
				prefix, *m.Range.Min, *m.Range.Max,
			))
		}
	}

	return errs
}
