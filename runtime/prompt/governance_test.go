package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// packWithGovernance builds a pack from JSON so the tests exercise the same
// decoding path a real pack takes, rather than hand-building structs that could
// hold a shape the loader never produces.
func packWithGovernance(t *testing.T, packGov, agentGov string) *prompt.Pack {
	t.Helper()

	metadata := `"metadata":{}`
	if packGov != "" {
		metadata = `"metadata":{"governance":` + packGov + `}`
	}
	agent := `{"description":"the billing agent"}`
	if agentGov != "" {
		agent = `{"description":"the billing agent","governance":` + agentGov + `}`
	}

	src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
	  "template_engine":{"version":"v1","syntax":"{{variable}}"},
	  "prompts":{},` + metadata + `,
	  "agents":{"entry":"billing","members":{"billing":` + agent + `}}}`

	var p prompt.Pack
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	return &p
}

// TestAgentFieldReplacesPackField — the base case of the override rule.
func TestAgentFieldReplacesPackField(t *testing.T) {
	p := packWithGovernance(t,
		`{"autonomy_level":"suggests","accountable_owner":"platform@example.com"}`,
		`{"autonomy_level":"acts_autonomously"}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	require.Equal(t, prompt.AutonomyLevelActsAutonomously, got.AutonomyLevel,
		"a field present on the agent must replace the pack value")
	require.Equal(t, "platform@example.com", got.AccountableOwner,
		"a field absent on the agent must inherit the pack value")
}

// TestArraysReplaceWholeAndAreNotAppended is the rule a consumer implementing
// this by hand is most likely to get wrong, because appending is the intuitive
// guess. An agent narrowing the pack's approved environments to staging must end
// up cleared for staging ONLY — appending would leave it cleared for production
// as well, which is the opposite of what the author wrote.
func TestArraysReplaceWholeAndAreNotAppended(t *testing.T) {
	p := packWithGovernance(t,
		`{"approved_environments":["production","staging"],
		  "capabilities":["ai:decision-making","ai:profiling"]}`,
		`{"approved_environments":["staging"]}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	require.Equal(t, []string{"staging"}, got.ApprovedEnvironments,
		"arrays replace whole; appending would re-approve production")
	require.Equal(t, []string{"ai:decision-making", "ai:profiling"}, got.Capabilities,
		"an array the agent does not declare still inherits")
}

// TestExplicitFalseOverridesTrueDisclosure — requires_ai_disclosure is the one
// *bool in the block, and this is why. An agent that must NOT disclose, under a
// pack that says it must, has to be able to say so; a plain bool would make that
// indistinguishable from silence and the pack's true would win.
func TestExplicitFalseOverridesTrueDisclosure(t *testing.T) {
	p := packWithGovernance(t,
		`{"requires_ai_disclosure":true}`,
		`{"requires_ai_disclosure":false}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	require.NotNil(t, got.RequiresAIDisclosure)
	require.False(t, *got.RequiresAIDisclosure,
		"an explicit false must override the pack's true, not read as unset")
}

// TestSilenceOnDisclosureInherits is the other half: saying nothing must NOT be
// read as false.
func TestSilenceOnDisclosureInherits(t *testing.T) {
	p := packWithGovernance(t, `{"requires_ai_disclosure":true}`, `{"autonomy_level":"suggests"}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	require.NotNil(t, got.RequiresAIDisclosure,
		"an agent that says nothing about disclosure must inherit, not reset to unset")
	require.True(t, *got.RequiresAIDisclosure)
}

// TestUnknownAgentIsAnErrorNotAFallback — for most lookups a quiet fallback is a
// convenience. For governance it is a lie: a caller that typo'd the agent name
// would be handed the pack's autonomy level and told the agent needs no
// approval, when nothing about that agent had been checked at all.
func TestUnknownAgentIsAnErrorNotAFallback(t *testing.T) {
	p := packWithGovernance(t, `{"autonomy_level":"suggests"}`, "")

	got, err := prompt.ResolveGovernance(p, "billling")
	require.Error(t, err, "an unknown agent must not silently resolve to the pack values")
	require.Nil(t, got)
	require.Contains(t, err.Error(), "billling", "the error must name the agent asked for")
	require.Contains(t, err.Error(), "billing", "and list the agents that do exist")
}

func TestEmptyAgentNameGivesPackGovernance(t *testing.T) {
	p := packWithGovernance(t, `{"autonomy_level":"suggests"}`, `{"autonomy_level":"acts_autonomously"}`)

	got, err := prompt.ResolveGovernance(p, "")
	require.NoError(t, err)
	require.Equal(t, prompt.AutonomyLevelSuggests, got.AutonomyLevel)
}

func TestAgentWithoutGovernanceInheritsWholesale(t *testing.T) {
	p := packWithGovernance(t, `{"autonomy_level":"suggests","intended_purpose":"triage"}`, "")

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)
	require.Equal(t, prompt.AutonomyLevelSuggests, got.AutonomyLevel)
	require.Equal(t, "triage", got.IntendedPurpose)
}

// TestAgentGovernanceStandsWithoutAPackDeclaration — an undeclared pack does not
// erase what an agent declares about itself.
func TestAgentGovernanceStandsWithoutAPackDeclaration(t *testing.T) {
	p := packWithGovernance(t, "", `{"autonomy_level":"acts_with_oversight"}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, prompt.AutonomyLevelActsWithOversight, got.AutonomyLevel)
}

// TestNoGovernanceAnywhereResolvesToNothing — absence must resolve to nil, not
// to a zero-valued declaration. A &Governance{} reads as "declared, with
// everything empty" to anything rendering it, which is a different and worse
// claim than "not declared".
//
// The declared case is asserted alongside it deliberately: on its own, the
// absence half would pass an implementation that always returned nil, and a
// resolver that never resolves anything is the more likely bug.
func TestNoGovernanceAnywhereResolvesToNothing(t *testing.T) {
	absent := packWithGovernance(t, "", "")
	got, err := prompt.ResolveGovernance(absent, "billing")
	require.NoError(t, err)
	require.Nil(t, got, "absence must resolve to nil, not to a zero-valued declaration")

	declared := packWithGovernance(t, `{"autonomy_level":"suggests"}`, "")
	got, err = prompt.ResolveGovernance(declared, "billing")
	require.NoError(t, err)
	require.Equal(t, prompt.AutonomyLevelSuggests, got.AutonomyLevel,
		"the same call must return a declaration when there is one to return")

	require.Nil(t, prompt.PackGovernance(nil))
	require.Nil(t, prompt.PackGovernance(&prompt.Pack{}))
}

// TestResolvedGovernanceIsACopy — governance is read by anything that wants to
// know what a pack claims. A caller adjusting the result in place must not
// rewrite the loaded pack for every other caller.
func TestResolvedGovernanceIsACopy(t *testing.T) {
	p := packWithGovernance(t,
		`{"autonomy_level":"suggests","approved_environments":["production"]}`, "")

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	got.AutonomyLevel = "acts_autonomously"
	got.ApprovedEnvironments[0] = "anywhere"

	require.Equal(t, prompt.AutonomyLevelSuggests, p.Metadata.Governance.AutonomyLevel,
		"mutating the resolved copy must not reach the pack")
	require.Equal(t, []string{"production"}, p.Metadata.Governance.ApprovedEnvironments,
		"the slice must be copied, not shared")
}

// TestExtensionsReplaceWholeButVocabulariesMerge — the two containers behave
// differently, which is the whole reason this test names both.
//
// extensions is in the spec's "arrays and extensions replace whole" list.
// vocabularies is not, and it is a prefix map that makes CURIEs resolvable:
// replacing it would put an agent's INHERITED values out of scope, because the
// pack's risk_classification below is written as a CURIE against the pack's own
// prefix. Prefix maps accumulate in every other CURIE system for the reason
// this test demonstrates.
func TestExtensionsReplaceWholeButVocabulariesMerge(t *testing.T) {
	p := packWithGovernance(t,
		`{"extensions":{"acme:tier":"gold","acme:region":"eu"},
		  "vocabularies":{"acme":"https://acme.example/ns#"},
		  "risk_classification":"acme:tier-3"}`,
		`{"extensions":{"acme:tier":"silver"},
		  "vocabularies":{"other":"https://other.example/ns#"}}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)

	require.Equal(t, map[string]any{"acme:tier": "silver"}, got.Extensions,
		"extensions replace whole; merging would leave acme:region behind")

	require.Equal(t, map[string]string{
		"acme":  "https://acme.example/ns#",
		"other": "https://other.example/ns#",
	}, got.Vocabularies, "prefixes accumulate")

	require.Equal(t, "acme:tier-3", got.RiskClassification)
	require.Contains(t, got.Vocabularies, "acme",
		"the inherited risk_classification is a CURIE against the pack's prefix, "+
			"so dropping that prefix would leave an inherited value unresolvable")
}

// TestAnAgentCanRebindAPrefix — merging must still let an agent point an
// existing prefix at a different IRI, or it could never override a vocabulary.
func TestAnAgentCanRebindAPrefix(t *testing.T) {
	p := packWithGovernance(t,
		`{"vocabularies":{"acme":"https://acme.example/v1#","shared":"https://s.example#"}}`,
		`{"vocabularies":{"acme":"https://acme.example/v2#"}}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)
	require.Equal(t, "https://acme.example/v2#", got.Vocabularies["acme"],
		"a prefix the agent redeclares takes the agent's IRI")
	require.Equal(t, "https://s.example#", got.Vocabularies["shared"],
		"one it does not mention is inherited")
}

// TestAgentGovernanceReportsWhatIsWritten — AgentGovernance is for showing what
// the pack says, so it must NOT inherit.
func TestAgentGovernanceReportsWhatIsWritten(t *testing.T) {
	p := packWithGovernance(t,
		`{"autonomy_level":"suggests","accountable_owner":"platform@example.com"}`,
		`{"autonomy_level":"acts_autonomously"}`)

	got, err := prompt.AgentGovernance(p, "billing")
	require.NoError(t, err)
	require.Equal(t, prompt.AutonomyLevelActsAutonomously, got.AutonomyLevel)
	require.Empty(t, got.AccountableOwner,
		"AgentGovernance reports what is written; inheriting is ResolveGovernance's job")
}

func TestDescribeGovernanceOmitsUndeclaredFields(t *testing.T) {
	got := prompt.DescribeGovernance(&packspec.Governance{
		AutonomyLevel:        prompt.AutonomyLevelActsWithApproval,
		AccountableOwner:     "risk@example.com",
		RequiresAIDisclosure: packspec.Ptr(true),
	})

	for _, want := range []string{"acts_with_approval", "risk@example.com", "must disclose as AI"} {
		require.Contains(t, got, want)
	}
	// An undeclared field must not be printed as a default: absence is not a
	// value, and "risk: unknown" invites reading it as one.
	require.NotContains(t, got, "risk:")
	require.NotContains(t, got, "purpose:")

	require.Empty(t, prompt.DescribeGovernance(nil))
}

// TestDescribeNamesAnExplicitNoDisclosure — "not required" and "not stated" are
// different claims, and only one of them is a decision someone made.
func TestDescribeNamesAnExplicitNoDisclosure(t *testing.T) {
	stated := prompt.DescribeGovernance(&packspec.Governance{
		RequiresAIDisclosure: packspec.Ptr(false),
	})
	require.Contains(t, stated, "not required")

	silent := prompt.DescribeGovernance(&packspec.Governance{AutonomyLevel: "suggests"})
	require.NotContains(t, strings.ToLower(silent), "disclos",
		"saying nothing about disclosure must not be rendered as a decision")
}

// TestResolveAgainstAPackWithNoAgents — the error has to be usable, not a nil
// dereference or a bare "not found".
func TestResolveAgainstAPackWithNoAgents(t *testing.T) {
	var p prompt.Pack
	_, err := prompt.ResolveGovernance(&p, "billing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no agents")

	_, err = prompt.ResolveGovernance(nil, "billing")
	require.Error(t, err)
}

// RFC 0016 adds three governance fields, and they follow RFC 0013's
// per-field replacement like everything else in the block. Each case here
// covers one field on its own: the agent's value replaces the pack's whole, an
// absent one inherits, and neither is merged with the other.
func TestRFC0016FieldsFollowPerFieldReplacement(t *testing.T) {
	const packGov = `{
	  "independent_of":{"axes":["model","provider"],"enforcement":"strict"},
	  "obligations":[
	    {"id":"art50","obligation":"eu-aiact:Article50","controls":[{"field":"requires_ai_disclosure"}]},
	    {"id":"gdpr22","obligation":"legal-eu-gdpr:Article22","controls":[{"external":"DPO sign-off"}]}],
	  "reviews":[{"id":"bias","type":"pp:BiasTesting","cadence":"P3M","owner":"ml-risk"}]}`

	strict := packspec.Ptr("strict")
	cases := []struct {
		name            string
		agentGov        string
		wantAxes        []any
		wantEnforcement *string
		wantObligations []string
		wantReviews     []string
	}{
		{
			// The pack's strict enforcement must not leak into the agent's
			// object: a half-inherited requirement is one nobody wrote.
			name:            "independent_of replaces whole",
			agentGov:        `{"independent_of":{"axes":["accountable_owner"]}}`,
			wantAxes:        []any{"accountable_owner"},
			wantEnforcement: nil,
			wantObligations: []string{"art50", "gdpr22"},
			wantReviews:     []string{"bias"},
		},
		{
			name: "obligations replace whole",
			agentGov: `{"obligations":[
			  {"id":"own","obligation":"acme:Own","controls":[{"external":"agent-only"}]}]}`,
			wantAxes:        []any{"model", "provider"},
			wantEnforcement: strict,
			wantObligations: []string{"own"},
			wantReviews:     []string{"bias"},
		},
		{
			name:            "reviews replace whole",
			agentGov:        `{"reviews":[{"id":"acc","type":"pp:AccuracyReview","cadence":"P1Y","owner":"qa"}]}`,
			wantAxes:        []any{"model", "provider"},
			wantEnforcement: strict,
			wantObligations: []string{"art50", "gdpr22"},
			wantReviews:     []string{"acc"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := packWithGovernance(t, packGov, tc.agentGov)
			got, err := prompt.ResolveGovernance(p, "billing")
			require.NoError(t, err)

			require.Equal(t, tc.wantAxes, got.IndependentOf.Axes)
			require.Equal(t, tc.wantEnforcement, got.IndependentOf.Enforcement)
			require.Equal(t, tc.wantObligations, obligationIDs(got.Obligations))
			require.Equal(t, tc.wantReviews, reviewIDs(got.Reviews))
		})
	}
}

func obligationIDs(in []*packspec.Obligation) []string {
	ids := make([]string, 0, len(in))
	for _, o := range in {
		ids = append(ids, o.ID)
	}
	return ids
}

func reviewIDs(in []*packspec.Review) []string {
	ids := make([]string, 0, len(in))
	for _, r := range in {
		ids = append(ids, r.ID)
	}
	return ids
}

// TestRFC0016FieldsAreDeepCopies — the new fields are the first in the block
// with nested structs and nested extensions, so a top-level copy would still
// hand a caller pointers into the loaded pack. Every level is mutated here.
func TestRFC0016FieldsAreDeepCopies(t *testing.T) {
	p := packWithGovernance(t, `{
	  "independent_of":{"axes":["model"],"enforcement":"strict"},
	  "obligations":[{"id":"art50","obligation":"eu-aiact:Article50",
	    "applies_to":{"capability":"eu-aiact:DeepFake"},
	    "controls":[{"validator":"no-deepfake"}],
	    "extensions":{"acme:controls":{"set":["CC7"]}}}],
	  "reviews":[{"id":"bias","type":"pp:BiasTesting","cadence":"P3M","owner":"ml-risk",
	    "satisfies":["art50"],"extensions":{"acme:evidence":{"path":"s3://x"}}}],
	  "extensions":{"acme:nested":{"k":"v"}}}`, "")

	for _, name := range []string{"", "billing"} {
		got, err := prompt.ResolveGovernance(p, name)
		require.NoError(t, err)

		got.IndependentOf.Axes[0] = "tools"
		*got.IndependentOf.Enforcement = "advisory"
		got.Obligations[0].ID = "changed"
		got.Obligations[0].AppliesTo.Capability = "changed"
		got.Obligations[0].Controls[0].Validator = "changed"
		got.Obligations[0].Extensions["acme:controls"].(map[string]any)["set"].([]any)[0] = "changed"
		got.Reviews[0].Satisfies[0] = "changed"
		got.Reviews[0].Extensions["acme:evidence"].(map[string]any)["path"] = "changed"
		got.Extensions["acme:nested"].(map[string]any)["k"] = "changed"
	}

	g := p.Metadata.Governance
	require.Equal(t, []any{"model"}, g.IndependentOf.Axes)
	require.Equal(t, "strict", *g.IndependentOf.Enforcement)
	require.Equal(t, "art50", g.Obligations[0].ID)
	require.Equal(t, "eu-aiact:DeepFake", g.Obligations[0].AppliesTo.Capability)
	require.Equal(t, "no-deepfake", g.Obligations[0].Controls[0].Validator)
	require.Equal(t, "CC7", g.Obligations[0].Extensions["acme:controls"].(map[string]any)["set"].([]any)[0])
	require.Equal(t, []string{"art50"}, g.Reviews[0].Satisfies)
	require.Equal(t, "s3://x", g.Reviews[0].Extensions["acme:evidence"].(map[string]any)["path"])
	require.Equal(t, "v", g.Extensions["acme:nested"].(map[string]any)["k"],
		"nested extensions values must be copied too, not shared")
}

// TestRFC0016AgentOverlayIsADeepCopy — the overlay path copies from the
// agent's declaration rather than the pack's, so it needs its own check.
func TestRFC0016AgentOverlayIsADeepCopy(t *testing.T) {
	p := packWithGovernance(t, `{"autonomy_level":"suggests"}`, `{
	  "independent_of":{"axes":["prompts"]},
	  "obligations":[{"id":"o","obligation":"x","controls":[{"eval":"e"}]}],
	  "reviews":[{"id":"r","type":"t","cadence":"P1Y","owner":"o","satisfies":["o"]}]}`)

	got, err := prompt.ResolveGovernance(p, "billing")
	require.NoError(t, err)
	got.IndependentOf.Axes[0] = "tools"
	got.Obligations[0].Controls[0].Eval = "changed"
	got.Reviews[0].Satisfies[0] = "changed"

	agent := p.Agents.Members["billing"].Governance
	require.Equal(t, []any{"prompts"}, agent.IndependentOf.Axes)
	require.Equal(t, "e", agent.Obligations[0].Controls[0].Eval)
	require.Equal(t, []string{"o"}, agent.Reviews[0].Satisfies)
}

func TestDescribeGovernanceListsRFC0016Fields(t *testing.T) {
	got := prompt.DescribeGovernance(&packspec.Governance{
		IndependentOf: &packspec.GovernanceIndependentOf{
			Axes: []any{"accountable_owner", "model"}, Enforcement: packspec.Ptr("strict"),
		},
		Obligations: []*packspec.Obligation{{ID: "art50"}, {ID: "gdpr22"}},
		Reviews:     []*packspec.Review{{ID: "bias", Cadence: "P3M"}},
	})

	require.Contains(t, got, "independent of: accountable_owner/model (strict)")
	require.Contains(t, got, "obligations: art50/gdpr22")
	require.Contains(t, got, "reviews: bias every P3M")

	// Without a declared enforcement, the schema's "advisory" default is not
	// printed: it was not declared.
	got = prompt.DescribeGovernance(&packspec.Governance{
		IndependentOf: &packspec.GovernanceIndependentOf{Axes: []any{"tools"}},
	})
	require.Equal(t, "independent of: tools", got)
}
