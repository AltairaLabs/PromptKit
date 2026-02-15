package agentcard

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

func TestGenerateAgentCards_NilPack(t *testing.T) {
	if cards := GenerateAgentCards(nil); cards != nil {
		t.Fatalf("expected nil, got %v", cards)
	}
}

func TestGenerateAgentCards_NilAgents(t *testing.T) {
	pack := &prompt.Pack{}
	if cards := GenerateAgentCards(pack); cards != nil {
		t.Fatalf("expected nil, got %v", cards)
	}
}

func TestGenerateAgentCards_ThreeAgents(t *testing.T) {
	pack := &prompt.Pack{
		Version: "v1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"coordinator": {
				Name:        "Coordinator Agent",
				Description: "Coordinates tasks across agents",
			},
			"researcher": {
				Name:        "Research Agent",
				Description: "Performs deep research",
			},
			"analyst": {
				Name:        "Analyst Agent",
				Description: "Analyzes data",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "coordinator",
			Members: map[string]*prompt.AgentDef{
				"coordinator": {
					Description: "Orchestrates the team",
					Tags:        []string{"orchestration", "routing"},
				},
				"researcher": {
					Tags:        []string{"research", "web"},
					InputModes:  []string{"text/plain", "application/json"},
					OutputModes: []string{"text/plain", "text/markdown"},
				},
				"analyst": {
					Tags: []string{"analysis"},
				},
			},
		},
	}

	cards := GenerateAgentCards(pack)
	if cards == nil {
		t.Fatal("expected non-nil cards")
	}
	if len(cards) != 3 {
		t.Fatalf("expected 3 cards, got %d", len(cards))
	}

	// Coordinator: agentDef description overrides prompt description
	coord := cards["coordinator"]
	assertCard(t, coord, "Coordinator Agent", "Orchestrates the team", "v1.0.0")
	assertSkill(t, coord.Skills[0], "coordinator", "Coordinator Agent", "Orchestrates the team",
		[]string{"orchestration", "routing"}, []string{"text/plain"}, []string{"text/plain"})

	// Researcher: no agentDef description, falls back to prompt description; custom modes
	res := cards["researcher"]
	assertCard(t, res, "Research Agent", "Performs deep research", "v1.0.0")
	assertSkill(t, res.Skills[0], "researcher", "Research Agent", "Performs deep research",
		[]string{"research", "web"},
		[]string{"text/plain", "application/json"},
		[]string{"text/plain", "text/markdown"})
	assertStringSlice(t, "DefaultInputModes", res.DefaultInputModes, []string{"text/plain", "application/json"})
	assertStringSlice(t, "DefaultOutputModes", res.DefaultOutputModes, []string{"text/plain", "text/markdown"})

	// Analyst: no agentDef description, falls back to prompt description; default modes
	ana := cards["analyst"]
	assertCard(t, ana, "Analyst Agent", "Analyzes data", "v1.0.0")
	assertSkill(t, ana.Skills[0], "analyst", "Analyst Agent", "Analyzes data",
		[]string{"analysis"}, []string{"text/plain"}, []string{"text/plain"})
}

func TestGenerateAgentCards_AgentWithoutMatchingPrompt(t *testing.T) {
	pack := &prompt.Pack{
		Version: "v2.0.0",
		Prompts: map[string]*prompt.PackPrompt{},
		Agents: &prompt.AgentsConfig{
			Entry: "orphan",
			Members: map[string]*prompt.AgentDef{
				"orphan": {
					Description: "No matching prompt",
					Tags:        []string{"solo"},
				},
			},
		},
	}

	cards := GenerateAgentCards(pack)
	if cards == nil {
		t.Fatal("expected non-nil cards")
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}

	card := cards["orphan"]
	assertCard(t, card, "orphan", "No matching prompt", "v2.0.0")
	assertSkill(t, card.Skills[0], "orphan", "orphan", "No matching prompt",
		[]string{"solo"}, []string{"text/plain"}, []string{"text/plain"})
}

func TestDefaultModes(t *testing.T) {
	// nil input
	got := defaultModes(nil)
	assertStringSlice(t, "nil", got, []string{"text/plain"})

	// empty input
	got = defaultModes([]string{})
	assertStringSlice(t, "empty", got, []string{"text/plain"})

	// custom input
	custom := []string{"image/png", "text/plain"}
	got = defaultModes(custom)
	assertStringSlice(t, "custom", got, custom)
}

// --- helpers ---

func assertCard(t *testing.T, card *a2a.AgentCard, name, desc, version string) {
	t.Helper()
	if card == nil {
		t.Fatal("card is nil")
	}
	if card.Name != name {
		t.Errorf("card.Name = %q, want %q", card.Name, name)
	}
	if card.Description != desc {
		t.Errorf("card.Description = %q, want %q", card.Description, desc)
	}
	if card.Version != version {
		t.Errorf("card.Version = %q, want %q", card.Version, version)
	}
}

func assertSkill(t *testing.T, skill a2a.AgentSkill, id, name, desc string, tags, inModes, outModes []string) {
	t.Helper()
	if skill.ID != id {
		t.Errorf("skill.ID = %q, want %q", skill.ID, id)
	}
	if skill.Name != name {
		t.Errorf("skill.Name = %q, want %q", skill.Name, name)
	}
	if skill.Description != desc {
		t.Errorf("skill.Description = %q, want %q", skill.Description, desc)
	}
	assertStringSlice(t, "tags", skill.Tags, tags)
	assertStringSlice(t, "inputModes", skill.InputModes, inModes)
	assertStringSlice(t, "outputModes", skill.OutputModes, outModes)
}

func assertStringSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: len = %d, want %d; got %v", label, len(got), len(want), got)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
