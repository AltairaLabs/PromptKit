package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// panicExecutor simulates a conversation executor that panics mid-run (e.g. a
// nil deref on a malformed provider response in the synchronous path).
type panicExecutor struct{}

func (panicExecutor) ExecuteConversation(context.Context, ConversationRequest) *ConversationResult {
	panic("boom: simulated executor panic")
}

func (panicExecutor) ExecuteConversationStream(
	context.Context, ConversationRequest,
) (<-chan ConversationStreamChunk, error) {
	panic("boom: simulated executor stream panic")
}

type recordingSessionHook struct {
	started bool
	ended   bool
}

func (h *recordingSessionHook) Name() string { return "recording" }
func (h *recordingSessionHook) OnSessionStart(context.Context, hooks.SessionEvent) error {
	h.started = true
	return nil
}
func (h *recordingSessionHook) OnSessionUpdate(context.Context, hooks.SessionEvent) error { return nil }
func (h *recordingSessionHook) OnSessionEnd(context.Context, hooks.SessionEvent) error {
	h.ended = true
	return nil
}

// A run that panics must NOT crash the process or the batch: the panic is
// recovered, the run is recorded, and OnSessionEnd still fires so the
// capture/state hook always runs.
func TestExecuteRun_PanicRecoveredAndSessionEndFires(t *testing.T) {
	cfg := &arenaconfig.Config{
		StateStore: &config.StateStoreConfig{Type: "memory"},
		LoadedScenarios: map[string]*arenaconfig.Scenario{
			"s": {
				ID:       "s",
				TaskType: "assistance",
				Turns:    []arenaconfig.TurnDefinition{{Role: "user", Content: "hi"}},
			},
		},
		LoadedProviders: map[string]*config.Provider{
			"p": {ID: "p", Type: "mock", Model: "m"},
		},
	}
	providerRegistry := providers.NewRegistry()
	providerRegistry.Register(&testProvider{id: "p"})

	eng, err := NewEngine(cfg, providerRegistry, nil, nil, panicExecutor{}, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	hook := &recordingSessionHook{}
	eng.WithSessionHooks(hooks.NewRegistry(hooks.WithSessionHook(hook)))

	plan := &RunPlan{Combinations: []RunCombination{
		{Region: "default", ScenarioID: "s", ProviderID: "p"},
	}}

	// Must not panic the test binary, and must return the run.
	runIDs, err := eng.ExecuteRuns(context.Background(), plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns must not fail at the batch level on a per-run panic: %v", err)
	}
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run id, got %d", len(runIDs))
	}
	if !hook.started {
		t.Error("OnSessionStart should have fired before the panic")
	}
	if !hook.ended {
		t.Error("OnSessionEnd must fire even when the run panics (so state is captured)")
	}
}
