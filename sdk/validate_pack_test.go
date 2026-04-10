package sdk_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// writeValidatePackFixture writes a promptpack JSON to a temp file and
// returns its path. Reused by the ValidatePack tests.
func writeValidatePackFixture(t *testing.T, pack map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.json")
	data, err := json.MarshalIndent(pack, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// baseValidPack returns a minimal promptpack with one prompt and no
// validators or evals. The shape matches the promptpack spec's
// required top-level fields so the fixture passes strict schema
// validation (ValidatePack's default). Callers can mutate the returned
// map before passing to writeValidatePackFixture.
func baseValidPack() map[string]any {
	return map[string]any{
		"id":          "validate-pack-test",
		"name":        "Validate Pack Test",
		"version":     "1.0.0",
		"description": "ValidatePack test fixture",
		"template_engine": map[string]any{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]any{
			"default": map[string]any{
				"id":              "default",
				"name":            "Default",
				"description":     "test prompt",
				"version":         "1.0.0",
				"system_template": "You are helpful.",
			},
		},
	}
}

func TestValidatePack_ValidPackReturnsNoIssues(t *testing.T) {
	pack := baseValidPack()
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	assert.Empty(t, issues, "a valid pack must produce no issues")
}

func TestValidatePack_ValidPackWithGoodValidator(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "max_length",
			"enabled": true,
			"params":  map[string]any{"max_characters": 2000},
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestValidatePack_ReportsUnknownValidatorType(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "nonexistent_validator",
			"enabled": true,
			"params":  map[string]any{},
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "validator", issues[0].Kind)
	assert.Equal(t, "default", issues[0].PromptID)
	assert.Equal(t, "nonexistent_validator", issues[0].Type)
	assert.Contains(t, issues[0].Reason, "unknown")
}

func TestValidatePack_ReportsMissingValidatorParams(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "max_length",
			"enabled": true,
			"params":  map[string]any{}, // missing required max_characters
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "validator", issues[0].Kind)
	assert.Equal(t, "max_length", issues[0].Type)
	assert.Contains(t, issues[0].Reason, "max")
}

func TestValidatePack_SkipsDisabledValidator(t *testing.T) {
	// Disabled validators are not checked — consistent with
	// convertPackValidatorsToHooks behaviour at Open() time.
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "max_length",
			"enabled": false,
			"params":  map[string]any{}, // would be invalid if enabled
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	assert.Empty(t, issues, "disabled validators must not produce issues")
}

func TestValidatePack_ReportsUnknownEvalType(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["evals"] = []any{
		map[string]any{
			"id":      "bad-eval",
			"type":    "nonexistent_eval",
			"trigger": "every_turn",
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "eval", issues[0].Kind)
	assert.Equal(t, "bad-eval", issues[0].ID)
	assert.Contains(t, issues[0].Reason, "unknown")
}

func TestValidatePack_ReportsMissingEvalParams(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["evals"] = []any{
		map[string]any{
			"id":      "len-eval",
			"type":    "max_length",
			"trigger": "every_turn",
			"params":  map[string]any{}, // missing required max_characters
		},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "eval", issues[0].Kind)
	assert.Equal(t, "len-eval", issues[0].ID)
	assert.Contains(t, issues[0].Reason, "max")
}

func TestValidatePack_MultipleIssues(t *testing.T) {
	pack := baseValidPack()
	prompts := pack["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{"type": "max_length", "enabled": true, "params": map[string]any{}},
		map[string]any{"type": "unknown_type", "enabled": true, "params": map[string]any{}},
	}
	def["evals"] = []any{
		map[string]any{"id": "bad1", "type": "nonexistent", "trigger": "every_turn"},
	}
	path := writeValidatePackFixture(t, pack)

	issues, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	assert.Len(t, issues, 3, "must report all three issues")
}

func TestValidatePack_FileNotFound(t *testing.T) {
	issues, err := sdk.ValidatePack("/nonexistent/path/pack.json", false)
	require.Error(t, err)
	assert.Nil(t, issues)
}

func TestPackIssue_String(t *testing.T) {
	// Validator issue with a prompt — uses the "prompts.<id>.<kind>s[i]"
	// location format and the Type tag.
	vIssue := sdk.PackIssue{
		Severity: "error",
		Kind:     "validator",
		PromptID: "default",
		Index:    2,
		Type:     "max_length",
		Reason:   "max_length requires positive integer",
	}
	got := vIssue.String()
	assert.Contains(t, got, "error")
	assert.Contains(t, got, "prompts.default.validators[2]")
	assert.Contains(t, got, "max_length")
	assert.Contains(t, got, "requires positive integer")

	// Eval issue with an ID — tag should show both type and id.
	eIssue := sdk.PackIssue{
		Severity: "error",
		Kind:     "eval",
		PromptID: "default",
		Index:    0,
		Type:     "contains",
		ID:       "tone-check",
		Reason:   "missing 'needle'",
	}
	got = eIssue.String()
	assert.Contains(t, got, "prompts.default.evals[0]")
	assert.Contains(t, got, "id=tone-check")

	// Pack-level issue (no PromptID) — location collapses to bare Kind.
	packLevel := sdk.PackIssue{
		Severity: "error",
		Kind:     "eval",
		ID:       "pack-wide",
		Type:     "unknown",
		Reason:   "unknown type",
	}
	got = packLevel.String()
	assert.Contains(t, got, "error eval")
	assert.NotContains(t, got, "prompts.")
}

func TestValidatePack_SchemaValidationError(t *testing.T) {
	// Malformed pack — missing required top-level fields
	// ("name", "version", "template_engine", "prompts"). With strict
	// schema validation on by default, this fails at the schema layer;
	// ValidatePack surfaces the failure as an error, not a PackIssue
	// (consistent with the contract: structural/schema failures are
	// errors, semantic problems are issues).
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"id": "bad"}`), 0o600))

	issues, err := sdk.ValidatePack(path, false)
	require.Error(t, err, "structural failure must be returned as error, not as issues")
	assert.Nil(t, issues)
}

// TestValidatePack_StrictSchemaRejectsNonSpecFields pins the strict
// default: a validator declaring a field the promptpack spec forbids
// (additionalProperties:false) is returned as a load-time error, not as
// a PackIssue. This is the schema-level guarantee — the same one that
// would catch the issue #933 class of struct drift if someone added
// "monitor" back to pack.Validator.
func TestValidatePack_StrictSchemaRejectsNonSpecFields(t *testing.T) {
	p := baseValidPack()
	prompts := p["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "max_length",
			"enabled": true,
			"params":  map[string]any{"max_characters": 2000},
			"monitor": true, // forbidden by additionalProperties:false
		},
	}
	path := writeValidatePackFixture(t, p)

	issues, err := sdk.ValidatePack(path, false)
	require.Error(t, err, "strict schema validation must reject non-spec validator fields")
	assert.Nil(t, issues, "schema failures must be returned as error, not issues")
}

// TestValidatePack_SkipSchemaAllowsNonSpecFields confirms the opt-out.
// With skipSchemaValidation=true, strict schema is bypassed; handler-level
// validation still runs on the declared validators. The forbidden
// "monitor" field is ignored at load time (it's not in the Go struct),
// and the validator itself is semantically valid, so no issues are
// reported.
func TestValidatePack_SkipSchemaAllowsNonSpecFields(t *testing.T) {
	p := baseValidPack()
	prompts := p["prompts"].(map[string]any)
	def := prompts["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{
			"type":    "max_length",
			"enabled": true,
			"params":  map[string]any{"max_characters": 2000},
			"monitor": true, // forbidden by schema, ignored at unmarshal
		},
	}
	path := writeValidatePackFixture(t, p)

	issues, err := sdk.ValidatePack(path, true)
	require.NoError(t, err, "schema check must be bypassed with skipSchemaValidation=true")
	assert.Empty(t, issues, "semantically valid validator produces no issues")
}
