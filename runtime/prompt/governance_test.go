package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
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
