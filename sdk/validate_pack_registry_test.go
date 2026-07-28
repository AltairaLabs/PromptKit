package sdk_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// Regression coverage for https://github.com/AltairaLabs/PromptKit/issues/1725.
//
// After #1724, Open() resolves an eval-backed guardrail against the registry
// supplied by sdk.WithEvalRegistry, so a pack validator naming a custom eval
// type builds and enforces. ValidatePack could not do the same — it always
// resolved against a freshly built default registry — so preflight reported a
// pack the runtime happily accepts. The failure mode is a FALSE POSITIVE, so
// these tests assert the issue is ABSENT, each paired with a control that keeps
// "report nothing at all" from passing.
//
// The custom type (customGuardrailType) and the registry carrying its handler
// (customEvalRegistry) are shared with guardrail_custom_registry_test.go.

// absentEvalType names a handler no registry in this package registers — not
// the built-in default set, and not customEvalRegistry either.
const absentEvalType = "test_absent_from_every_registry_1725"

// customTypePack returns a schema-valid pack whose default prompt declares
// BOTH a validator and an eval def naming typeName.
//
// Both halves matter: ValidatePack routes validators through
// validatePromptValidators and eval defs through validateEvalDefs, two
// separate helpers each taking their own registry argument. Threading the
// caller's registry into one and leaving the other on the default is the
// "fixed one sibling, left the other" shape this bug belongs to, so every
// assertion below counts issues across both.
func customTypePack(typeName, blockedMessage string) map[string]any {
	p := baseValidPack()
	prompts := p["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    typeName,
			"enabled": true,
			"params":  map[string]any{"message": blockedMessage},
		},
	}
	def["evals"] = []any{
		map[string]any{
			"id":      "custom-eval",
			"type":    typeName,
			"trigger": "every_turn",
			"params":  map[string]any{},
		},
	}
	return p
}

// unknownTypeIssueKinds returns the Kind of every issue whose Reason names an
// unknown type, so a test can assert which of the two preflight paths reported.
// Both producers — guardrails.ErrUnknownGuardrailType and
// evals.ValidateEvalTypes — spell it "unknown".
func unknownTypeIssueKinds(issues []sdk.PackIssue) []string {
	kinds := make([]string, 0, len(issues))
	for _, iss := range issues {
		if strings.Contains(iss.Reason, "unknown") {
			kinds = append(kinds, iss.Kind)
		}
	}
	return kinds
}

// TestValidatePackWithRegistry_CustomTypeAcceptedButDefaultStillReports is the
// core pair. The same pack file is checked twice, and the two halves have to
// disagree:
//
//   - with the caller's registry, the custom type is known and NOTHING is
//     reported. Mutation caught: ValidatePackWithRegistry ignoring its registry
//     argument for either the validator path or the eval path (an issue comes
//     back), and a nil-vs-supplied mix-up in evalRegistryOrDefault.
//   - with plain ValidatePack (default registry) the SAME pack still reports
//     both. Mutation caught: a "fix" that simply stopped reporting unknown
//     types, or dropped the dry-run construction entirely — that would make the
//     first half pass while quietly disabling preflight.
func TestValidatePackWithRegistry_CustomTypeAcceptedButDefaultStillReports(t *testing.T) {
	path := writeValidatePackFixture(t, customTypePack(customGuardrailType, "blocked"))

	t.Run("supplied registry knows the type", func(t *testing.T) {
		issues, err := sdk.ValidatePackWithRegistry(path, false, customEvalRegistry())
		require.NoError(t, err)
		assert.Empty(t, issues,
			"a validator and an eval naming a handler in the supplied registry must not be reported")
	})

	t.Run("default registry does not", func(t *testing.T) {
		issues, err := sdk.ValidatePack(path, false)
		require.NoError(t, err)
		require.Len(t, issues, 2,
			"the default registry does not know the custom type, so both the validator and the eval must be reported")
		assert.ElementsMatch(t, []string{"validator", "eval"}, unknownTypeIssueKinds(issues),
			"both preflight paths must be reporting — otherwise the pair above proves only one of them")
	})
}

// TestValidatePackWithRegistry_StillReportsTypeAbsentFromSuppliedRegistry pins
// that threading a registry narrows WHICH types are known — it does not switch
// the check off. A type absent from the supplied registry is still reported on
// both paths.
//
// Mutation caught: making ValidatePackWithRegistry skip validation whenever a
// registry is supplied, or evalRegistryOrDefault handing back something
// permissive. Either would pass the "custom type accepted" half above while
// blinding preflight to real typos.
func TestValidatePackWithRegistry_StillReportsTypeAbsentFromSuppliedRegistry(t *testing.T) {
	path := writeValidatePackFixture(t, customTypePack(absentEvalType, "blocked"))

	issues, err := sdk.ValidatePackWithRegistry(path, false, customEvalRegistry())
	require.NoError(t, err)
	require.Len(t, issues, 2,
		"a type in neither the default nor the supplied registry must still be reported")
	assert.ElementsMatch(t, []string{"validator", "eval"}, unknownTypeIssueKinds(issues))
}

// TestValidatePackWithRegistry_NilRegistryMeansDefault pins the nil contract
// established by #1724's CompileValidatorsWithRegistry / HookWithRegistry: nil
// is "no preference", i.e. the built-in default set — not "no registry, skip
// the checks".
//
// Mutations caught: a nil registry silently disabling validation (the custom
// pack would come back clean), and a missing nil guard (nil-deref panic inside
// registry.Get). The built-in half additionally pins that the default path
// still accepts a legitimate pack.
func TestValidatePackWithRegistry_NilRegistryMeansDefault(t *testing.T) {
	t.Run("custom type reported exactly as ValidatePack does", func(t *testing.T) {
		path := writeValidatePackFixture(t, customTypePack(customGuardrailType, "blocked"))

		withNil, err := sdk.ValidatePackWithRegistry(path, false, nil)
		require.NoError(t, err)
		viaValidatePack, err := sdk.ValidatePack(path, false)
		require.NoError(t, err)

		require.Len(t, withNil, 2, "nil must mean the default registry, not 'skip validation'")
		assert.ElementsMatch(t, viaValidatePack, withNil,
			"ValidatePackWithRegistry(path, skip, nil) must be equivalent to ValidatePack(path, skip)")
	})

	t.Run("built-in type still accepted", func(t *testing.T) {
		p := baseValidPack()
		prompts := p["prompts"].(map[string]any)
		def := prompts["default"].(map[string]any)
		def["validators"] = []any{
			map[string]any{
				"type":    "max_length",
				"enabled": true,
				"params":  map[string]any{"max_characters": 2000},
			},
		}
		path := writeValidatePackFixture(t, p)

		issues, err := sdk.ValidatePackWithRegistry(path, false, nil)
		require.NoError(t, err)
		assert.Empty(t, issues, "a built-in validator must remain valid under the nil-registry default")
	})
}

// TestValidatePackWithRegistry_AgreesWithOpen is the consistency assertion that
// gives the fix its point: what preflight accepts, the runtime must accept too.
// The identical pack bytes are fed to ValidatePackWithRegistry and to
// sdk.Open + Send with the SAME registry, and the guardrail is observed
// actually enforcing on the turn.
//
// Mutation caught: preflight and runtime drifting apart again — e.g. preflight
// resolving against the default registry (issues reported for a pack Open runs
// fine), or the guardrail being built but not enforcing (the raw response with
// the forbidden token comes back).
func TestValidatePackWithRegistry_AgreesWithOpen(t *testing.T) {
	const blocked = "blocked by the custom pack validator"
	const dirty = "Have a kumquat, on the house."

	packJSON, err := json.MarshalIndent(customTypePack(customGuardrailType, blocked), "", "  ")
	require.NoError(t, err)

	path := writeTestPack(t, packJSON)
	issues, err := sdk.ValidatePackWithRegistry(path, false, customEvalRegistry())
	require.NoError(t, err)
	require.Empty(t, issues, "preflight must accept the pack the runtime is about to enforce")

	got := sendOnce(t, packJSON, dirty, sdk.WithEvalRegistry(customEvalRegistry()))
	assert.Equal(t, blocked, got,
		"the validator preflight accepted must enforce at runtime under the same registry")
	assert.NotContains(t, got, forbiddenToken)
}
