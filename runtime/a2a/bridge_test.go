package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// serveAgentCard returns an httptest.Server that serves the given AgentCard
// at /.well-known/agent.json.
func serveAgentCard(t *testing.T, card AgentCard) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(card); err != nil {
			t.Fatalf("encode agent card: %v", err)
		}
	}))
}

// schemaProps extracts the "properties" map from a JSON Schema RawMessage.
func schemaProps(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties")
	}
	return props
}

// schemaRequired extracts the "required" array from a JSON Schema RawMessage.
func schemaRequired(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	reqRaw, ok := schema["required"]
	if !ok {
		return nil
	}
	arr, ok := reqRaw.([]any)
	if !ok {
		t.Fatal("required is not an array")
	}
	result := make([]string, len(arr))
	for i, v := range arr {
		result[i] = v.(string)
	}
	return result
}

func TestToolBridge_RegisterAgent_TextOnly(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "echo",
		Skills: []AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Echoes input",
				InputModes:  []string{"text/plain"},
				OutputModes: []string{"text/plain"},
			},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if len(tds) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tds))
	}

	td := tds[0]

	// Input: should have only "query"
	inProps := schemaProps(t, td.InputSchema)
	if _, ok := inProps["query"]; !ok {
		t.Error("input schema missing 'query'")
	}
	if _, ok := inProps["image_url"]; ok {
		t.Error("text-only skill should not have 'image_url'")
	}
	if _, ok := inProps["audio_data"]; ok {
		t.Error("text-only skill should not have 'audio_data'")
	}
	req := schemaRequired(t, td.InputSchema)
	if len(req) != 1 || req[0] != "query" {
		t.Errorf("expected required=[query], got %v", req)
	}

	// Output: should have only "response"
	outProps := schemaProps(t, td.OutputSchema)
	if _, ok := outProps["response"]; !ok {
		t.Error("output schema missing 'response'")
	}
	if _, ok := outProps["media_url"]; ok {
		t.Error("text-only skill should not have 'media_url'")
	}
}

func TestToolBridge_RegisterAgent_Multimodal(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "vision",
		Skills: []AgentSkill{
			{
				ID:          "describe",
				Name:        "Describe Image",
				Description: "Describes an image",
				InputModes:  []string{"text/plain", "image/*"},
				OutputModes: []string{"text/plain"},
			},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	td := tds[0]
	inProps := schemaProps(t, td.InputSchema)
	if _, ok := inProps["image_url"]; !ok {
		t.Error("multimodal input should have 'image_url'")
	}
	if _, ok := inProps["image_data"]; !ok {
		t.Error("multimodal input should have 'image_data'")
	}
}

func TestToolBridge_RegisterAgent_AudioInput(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "transcriber",
		Skills: []AgentSkill{
			{
				ID:          "transcribe",
				Name:        "Transcribe Audio",
				Description: "Transcribes audio",
				InputModes:  []string{"text/plain", "audio/*"},
				OutputModes: []string{"text/plain"},
			},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	td := tds[0]
	inProps := schemaProps(t, td.InputSchema)
	if _, ok := inProps["audio_data"]; !ok {
		t.Error("audio input should have 'audio_data'")
	}
}

func TestToolBridge_RegisterAgent_MultimodalOutput(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "generator",
		Skills: []AgentSkill{
			{
				ID:          "generate",
				Name:        "Generate Image",
				Description: "Generates an image",
				InputModes:  []string{"text/plain"},
				OutputModes: []string{"text/plain", "image/png"},
			},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	td := tds[0]
	outProps := schemaProps(t, td.OutputSchema)
	if _, ok := outProps["media_url"]; !ok {
		t.Error("multimodal output should have 'media_url'")
	}
	if _, ok := outProps["media_type"]; !ok {
		t.Error("multimodal output should have 'media_type'")
	}
}

func TestToolBridge_RegisterAgent_MultipleSkills(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "multi",
		Skills: []AgentSkill{
			{ID: "skill_a", Name: "A"},
			{ID: "skill_b", Name: "B"},
			{ID: "skill_c", Name: "C"},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if len(tds) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tds))
	}
}

func TestToolBridge_RegisterAgent_InheritsModes(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name:              "default_modes",
		DefaultInputModes: []string{"text/plain", "image/*"},
		DefaultOutputModes: []string{"text/plain", "audio/wav"},
		Skills: []AgentSkill{
			{
				ID:   "inherit",
				Name: "Inheriting Skill",
				// No InputModes/OutputModes — should inherit from card defaults
			},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	td := tds[0]

	// Input should inherit image/* from card defaults
	inProps := schemaProps(t, td.InputSchema)
	if _, ok := inProps["image_url"]; !ok {
		t.Error("inherited input should have 'image_url' from card defaults")
	}

	// Output should inherit audio/wav from card defaults
	outProps := schemaProps(t, td.OutputSchema)
	if _, ok := outProps["media_url"]; !ok {
		t.Error("inherited output should have 'media_url' from card defaults")
	}
}

func TestToolBridge_RegisterAgent_ToolNaming(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "Weather Bot",
		Skills: []AgentSkill{
			{ID: "forecast", Name: "Forecast"},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	expected := "a2a__weather_bot__forecast"
	if tds[0].Name != expected {
		t.Errorf("expected name %q, got %q", expected, tds[0].Name)
	}
}

func TestToolBridge_RegisterAgent_NameSanitization(t *testing.T) {
	tests := []struct {
		agentName string
		skillID   string
		want      string
	}{
		{"My Agent!", "do-stuff", "a2a__my_agent__do_stuff"},
		{"UPPER", "CASE", "a2a__upper__case"},
		{"  spaces  ", "  skill  ", "a2a__spaces__skill"},
		{"multi---dash", "under___score", "a2a__multi_dash__under_score"},
	}

	for _, tt := range tests {
		srv := serveAgentCard(t, AgentCard{
			Name: tt.agentName,
			Skills: []AgentSkill{
				{ID: tt.skillID, Name: "Test"},
			},
		})

		bridge := NewToolBridge(NewClient(srv.URL))
		tds, err := bridge.RegisterAgent(context.Background())
		if err != nil {
			srv.Close()
			t.Fatalf("RegisterAgent: %v", err)
		}
		if tds[0].Name != tt.want {
			t.Errorf("sanitize(%q, %q) = %q, want %q",
				tt.agentName, tt.skillID, tds[0].Name, tt.want)
		}
		srv.Close()
	}
}

func TestToolBridge_RegisterAgent_A2AConfig(t *testing.T) {
	srv := serveAgentCard(t, AgentCard{
		Name: "configured",
		Skills: []AgentSkill{
			{ID: "my_skill", Name: "My Skill"},
		},
	})
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	tds, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	td := tds[0]
	if td.Mode != "a2a" {
		t.Errorf("expected mode 'a2a', got %q", td.Mode)
	}
	if td.A2AConfig == nil {
		t.Fatal("A2AConfig is nil")
	}
	if td.A2AConfig.AgentURL != srv.URL {
		t.Errorf("AgentURL = %q, want %q", td.A2AConfig.AgentURL, srv.URL)
	}
	if td.A2AConfig.SkillID != "my_skill" {
		t.Errorf("SkillID = %q, want %q", td.A2AConfig.SkillID, "my_skill")
	}
}

func TestToolBridge_RegisterAgent_DiscoverError(t *testing.T) {
	// Server that always returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	_, err := bridge.RegisterAgent(context.Background())
	if err == nil {
		t.Fatal("expected error from discover failure")
	}
}

func TestToolBridge_GetToolDescriptors(t *testing.T) {
	// Register two agents and verify accumulation.
	srv1 := serveAgentCard(t, AgentCard{
		Name: "agent1",
		Skills: []AgentSkill{
			{ID: "s1", Name: "Skill 1"},
		},
	})
	defer srv1.Close()

	srv2 := serveAgentCard(t, AgentCard{
		Name: "agent2",
		Skills: []AgentSkill{
			{ID: "s2", Name: "Skill 2"},
			{ID: "s3", Name: "Skill 3"},
		},
	})
	defer srv2.Close()

	bridge := NewToolBridge(NewClient(srv1.URL))
	_, err := bridge.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent agent1: %v", err)
	}

	// Register second agent using a new bridge-internal client.
	// For multi-agent, we re-create with a second client.
	bridge2 := NewToolBridge(NewClient(srv2.URL))
	tds2, err := bridge2.RegisterAgent(context.Background())
	if err != nil {
		t.Fatalf("RegisterAgent agent2: %v", err)
	}

	// Manually append to first bridge's descriptors to simulate multi-agent usage.
	bridge.tools = append(bridge.tools, tds2...)

	all := bridge.GetToolDescriptors()
	if len(all) != 3 {
		t.Fatalf("expected 3 accumulated tools, got %d", len(all))
	}

	// Verify names.
	names := make(map[string]bool)
	for _, td := range all {
		names[td.Name] = true
	}
	for _, want := range []string{"a2a__agent1__s1", "a2a__agent2__s2", "a2a__agent2__s3"} {
		if !names[want] {
			t.Errorf("missing expected tool %q", want)
		}
	}
}

// Verify that the unused import doesn't cause issues — tools is used for A2AConfig type assertions.
var _ *tools.A2AConfig
