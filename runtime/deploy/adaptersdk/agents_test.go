package adaptersdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// multiAgentPack returns a two-member pack used across several tests.
func multiAgentPack() *prompt.Pack {
	return &prompt.Pack{
		ID:      "test-pack",
		Version: "v1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"router": {
				ID:          "router",
				Name:        "Router",
				Description: "Routes requests",
			},
			"worker": {
				ID:          "worker",
				Name:        "Worker",
				Description: "Processes tasks",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "router",
			Members: map[string]*prompt.AgentDef{
				"router": {
					Description: "Entry router agent",
					Tags:        []string{"routing"},
					InputModes:  []string{"text/plain", "application/json"},
					OutputModes: []string{"text/plain"},
				},
				"worker": {
					Tags: []string{"processing"},
				},
			},
		},
	}
}

func TestIsMultiAgent(t *testing.T) {
	tests := []struct {
		name string
		pack *prompt.Pack
		want bool
	}{
		{
			name: "nil agents",
			pack: &prompt.Pack{},
			want: false,
		},
		{
			name: "empty members",
			pack: &prompt.Pack{
				Agents: &prompt.AgentsConfig{
					Members: map[string]*prompt.AgentDef{},
				},
			},
			want: false,
		},
		{
			name: "valid agents",
			pack: multiAgentPack(),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsMultiAgent(tc.pack)
			if got != tc.want {
				t.Errorf("IsMultiAgent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractAgents(t *testing.T) {
	t.Run("nil when not multi-agent", func(t *testing.T) {
		pack := &prompt.Pack{}
		agents := ExtractAgents(pack)
		if agents != nil {
			t.Fatalf("expected nil, got %v", agents)
		}
	})

	t.Run("sorted output with correct fields", func(t *testing.T) {
		pack := multiAgentPack()
		agents := ExtractAgents(pack)

		if len(agents) != 2 {
			t.Fatalf("expected 2 agents, got %d", len(agents))
		}

		// Sorted alphabetically: router, worker
		if agents[0].Name != "router" {
			t.Errorf("expected first agent name 'router', got %q", agents[0].Name)
		}
		if agents[1].Name != "worker" {
			t.Errorf("expected second agent name 'worker', got %q", agents[1].Name)
		}
	})

	t.Run("entry agent marked correctly", func(t *testing.T) {
		pack := multiAgentPack()
		agents := ExtractAgents(pack)

		for _, a := range agents {
			if a.Name == "router" && !a.IsEntry {
				t.Error("router should be marked as entry")
			}
			if a.Name == "worker" && a.IsEntry {
				t.Error("worker should not be marked as entry")
			}
		}
	})

	t.Run("description falls back to prompt", func(t *testing.T) {
		pack := multiAgentPack()
		agents := ExtractAgents(pack)

		// worker has no agent-level description; should fall back to prompt description.
		for _, a := range agents {
			if a.Name == "worker" {
				if a.Description != "Processes tasks" {
					t.Errorf("expected fallback description 'Processes tasks', got %q", a.Description)
				}
			}
			if a.Name == "router" {
				if a.Description != "Entry router agent" {
					t.Errorf("expected agent description 'Entry router agent', got %q", a.Description)
				}
			}
		}
	})

	t.Run("default modes applied when empty", func(t *testing.T) {
		pack := multiAgentPack()
		agents := ExtractAgents(pack)

		for _, a := range agents {
			if a.Name == "router" {
				if len(a.InputModes) != 2 || a.InputModes[0] != "text/plain" {
					t.Errorf("router input modes incorrect: %v", a.InputModes)
				}
			}
			if a.Name == "worker" {
				// Worker has no modes defined; should default to text/plain.
				if len(a.InputModes) != 1 || a.InputModes[0] != "text/plain" {
					t.Errorf("worker input modes should default to [text/plain], got %v", a.InputModes)
				}
				if len(a.OutputModes) != 1 || a.OutputModes[0] != "text/plain" {
					t.Errorf("worker output modes should default to [text/plain], got %v", a.OutputModes)
				}
			}
		}
	})
}

func TestGenerateAgentCards(t *testing.T) {
	t.Run("delegates to agentcard package", func(t *testing.T) {
		pack := multiAgentPack()
		cards := GenerateAgentCards(pack)

		if cards == nil {
			t.Fatal("expected non-nil cards")
		}
		if len(cards) != 2 {
			t.Fatalf("expected 2 cards, got %d", len(cards))
		}
		if cards["router"] == nil {
			t.Error("expected card for 'router'")
		}
		if cards["worker"] == nil {
			t.Error("expected card for 'worker'")
		}
	})

	t.Run("nil for non-multi-agent pack", func(t *testing.T) {
		pack := &prompt.Pack{}
		cards := GenerateAgentCards(pack)
		if cards != nil {
			t.Errorf("expected nil cards, got %v", cards)
		}
	})
}

func TestGenerateAgentResourcePlan(t *testing.T) {
	t.Run("nil for non-multi-agent pack", func(t *testing.T) {
		pack := &prompt.Pack{}
		changes := GenerateAgentResourcePlan(pack)
		if changes != nil {
			t.Errorf("expected nil, got %v", changes)
		}
	})

	t.Run("correct resources for multi-agent pack", func(t *testing.T) {
		pack := multiAgentPack()
		changes := GenerateAgentResourcePlan(pack)

		// 2 members * 2 resources each + 1 gateway = 5
		if len(changes) != 5 {
			t.Fatalf("expected 5 resource changes, got %d", len(changes))
		}

		// Count resource types.
		typeCounts := map[string]int{}
		for _, c := range changes {
			typeCounts[c.Type]++
		}

		if typeCounts["agent_runtime"] != 2 {
			t.Errorf("expected 2 agent_runtime resources, got %d", typeCounts["agent_runtime"])
		}
		if typeCounts["a2a_endpoint"] != 2 {
			t.Errorf("expected 2 a2a_endpoint resources, got %d", typeCounts["a2a_endpoint"])
		}
		if typeCounts["gateway"] != 1 {
			t.Errorf("expected 1 gateway resource, got %d", typeCounts["gateway"])
		}

		// All actions should be CREATE.
		for _, c := range changes {
			if c.Action != deploy.ActionCreate {
				t.Errorf("expected ActionCreate for %s/%s, got %s", c.Type, c.Name, c.Action)
			}
		}

		// Gateway should be for the entry agent.
		gateway := changes[len(changes)-1]
		if gateway.Name != "router_gateway" {
			t.Errorf("expected gateway name 'router_gateway', got %q", gateway.Name)
		}
	})

	t.Run("deterministic ordering by name", func(t *testing.T) {
		pack := multiAgentPack()

		// Run multiple times to verify determinism.
		for i := range 5 {
			changes := GenerateAgentResourcePlan(pack)

			// First two should be router resources (alphabetically before worker).
			if changes[0].Name != "router" {
				t.Errorf("iteration %d: expected first resource for 'router', got %q", i, changes[0].Name)
			}
			if changes[2].Name != "worker" {
				t.Errorf("iteration %d: expected third resource for 'worker', got %q", i, changes[2].Name)
			}
		}
	})
}
