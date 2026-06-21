package main

import (
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
)

func TestChatCmd_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"chat"})
	if err != nil {
		t.Fatalf("find chat: %v", err)
	}
	if cmd.Name() != "chat" {
		t.Fatalf("want chat command, got %q", cmd.Name())
	}
	if cmd.Flag("config") == nil {
		t.Fatalf("chat command missing --config flag")
	}
	if cmd.Flag("mock-provider") == nil {
		t.Fatalf("chat command missing --mock-provider flag")
	}
}

// interactiveFixtureAdapter builds an engineChatAdapter backed by the
// shared interactive test fixture. The engine is closed on test cleanup.
func interactiveFixtureAdapter(t *testing.T) *engineChatAdapter {
	t.Helper()
	cfg := filepath.Join("..", "..", "engine", "testdata", "interactive", "config.arena.yaml")
	eng, err := engine.NewEngineFromConfigFile(filepath.Clean(cfg))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	return &engineChatAdapter{eng: eng}
}

func TestEngineChatAdapter_Agents(t *testing.T) {
	a := interactiveFixtureAdapter(t)
	agents := a.Agents()
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	if agents[0].TaskType != "basic" {
		t.Fatalf("want task_type basic, got %q", agents[0].TaskType)
	}
}

func TestEngineChatAdapter_ProviderIDs(t *testing.T) {
	a := interactiveFixtureAdapter(t)
	ids := a.ProviderIDs()
	if len(ids) == 0 {
		t.Fatal("want at least one provider ID, got none")
	}
}

func TestEngineChatAdapter_MissingRequiredVars(t *testing.T) {
	a := interactiveFixtureAdapter(t)

	missing, err := a.MissingRequiredVars("basic", nil)
	if err != nil {
		t.Fatalf("MissingRequiredVars: %v", err)
	}
	if len(missing) == 0 {
		t.Fatal("want at least one missing var, got none")
	}

	missing, err = a.MissingRequiredVars("basic", map[string]string{"company": "Acme"})
	if err != nil {
		t.Fatalf("MissingRequiredVars (provided): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("want no missing vars when provided, got %v", missing)
	}
}

func TestEngineChatAdapter_HasConfigEvals(t *testing.T) {
	a := interactiveFixtureAdapter(t)
	// HasConfigEvals should not panic; the fixture may or may not have evals.
	_ = a.HasConfigEvals()
}

func TestEngineChatAdapter_NewChatSession_OK(t *testing.T) {
	a := interactiveFixtureAdapter(t)
	ids := a.ProviderIDs()
	if len(ids) == 0 {
		t.Fatal("no providers in fixture")
	}
	sess, err := a.NewChatSession(pages.ChatSessionOptions{
		ProviderID: ids[0],
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
		RunEvals:   false,
	})
	if err != nil {
		t.Fatalf("NewChatSession: %v", err)
	}
	if sess == nil {
		t.Fatal("want non-nil session")
	}
}

func TestEngineChatAdapter_NewChatSession_ProviderNotFound(t *testing.T) {
	a := interactiveFixtureAdapter(t)
	_, err := a.NewChatSession(pages.ChatSessionOptions{
		ProviderID: "no-such-provider",
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
	})
	if err == nil {
		t.Fatal("want error for missing provider, got nil")
	}
}
