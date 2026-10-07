package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

// govValidationPack builds a pack whose prompts declare validator ids
// ("no-pii" on support, "no-cards" on billing) and evals (prompt-level "tone",
// pack-level "safety"), so the governance under test has real things to
// reference. Decoded from JSON without schema validation, which is the path
// these rules must also cover.
func govValidationPack(t *testing.T, packGov, agentGov string) *prompt.Pack {
	t.Helper()

	metadata := `"metadata":{}`
	if packGov != "" {
		metadata = `"metadata":{"governance":` + packGov + `}`
	}
	agent := `{}`
	if agentGov != "" {
		agent = `{"governance":` + agentGov + `}`
	}

	src := `{"id":"p","name":"P","version":"1.0.0",
	  "template_engine":{"version":"v1","syntax":"{{variable}}"},
	  "evals":[{"id":"safety","type":"contains","trigger":"every_turn"}],
	  "prompts":{
	    "support":{"id":"support","name":"S","version":"1.0.0","system_template":"s",
	      "validators":[{"id":"no-pii","type":"banned_words"},{"type":"max_length"}]},
	    "billing":{"id":"billing","name":"B","version":"1.0.0","system_template":"b",
	      "validators":[{"id":"no-cards","type":"banned_words"}],
	      "evals":[{"id":"tone","type":"contains","trigger":"every_turn"}]}},
	  ` + metadata + `,
	  "agents":{"entry":"billing","members":{"billing":` + agent + `,"support":{}}}}`

	var p prompt.Pack
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	return &p
}

func obligation(id, controls string) string {
	return `{"id":"` + id + `","obligation":"eu-aiact:Article50","controls":[` + controls + `]}`
}

func review(id, extra string) string {
	r := `{"id":"` + id + `","type":"pp:BiasTesting","cadence":"P3M","owner":"ml-risk"`
	if extra != "" {
		r += "," + extra
	}
	return r + "}"
}

// TestGovernanceReferenceRules covers rules 2 and 4 to 8 with a passing and a
// failing case each. wantErrors is the exact number of errors, and wantText a
// substring every failing case must carry, so a case that fails for a
// different reason than intended is caught.
func TestGovernanceReferenceRules(t *testing.T) {
	cases := []struct {
		name       string
		packGov    string
		agentGov   string
		wantErrors int
		wantText   string
	}{
		{name: "no governance", wantErrors: 0},

		// Rule 2: ids unique within the declaring object.
		{
			name: "duplicate obligation id",
			packGov: `{"obligations":[` + obligation("a", `{"external":"x"}`) + `,` +
				obligation("a", `{"external":"y"}`) + `]}`,
			wantErrors: 1, wantText: `obligation id "a" is declared more than once`,
		},
		{
			name:       "duplicate review id",
			packGov:    `{"reviews":[` + review("r", "") + `,` + review("r", "") + `]}`,
			wantErrors: 1, wantText: `review id "r" is declared more than once`,
		},
		{
			name:       "same id in pack and agent is not a duplicate",
			packGov:    `{"obligations":[` + obligation("a", `{"external":"x"}`) + `]}`,
			agentGov:   `{"obligations":[` + obligation("a", `{"external":"y"}`) + `]}`,
			wantErrors: 0,
		},

		// Rule 4: eval references resolve to the pack's or a prompt's evals.
		{
			name:       "control eval resolves to a prompt-level eval",
			packGov:    `{"obligations":[` + obligation("a", `{"eval":"tone"}`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "control eval resolves to a pack-level eval",
			packGov:    `{"obligations":[` + obligation("a", `{"eval":"safety"}`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "control eval does not resolve",
			packGov:    `{"obligations":[` + obligation("a", `{"eval":"missing"}`) + `]}`,
			wantErrors: 1, wantText: `eval "missing" does not resolve`,
		},
		{
			name:       "review eval resolves to a prompt-level eval",
			packGov:    `{"reviews":[` + review("r", `"eval":"tone"`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "review eval does not resolve",
			packGov:    `{"reviews":[` + review("r", `"eval":"missing"`) + `]}`,
			wantErrors: 1, wantText: `eval "missing" does not resolve`,
		},

		// Rule 5: validator references resolve to a Validator.id on any prompt.
		{
			name:       "control validator resolves on another prompt",
			packGov:    `{"obligations":[` + obligation("a", `{"validator":"no-pii"}`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "control validator does not resolve",
			packGov:    `{"obligations":[` + obligation("a", `{"validator":"missing"}`) + `]}`,
			wantErrors: 1, wantText: `validator "missing" does not resolve`,
		},

		// Rule 6: a field control names a declared governance property.
		{
			name:       "field control names a declared property",
			packGov:    `{"accountable_owner":"risk","obligations":[` + obligation("a", `{"field":"accountable_owner"}`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "field control names an undeclared property",
			packGov:    `{"obligations":[` + obligation("a", `{"field":"accountable_owner"}`) + `]}`,
			wantErrors: 1, wantText: `field "accountable_owner" is not declared`,
		},
		{
			name:       "field control names something that is not a governance property",
			packGov:    `{"obligations":[` + obligation("a", `{"field":"colour"}`) + `]}`,
			wantErrors: 1, wantText: `field "colour" is not a governance property`,
		},
		{
			// An explicit false is a declaration, not silence.
			name: "explicit false counts as declared",
			packGov: `{"requires_ai_disclosure":false,"obligations":[` +
				obligation("a", `{"field":"requires_ai_disclosure"}`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "agent control relies on a field it inherits",
			packGov:    `{"accountable_owner":"risk"}`,
			agentGov:   `{"obligations":[` + obligation("a", `{"field":"accountable_owner"}`) + `]}`,
			wantErrors: 0,
		},

		// Rule 7: satisfies names an obligation in the effective object.
		{
			name: "satisfies resolves",
			packGov: `{"obligations":[` + obligation("a", `{"external":"x"}`) + `],` +
				`"reviews":[` + review("r", `"satisfies":["a"]`) + `]}`,
			wantErrors: 0,
		},
		{
			name:       "satisfies does not resolve",
			packGov:    `{"reviews":[` + review("r", `"satisfies":["nope"]`) + `]}`,
			wantErrors: 1, wantText: `satisfies "nope"`,
		},
		{
			// The agent replaces obligations whole, so the review it inherits
			// from the pack now names an obligation the agent no longer has.
			name: "agent obligations orphan an inherited review",
			packGov: `{"obligations":[` + obligation("a", `{"external":"x"}`) + `],` +
				`"reviews":[` + review("r", `"satisfies":["a"]`) + `]}`,
			agentGov:   `{"obligations":[` + obligation("b", `{"external":"y"}`) + `]}`,
			wantErrors: 1, wantText: `agents.members["billing"].governance (effective).reviews[0]: satisfies "a"`,
		},

		// Rule 8: cadence is a non-empty ISO 8601 duration.
		{name: "cadence P3M", packGov: `{"reviews":[` + review("r", "") + `]}`, wantErrors: 0},
		{
			name:       "cadence PT12H",
			packGov:    `{"reviews":[{"id":"r","type":"t","cadence":"PT12H","owner":"o"}]}`,
			wantErrors: 0,
		},
		{
			name:       "cadence P is empty",
			packGov:    `{"reviews":[{"id":"r","type":"t","cadence":"P","owner":"o"}]}`,
			wantErrors: 1, wantText: `cadence "P"`,
		},
		{
			name:       "cadence without P",
			packGov:    `{"reviews":[{"id":"r","type":"t","cadence":"3M","owner":"o"}]}`,
			wantErrors: 1, wantText: `cadence "3M"`,
		},
		{
			name:       "cadence with an unknown unit",
			packGov:    `{"reviews":[{"id":"r","type":"t","cadence":"P3Q","owner":"o"}]}`,
			wantErrors: 1, wantText: `cadence "P3Q"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := govValidationPack(t, tc.packGov, tc.agentGov).ValidateGovernance()

			require.Len(t, res.Errors, tc.wantErrors, "errors: %v", res.Errors)
			for _, e := range res.Errors {
				require.Contains(t, e, tc.wantText)
			}
		})
	}
}

// TestValidatorIDsAreUniqueAcrossThePack — rule 5's uniqueness spans prompts,
// so the same id on two different prompts is an error even though each prompt
// on its own is fine.
func TestValidatorIDsAreUniqueAcrossThePack(t *testing.T) {
	p := govValidationPack(t, "", "")
	p.Prompts["billing"].Validators[0].ID = "no-pii"

	res := p.ValidateGovernance()

	require.Equal(t, []string{
		`prompts["support"].validators[0]: validator id "no-pii" is already declared at ` +
			`prompts["billing"].validators[0]; validator ids must be unique across the pack`,
	}, res.Errors)
}

// TestUndeclaredPrefixWarnsAndNeverErrors is rule 10. Every case produces no
// error; only an undeclared prefix produces a warning.
func TestUndeclaredPrefixWarnsAndNeverErrors(t *testing.T) {
	cases := []struct {
		name      string
		packGov   string
		agentGov  string
		wantWarns []string
	}{
		{
			name:      "undeclared prefix in obligation",
			packGov:   `{"obligations":[{"id":"a","obligation":"acme:Policy-4","controls":[{"external":"x"}]}]}`,
			wantWarns: []string{`metadata.governance.obligations[0].obligation: term "acme:Policy-4" uses prefix "acme"`},
		},
		{
			name: "undeclared prefix in applies_to",
			packGov: `{"obligations":[{"id":"a","obligation":"x","controls":[{"external":"x"}],` +
				`"applies_to":{"capability":"acme:Cap","data_class":"pd:Health","risk_classification":"zz:High"}}]}`,
			wantWarns: []string{
				`metadata.governance.obligations[0].applies_to.capability: term "acme:Cap" uses prefix "acme"`,
				`metadata.governance.obligations[0].applies_to.risk_classification: term "zz:High" uses prefix "zz"`,
			},
		},
		{
			name:      "undeclared prefix in review type",
			packGov:   `{"reviews":[{"id":"r","type":"nist:Measure","cadence":"P1Y","owner":"o"}]}`,
			wantWarns: []string{`metadata.governance.reviews[0].type: term "nist:Measure" uses prefix "nist"`},
		},
		{
			name: "prefix declared in vocabularies",
			packGov: `{"vocabularies":{"acme":"https://acme.example/v#"},` +
				`"obligations":[{"id":"a","obligation":"acme:Policy","controls":[{"external":"x"}]}]}`,
		},
		{
			name: "well-known prefixes and free strings",
			packGov: `{"reviews":[{"id":"r","type":"pp:BiasTesting","cadence":"P1Y","owner":"o"},` +
				`{"id":"s","type":"Annual bias review","cadence":"P1Y","owner":"o"}]}`,
		},
		{
			name: "absolute IRIs are not CURIEs",
			packGov: `{"obligations":[{"id":"a","obligation":"https://acme.example/policy#4","controls":[{"external":"x"}]},` +
				`{"id":"b","obligation":"urn:acme:policy:4","controls":[{"external":"x"}]}]}`,
		},
		{
			// vocabularies merges, so an agent can use a prefix the pack declares.
			name:     "agent term uses a prefix the pack declares",
			packGov:  `{"vocabularies":{"acme":"https://acme.example/v#"}}`,
			agentGov: `{"obligations":[{"id":"a","obligation":"acme:Policy","controls":[{"external":"x"}]}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := govValidationPack(t, tc.packGov, tc.agentGov).ValidateGovernance()

			require.Empty(t, res.Errors, "an undeclared prefix must never be an error")
			require.Len(t, res.Warnings, len(tc.wantWarns), "warnings: %v", res.Warnings)
			for i, want := range tc.wantWarns {
				require.Contains(t, res.Warnings[i], want)
			}
		})
	}
}

// TestPackValidateIncludesGovernance — Validate is what promptarena's packc
// calls, so the governance checks must come through it.
func TestPackValidateIncludesGovernance(t *testing.T) {
	p := govValidationPack(t,
		`{"obligations":[{"id":"a","obligation":"acme:X","controls":[{"eval":"missing"}]}]}`, "")

	got := p.Validate()

	require.Contains(t, got,
		`metadata.governance.obligations[0].controls[0]: eval "missing" does not resolve to an eval `+
			`in the pack's or a prompt's evals`)
	require.Contains(t, got,
		`metadata.governance.obligations[0].obligation: term "acme:X" uses prefix "acme", `+
			`which is neither well-known nor declared in vocabularies`)
}

// TestCadencePatternMatchesTheEmbeddedSchema keeps the Go copy of the cadence
// pattern (used when schema validation is skipped) equal to the schema's.
func TestCadencePatternMatchesTheEmbeddedSchema(t *testing.T) {
	var doc struct {
		Defs struct {
			Review struct {
				Properties struct {
					Cadence struct {
						Pattern string `json:"pattern"`
					} `json:"cadence"`
				} `json:"properties"`
			} `json:"Review"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal([]byte(schema.GetEmbeddedSchema()), &doc))

	require.Equal(t, doc.Defs.Review.Properties.Cadence.Pattern, prompt.CadencePattern)
}
