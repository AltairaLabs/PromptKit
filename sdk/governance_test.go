package sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// govConversation builds a Conversation around a pack, scoped to promptName.
//
// Constructed directly rather than through Open() because Open needs a provider
// and a real pack on disk, and neither is involved in reading a declaration off
// the loaded pack. What matters is the field Governance() reads — promptName,
// which openAgent sets to the agent's member key.
func govConversation(t *testing.T, promptName, packGov, agentGov string) *Conversation {
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

	var p pack.Pack
	require.NoError(t, json.Unmarshal([]byte(src), &p))

	return &Conversation{pack: &p, promptName: promptName}
}

// TestGovernanceResolvesForAnAgentScopedConversation — openAgent opens a member
// as Open(packPath, memberKey), so promptName is the agent name and the agent's
// own declaration has to win over the pack's.
func TestGovernanceResolvesForAnAgentScopedConversation(t *testing.T) {
	c := govConversation(t, "billing",
		`{"autonomy_level":"suggests","accountable_owner":"platform@example.com"}`,
		`{"autonomy_level":"acts_autonomously"}`)

	got := c.Governance()
	require.NotNil(t, got)
	require.Equal(t, "acts_autonomously", got.AutonomyLevel,
		"an agent-scoped conversation must get the agent's resolved governance")
	require.Equal(t, "platform@example.com", got.AccountableOwner,
		"and still inherit what the agent does not declare")
}

// TestGovernanceFallsBackForAPlainPromptConversation — a single-prompt
// conversation is not an agent. ResolveGovernance rejects an unknown agent by
// design, which is right for a caller naming one and wrong here, so Governance
// checks membership first and returns the pack declaration.
func TestGovernanceFallsBackForAPlainPromptConversation(t *testing.T) {
	c := govConversation(t, "chat",
		`{"autonomy_level":"suggests"}`,
		`{"autonomy_level":"acts_autonomously"}`)

	got := c.Governance()
	require.NotNil(t, got, "a non-agent conversation must still see the pack declaration")
	require.Equal(t, "suggests", got.AutonomyLevel,
		"it must NOT pick up the billing agent's override")
}

// TestPackGovernanceIgnoresAgentScope — the two methods have to differ, or one
// of them is pointless.
func TestPackGovernanceIgnoresAgentScope(t *testing.T) {
	c := govConversation(t, "billing",
		`{"autonomy_level":"suggests"}`,
		`{"autonomy_level":"acts_autonomously"}`)

	require.Equal(t, "acts_autonomously", c.Governance().AutonomyLevel)
	require.Equal(t, "suggests", c.PackGovernance().AutonomyLevel,
		"PackGovernance must ignore the agent override")
}

// TestGovernanceIsNilWhenUndeclared — nil, not a zero-valued declaration: a
// &Governance{} reads as "declared everything as empty" to a host rendering it.
// The declared case is asserted alongside so an always-nil implementation fails.
func TestGovernanceIsNilWhenUndeclared(t *testing.T) {
	require.Nil(t, govConversation(t, "billing", "", "").Governance())

	declared := govConversation(t, "billing", `{"autonomy_level":"suggests"}`, "")
	require.Equal(t, "suggests", declared.Governance().AutonomyLevel,
		"the same call must return a declaration when there is one")
}

// TestGovernanceOnAnEmptyConversationDoesNotPanic — a host may read this before
// or after anything else, including on a zero value or a nil receiver.
//
// The populated case is asserted last so the test distinguishes "returns nil
// when there is nothing" from "returns nil always", which is what a missing
// pack lookup would look like and what the nil assertions alone cannot tell
// apart.
func TestGovernanceOnAnEmptyConversationDoesNotPanic(t *testing.T) {
	var nilConv *Conversation
	require.Nil(t, nilConv.Governance())
	require.Nil(t, nilConv.PackGovernance())
	require.Nil(t, (&Conversation{}).Governance())
	require.Nil(t, (&Conversation{}).PackGovernance())

	populated := govConversation(t, "billing", `{"autonomy_level":"suggests"}`, "")
	require.Equal(t, "suggests", populated.Governance().AutonomyLevel,
		"the same methods must return a declaration when the pack has one")
	require.Equal(t, "suggests", populated.PackGovernance().AutonomyLevel)
}

// TestGovernanceIsACopy — a host adjusting what it read must not rewrite the
// loaded pack for every other reader.
func TestGovernanceIsACopy(t *testing.T) {
	c := govConversation(t, "billing",
		`{"autonomy_level":"suggests","approved_environments":["production"]}`, "")

	got := c.Governance()
	got.AutonomyLevel = "acts_autonomously"
	got.ApprovedEnvironments[0] = "anywhere"

	require.Equal(t, "suggests", c.Governance().AutonomyLevel,
		"a second read must not see the first reader's edit")
	require.Equal(t, []string{"production"}, c.Governance().ApprovedEnvironments)
}

func TestSDKDescribeGovernance(t *testing.T) {
	got := DescribeGovernance(&Governance{
		AutonomyLevel:        "acts_with_approval",
		RequiresAIDisclosure: packspec.Ptr(true),
	})
	require.Contains(t, got, "acts_with_approval")
	require.Contains(t, got, "must disclose as AI")
	require.Empty(t, DescribeGovernance(nil))
}

// TestGovernanceWithANilAgentDefinition — a pack may declare a member with no
// body ("members":{"billing":null}). ResolveGovernance rejects that as declared
// but empty, which is the reachable path through Governance's error branch. An
// agent that declares nothing inherits the pack, so the fallback is also the
// right answer, not just a safe one.
func TestGovernanceWithANilAgentDefinition(t *testing.T) {
	src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
	  "template_engine":{"version":"v1","syntax":"{{variable}}"},
	  "prompts":{},"metadata":{"governance":{"autonomy_level":"suggests"}},
	  "agents":{"entry":"billing","members":{"billing":null}}}`

	var p pack.Pack
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	require.Contains(t, p.Agents.Members, "billing")
	require.Nil(t, p.Agents.Members["billing"], "the member must decode to a nil definition")

	c := &Conversation{pack: &p, promptName: "billing"}
	got := c.Governance()
	require.NotNil(t, got, "an empty agent definition must still inherit the pack declaration")
	require.Equal(t, "suggests", got.AutonomyLevel)
}
