package agentcard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// TestGeneratedCard_IsConsumedByTheToolBridge serves the card GenerateAgentCards
// builds for a pack agent, the way an A2A server publishes it, and lets the
// runtime's own A2A ToolBridge discover it. The tool the calling side ends up
// with is the contract between the two: its name comes from the card's name
// and skill ID, its description from the skill, and its input schema from the
// agent's input modes.
func TestGeneratedCard_IsConsumedByTheToolBridge(t *testing.T) {
	pack := &prompt.Pack{Pack: packspec.Pack{
		Version: "v1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"vision": {Name: "Vision Agent", Description: "Looks at pictures"},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "vision",
			Members: map[string]*prompt.AgentDef{
				"vision": {InputModes: []string{"text/plain", "image/png"}},
			},
		},
	}}
	card := GenerateAgentCards(pack)["vision"]
	if card == nil {
		t.Fatal("no card generated for the vision agent")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != a2a.AgentCardPath {
			http.NotFound(w, r)
			return
		}
		published := *card
		published.SupportedInterfaces = []a2a.AgentInterface{{
			URL: "http://" + r.Host + "/", ProtocolBinding: a2a.ProtocolBindingJSONRPC, ProtocolVersion: "1.0",
		}}
		_ = json.NewEncoder(w).Encode(published)
	}))
	defer srv.Close()

	descs, err := a2a.NewToolBridge(a2a.NewClient(srv.URL)).RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("got %d tools, want 1", len(descs))
	}
	d := descs[0]
	if d.Name != "a2a__vision_agent__vision" {
		t.Errorf("tool name = %q, want a2a__vision_agent__vision", d.Name)
	}
	if d.Description != "Looks at pictures" {
		t.Errorf("tool description = %q, want the prompt's description", d.Description)
	}
	if d.A2AConfig == nil || d.A2AConfig.SkillID != "vision" {
		t.Errorf("A2AConfig = %+v, want SkillID vision", d.A2AConfig)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"query", "image_url", "image_data"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("input schema lacks %q; an image-input agent must accept images", field)
		}
	}
	if _, ok := schema.Properties["audio_data"]; ok {
		t.Error("input schema offers audio_data to an agent that takes no audio")
	}
}
