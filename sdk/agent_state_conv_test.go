package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// RFC 0011: packToRuntimePack must carry AgentDef.State through to the runtime pack.
func TestPackToRuntimePack_CarriesAgentState(t *testing.T) {
	p := &pack.Pack{
		ID:      "x",
		Version: "1",
		Agents: &pack.AgentsConfig{
			Entry: "a",
			Members: map[string]*pack.AgentDef{
				"a":      {},
				"triage": {State: "triage"},
			},
		},
	}
	rp := packToRuntimePack(p)
	if rp.Agents == nil {
		t.Fatal("agents not converted")
	}
	if got := rp.Agents.Members["triage"].State; got != "triage" {
		t.Fatalf("State not carried through: %q", got)
	}
}
