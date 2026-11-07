package evals

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// prometheusNameRe matches valid Prometheus metric names.
var prometheusNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// prometheusLabelRe matches valid Prometheus label names.
var prometheusLabelRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// EvalFindingCode identifies what a ValidateEvalsDetailed finding is about.
type EvalFindingCode string

// The codes ValidateEvalsDetailed reports.
const (
	EvalFindingIDRequired          EvalFindingCode = "id_required"
	EvalFindingDuplicateID         EvalFindingCode = "duplicate_id"
	EvalFindingTypeRequired        EvalFindingCode = "type_required"
	EvalFindingTriggerRequired     EvalFindingCode = "trigger_required"
	EvalFindingUnknownTrigger      EvalFindingCode = "unknown_trigger"
	EvalFindingSamplePercentage    EvalFindingCode = "sample_percentage_out_of_range"
	EvalFindingMetricNameRequired  EvalFindingCode = "metric_name_required"
	EvalFindingMetricNameInvalid   EvalFindingCode = "metric_name_invalid"
	EvalFindingMetricTypeRequired  EvalFindingCode = "metric_type_required"
	EvalFindingMetricTypeInvalid   EvalFindingCode = "metric_type_invalid"
	EvalFindingMetricRangeInverted EvalFindingCode = "metric_range_inverted"
	EvalFindingMetricLabelInvalid  EvalFindingCode = "metric_label_invalid"
	EvalFindingMetricLabelReserved EvalFindingCode = "metric_label_reserved"
	EvalFindingWhenUnusable        EvalFindingCode = "when_unusable"
)

// EvalFindingSeverity is how serious a ValidateEvalsDetailed finding is.
type EvalFindingSeverity string

const (
	// EvalFindingError marks a def that is wrong.
	EvalFindingError EvalFindingSeverity = "error"
	// EvalFindingWarning marks a def that is valid but cannot run in this
	// runtime: a trigger it does not know. The PromptPack schema lists trigger
	// values as examples, not an enum, so such a pack is spec-valid.
	EvalFindingWarning EvalFindingSeverity = "warning"
)

// EvalFinding is one problem ValidateEvalsDetailed found with a def.
type EvalFinding struct {
	// Index is the def's position in the slice that was validated.
	Index int
	// ID is the def's id, empty when it has none.
	ID string
	// Code says what the finding is about.
	Code EvalFindingCode
	// Severity is EvalFindingWarning for EvalFindingUnknownTrigger and
	// EvalFindingError for every other code.
	Severity EvalFindingSeverity
	// Reason is the finding without the scope and index prefix.
	Reason string
	// Message is the finding as ValidateEvals words it:
	// "<scope> evals[<i>] (id=<quoted id>): <reason>", or
	// "<scope> evals[<i>]: <reason>" for a def without an id.
	Message string
}

// severityForCode returns the severity of a finding with the given code.
func severityForCode(code EvalFindingCode) EvalFindingSeverity {
	if code == EvalFindingUnknownTrigger {
		return EvalFindingWarning
	}
	return EvalFindingError
}

// ValidateEvals validates a slice of EvalDef for correctness and returns each
// finding's Message. See ValidateEvalsDetailed, which returns the same findings
// with the def they name, a code and a severity.
// The scope parameter is used in error messages (e.g. "pack", "prompt:foo").
// prompt.Pack.Validate reports its findings, and the SDK's Open skips each def
// it rejects, with a warning.
// It checks:
//   - IDs are non-empty and unique within the slice
//   - Type is non-empty
//   - Trigger is a valid value
//   - sample_percentage (if set) is in [0, 100]
//   - Metric name matches Prometheus naming regex
//   - Metric type is one of gauge/counter/histogram/boolean
//   - Metric label names match the Prometheus label regex and do not start with "__"
//   - when contains only supported conditions of the right type
func ValidateEvals(defs []EvalDef, scope string) []string {
	findings := ValidateEvalsDetailed(defs, scope)
	if len(findings) == 0 {
		return nil
	}
	errs := make([]string, len(findings))
	for i := range findings {
		errs[i] = findings[i].Message
	}
	return errs
}

// ValidateEvalsDetailed runs the checks ValidateEvals runs and returns each
// finding with the index and id of the def it names, a code and a severity, in
// def order. Classify a finding by its Code or Severity, never by its text.
func ValidateEvalsDetailed(defs []EvalDef, scope string) []EvalFinding {
	var findings []EvalFinding
	seen := make(map[string]bool, len(defs))

	for i := range defs {
		def := &defs[i]
		prefix := fmt.Sprintf("%s evals[%d]", scope, i)
		if def.ID != "" {
			prefix = fmt.Sprintf("%s evals[%d] (id=%q)", scope, i, def.ID)
		}
		add := func(code EvalFindingCode, reason string) {
			findings = append(findings, EvalFinding{
				Index:    i,
				ID:       def.ID,
				Code:     code,
				Severity: severityForCode(code),
				Reason:   reason,
				Message:  prefix + ": " + reason,
			})
		}
		validateEvalID(def, seen, add)
		validateEvalFields(def, add)
	}

	return findings
}

// addFinding records one finding about the def being validated.
type addFinding func(code EvalFindingCode, reason string)

// validateEvalID checks the eval ID for presence and uniqueness.
func validateEvalID(def *EvalDef, seen map[string]bool, add addFinding) {
	if def.ID == "" {
		add(EvalFindingIDRequired, "id is required")
		return
	}
	if seen[def.ID] {
		add(EvalFindingDuplicateID, fmt.Sprintf("duplicate eval id %q", def.ID))
	}
	seen[def.ID] = true
}

// validateEvalFields checks type, trigger, sample_percentage, metric and when.
func validateEvalFields(def *EvalDef, add addFinding) {
	if def.Type == "" {
		add(EvalFindingTypeRequired, "type is required")
	}

	if def.Trigger == "" {
		add(EvalFindingTriggerRequired, "trigger is required")
	} else if !ValidTriggers[EvalTrigger(def.Trigger)] {
		add(EvalFindingUnknownTrigger, fmt.Sprintf("invalid trigger %q", def.Trigger))
	}

	if def.SamplePercentage != nil {
		pct := *def.SamplePercentage
		if pct < 0 || pct > 100 {
			add(EvalFindingSamplePercentage, fmt.Sprintf(
				"sample_percentage must be between 0 and 100, got %g", pct))
		}
	}

	if def.Metric != nil {
		validateMetric(def.Metric, add)
	}

	if err := ValidateEvalWhen(def.When); err != nil {
		add(EvalFindingWhenUnusable, err.Error())
	}
}

// ValidateEvalTypes checks that every EvalDef's Type has a registered handler
// in the given registry, AND (when the handler implements ParamValidator)
// that its params are usable by the handler, AND (when it implements
// ParamNamer) that every param is one the handler reads. Returns a list of
// human-readable error strings for any type-lookup or param-validation
// failures. See ValidateHandlerParams.
//
// Params are normalized (ApplyDefaults + NormalizeParams) before calling
// ValidateParams, so handlers only need to check canonical key names.
//
// It also reports a Threshold an assertion or guardrail wrapper cannot apply
// (an unknown operator, a missing value, or one declared alongside min_score or
// max_score), and an unusable `when`. The SDK exposes it as a preflight check,
// and its eval middleware skips defs that fail it, with a warning.
func ValidateEvalTypes(defs []EvalDef, registry *EvalTypeRegistry) []string {
	logger.Debug("evals: validating eval types", "def_count", len(defs), "registered_types", registry.Types())
	var errs []string
	for i := range defs {
		def := &defs[i]
		// `when` is checked here as well as in ValidateEvals because a caller
		// may run only this check, and a diagnostic only ValidateEvals raised
		// would reach nobody there (#1931).
		if werr := ValidateEvalWhen(def.When); werr != nil {
			errs = append(errs, fmt.Sprintf("eval %q: %s", def.ID, werr))
		}
		if def.Type == "" {
			continue
		}
		handler, err := registry.Get(def.Type)
		if err != nil {
			errs = append(errs, fmt.Sprintf(
				"eval %q: unknown type %q (registered types: %v)",
				def.ID, def.Type, registry.Types(),
			))
			continue
		}
		if terr := checkWrapperThreshold(handler, def); terr != nil {
			errs = append(errs, fmt.Sprintf("eval %q: %s", def.ID, terr))
		}
		normalized := ApplyDefaults(def.Type, def.Params)
		normalized = NormalizeParams(def.Type, normalized)
		if perr := ValidateHandlerParams(def.Type, handler, normalized); perr != nil {
			errs = append(errs, fmt.Sprintf(
				"eval %q: %s", def.ID, perr.Error(),
			))
		}
	}
	return errs
}

// validateMetric validates a MetricDef within an eval.
func validateMetric(m *MetricDef, add addFinding) {
	if m.Name == "" {
		add(EvalFindingMetricNameRequired, "metric.name is required")
	} else if !prometheusNameRe.MatchString(m.Name) {
		add(EvalFindingMetricNameInvalid, fmt.Sprintf(
			"metric.name %q must match Prometheus naming: %s", m.Name, prometheusNameRe.String()))
	}

	if m.Type == "" {
		add(EvalFindingMetricTypeRequired, "metric.type is required")
	} else if !ValidMetricTypes[m.Type] {
		add(EvalFindingMetricTypeInvalid, fmt.Sprintf("invalid metric.type %q", m.Type))
	}

	if m.Range != nil && m.Range.Min != nil && m.Range.Max != nil && *m.Range.Min > *m.Range.Max {
		add(EvalFindingMetricRangeInverted, fmt.Sprintf(
			"metric.range.min (%g) must be <= range.max (%g)", *m.Range.Min, *m.Range.Max))
	}

	for _, labelName := range slices.Sorted(maps.Keys(MetricLabels(m))) {
		if !prometheusLabelRe.MatchString(labelName) {
			add(EvalFindingMetricLabelInvalid, fmt.Sprintf(
				"metric.labels key %q must match Prometheus label naming: %s",
				labelName, prometheusLabelRe.String()))
		}
		if strings.HasPrefix(labelName, "__") {
			add(EvalFindingMetricLabelReserved, fmt.Sprintf(
				"metric.labels key %q must not start with \"__\" (reserved by Prometheus)", labelName))
		}
	}
}
