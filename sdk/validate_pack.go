package sdk

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// PackIssue describes a single semantic problem with a loaded pack.
// Structural issues (malformed JSON, missing required fields, schema
// violations) are returned as an error from ValidatePack, not as
// PackIssues.
type PackIssue struct {
	// Severity is "error" for all current issues — each one would cause
	// the corresponding validator or eval to be warn-and-skipped by
	// sdk.Open() or fail-fast by Arena.
	Severity string

	// Kind identifies the subsystem: "validator" or "eval".
	Kind string

	// PromptID is the prompt name this issue came from. Empty for
	// pack-level evals that apply to all prompts.
	PromptID string

	// Index is the position within the prompt's validators or evals slice.
	Index int

	// Type is the handler type, e.g. "max_length" or "content_excludes".
	Type string

	// ID is the eval def ID. Empty for validators.
	ID string

	// Reason is a human-readable explanation of the issue.
	Reason string
}

// String formats a PackIssue for logging or CLI output.
func (p PackIssue) String() string {
	loc := p.Kind
	if p.PromptID != "" {
		loc = fmt.Sprintf("prompts.%s.%ss[%d]", p.PromptID, p.Kind, p.Index)
	}
	tag := p.Type
	if p.ID != "" {
		tag = fmt.Sprintf("%s id=%s", p.Type, p.ID)
	}
	return fmt.Sprintf("%s %s: %s (%s)", p.Severity, loc, p.Reason, tag)
}

// ValidatePack loads the pack at path and reports any semantic issues —
// unknown validator/eval types, missing required params — that would
// cause Open() to warn-and-skip them or Arena to fail fast.
//
// When skipSchemaValidation is false (the default for callers who pass
// the zero value), ValidatePack runs strict promptpack JSON schema
// validation against the embedded schema. A pack that fails schema
// validation (for example, a validator declaring a forbidden field like
// "monitor") is returned as a non-nil error — not as PackIssues —
// because the file itself is non-spec. Pass true to bypass this and
// check only handler-level issues.
//
// Returns (nil, nil) if the pack is fully valid.
// Returns (nil, err) if the pack file is missing, unreadable, fails
// JSON parse, or fails schema validation (when strict). These are
// considered fatal and distinct from semantic issues.
// Returns (issues, nil) if the pack loads cleanly but has semantic
// problems (unknown validator/eval types, missing required params)
// the caller should address.
//
// This is a pre-flight check for CI gates and operator tools. It runs
// the same handler-level validation the SDK runs internally during
// Open(), exposed as a standalone function.
func ValidatePack(path string, skipSchemaValidation bool) ([]PackIssue, error) {
	loaded, err := pack.Load(path, pack.LoadOptions{
		SkipSchemaValidation: skipSchemaValidation,
	})
	if err != nil {
		return nil, err
	}

	registry := evals.NewEvalTypeRegistry()
	var issues []PackIssue

	// Per-prompt validators.
	for promptID, promptDef := range loaded.Prompts {
		if promptDef == nil {
			continue
		}
		issues = append(issues, validatePromptValidators(promptID, promptDef.Validators, registry)...)
	}

	// Pack-level evals (apply to all prompts, PromptID="").
	issues = append(issues, validateEvalDefs("", loaded.Evals, registry)...)

	// Per-prompt evals.
	for promptID, promptDef := range loaded.Prompts {
		if promptDef == nil {
			continue
		}
		issues = append(issues, validateEvalDefs(promptID, promptDef.Evals, registry)...)
	}

	return issues, nil
}

// validatePromptValidators dry-run constructs each enabled validator
// through NewGuardrailHookFromRegistry — that function already performs
// type lookup, param normalisation, and ParamValidator checks. Disabled
// validators are skipped (matching convertPackValidatorsToHooks behavior
// at Open() time).
func validatePromptValidators(
	promptID string, vs []pack.Validator, registry *evals.EvalTypeRegistry,
) []PackIssue {
	var issues []PackIssue
	for i, v := range vs {
		if !v.Enabled {
			continue
		}
		if _, err := guardrails.NewGuardrailHookFromRegistry(v.Type, v.Params, registry); err != nil {
			issues = append(issues, PackIssue{
				Severity: "error",
				Kind:     "validator",
				PromptID: promptID,
				Index:    i,
				Type:     v.Type,
				Reason:   cleanGuardrailError(err.Error()),
			})
		}
	}
	return issues
}

// validateEvalDefs runs evals.ValidateEvalTypes on the given defs and
// converts each returned error string into a PackIssue. promptID is the
// owning prompt name, or "" for pack-level evals.
func validateEvalDefs(
	promptID string, defs []evals.EvalDef, registry *evals.EvalTypeRegistry,
) []PackIssue {
	errs := evals.ValidateEvalTypes(defs, registry)
	if len(errs) == 0 {
		return nil
	}
	issues := make([]PackIssue, 0, len(errs))
	for _, e := range errs {
		id, reason := parseEvalValidationError(e)
		issues = append(issues, PackIssue{
			Severity: "error",
			Kind:     "eval",
			PromptID: promptID,
			ID:       id,
			Type:     findEvalTypeByID(defs, id),
			Reason:   reason,
		})
	}
	return issues
}

// cleanGuardrailError strips the `guardrail "<type>": ` prefix that
// NewGuardrailHookFromRegistry wraps around ParamValidator errors so
// the Reason field carries just the handler's message. The unknown
// type error has a different shape (`unknown guardrail type: "..."`)
// and is returned unchanged — still contains the word "unknown".
func cleanGuardrailError(msg string) string {
	const prefix = "guardrail "
	if !strings.HasPrefix(msg, prefix) {
		return msg
	}
	rest := msg[len(prefix):]
	end := strings.Index(rest, `": `)
	if end < 0 {
		return msg
	}
	return strings.TrimSpace(rest[end+3:])
}

// findEvalTypeByID returns the Type field of an EvalDef with the given
// ID, or an empty string if not found. Iterates by index to avoid
// copying the EvalDef struct on each iteration.
func findEvalTypeByID(defs []evals.EvalDef, id string) string {
	for i := range defs {
		if defs[i].ID == id {
			return defs[i].Type
		}
	}
	return ""
}
