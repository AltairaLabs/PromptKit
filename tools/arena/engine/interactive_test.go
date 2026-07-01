package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

const interactiveFixture = "testdata/interactive/config.arena.yaml"

func newFixtureEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngineFromConfigFile(filepath.Clean(interactiveFixture))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestEngine_Agents(t *testing.T) {
	eng := newFixtureEngine(t)
	agents := eng.Agents()
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	if agents[0].TaskType != "basic" {
		t.Fatalf("want task_type basic, got %q", agents[0].TaskType)
	}
}

func TestEngine_ProviderIDs(t *testing.T) {
	eng := newFixtureEngine(t)
	ids := eng.ProviderIDs()
	if len(ids) != 1 || ids[0] != "mock" {
		t.Fatalf("want [mock], got %v", ids)
	}
}

func TestEngine_MissingRequiredVars(t *testing.T) {
	eng := newFixtureEngine(t)

	missing, err := eng.MissingRequiredVars("basic", nil)
	if err != nil {
		t.Fatalf("MissingRequiredVars: %v", err)
	}
	if len(missing) != 1 || missing[0] != "company" {
		t.Fatalf("want [company] missing, got %v", missing)
	}

	// Blank value counts as missing.
	missing, err = eng.MissingRequiredVars("basic", map[string]string{"company": ""})
	if err != nil {
		t.Fatalf("MissingRequiredVars (blank): %v", err)
	}
	if len(missing) != 1 || missing[0] != "company" {
		t.Fatalf("want [company] missing for blank value, got %v", missing)
	}

	missing, err = eng.MissingRequiredVars("basic", map[string]string{"company": "Acme"})
	if err != nil {
		t.Fatalf("MissingRequiredVars (provided): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("want none missing, got %v", missing)
	}
}

func TestEngine_MissingRequiredVars_UnknownTaskType(t *testing.T) {
	eng := newFixtureEngine(t)
	_, err := eng.MissingRequiredVars("no-such-task", nil)
	if err == nil {
		t.Fatal("want error for unknown task type, got nil")
	}
}

func newMockSession(t *testing.T, runEvals bool) *InteractiveSession {
	t.Helper()
	eng := newFixtureEngine(t)
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock",
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
		RunEvals:   runEvals,
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	return sess
}

func TestInteractiveSession_SendUserMessage(t *testing.T) {
	sess := newMockSession(t, false)
	ctx := context.Background()

	ch, err := sess.SendUserMessage(ctx, "Hello there")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
	}

	msgs, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	var sawUser, sawAssistant bool
	for i := range msgs {
		switch msgs[i].Role {
		case "user":
			sawUser = true
		case "assistant":
			sawAssistant = true
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("want user+assistant messages, got user=%v assistant=%v (%d msgs)", sawUser, sawAssistant, len(msgs))
	}
}

func TestInteractiveSession_HistoryPersists(t *testing.T) {
	sess := newMockSession(t, false)
	ctx := context.Background()

	ch1, err := sess.SendUserMessage(ctx, "first")
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	for range ch1 { //nolint:revive // draining
	}
	ch2, err := sess.SendUserMessage(ctx, "second")
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	for range ch2 { //nolint:revive // draining
	}

	msgs, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	users := 0
	for i := range msgs {
		if msgs[i].Role == "user" {
			users++
		}
	}
	if users < 2 {
		t.Fatalf("want >= 2 user turns persisted, got %d", users)
	}
}

func TestInteractiveSession_FreshConversationIsEmpty(t *testing.T) {
	sess := newMockSession(t, false)
	_, err := sess.engine.GetStateStore().Load(context.Background(), sess.ConversationID())
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("unexpected load error: %v", err)
	}
}

func TestInteractiveSession_RunEvalsEnabled(t *testing.T) {
	sessOff := newMockSession(t, false)
	if sessOff.RunEvalsEnabled() {
		t.Fatal("want RunEvalsEnabled false, got true")
	}
	sessOn := newMockSession(t, true)
	if !sessOn.RunEvalsEnabled() {
		t.Fatal("want RunEvalsEnabled true, got false")
	}
}

func TestInteractiveSession_Cost(t *testing.T) {
	sess := newMockSession(t, false)
	ctx := context.Background()

	// Cost on a fresh session (empty) should return zero value.
	cost, err := sess.Cost(ctx)
	if err != nil {
		t.Fatalf("Cost on fresh session: %v", err)
	}
	if cost.TotalCost != 0 {
		t.Fatalf("want zero cost for fresh session, got %v", cost.TotalCost)
	}

	// After a turn, Cost should not error (mock provider may not emit cost).
	ch, err := sess.SendUserMessage(ctx, "cost test")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for range ch { //nolint:revive // draining
	}
	_, err = sess.Cost(ctx)
	if err != nil {
		t.Fatalf("Cost after turn: %v", err)
	}
}

func TestNewInteractiveSession_ProviderNotFound(t *testing.T) {
	eng := newFixtureEngine(t)
	_, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "nonexistent",
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
	})
	if err == nil {
		t.Fatal("want error for missing provider, got nil")
	}
}

func TestScriptedExecutorFrom_DefaultExecutor(t *testing.T) {
	eng := newFixtureEngine(t)
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	// Unwrap to DefaultConversationExecutor directly to exercise that branch.
	composite, ok := eng.conversationExecutor.(*CompositeConversationExecutor)
	if !ok {
		t.Skip("executor is not composite, skipping branch test")
	}
	result := scriptedExecutorFrom(composite.defaultExecutor)
	if result == nil {
		t.Fatal("want scripted executor from DefaultConversationExecutor, got nil")
	}
}

func TestInteractiveSession_MessagesAfterTurn(t *testing.T) {
	sess := newMockSession(t, false)
	ctx := context.Background()

	ch, err := sess.SendUserMessage(ctx, "hello")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for range ch { //nolint:revive // draining
	}

	msgs, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("want at least one message, got none")
	}
}

func TestInteractiveSession_RunEvals_Enabled(t *testing.T) {
	sess := newMockSession(t, true)
	ctx := context.Background()

	ch, err := sess.SendUserMessage(ctx, "say something")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for range ch { //nolint:revive // draining
	}

	results, err := sess.RunEvals(ctx)
	if err != nil {
		t.Fatalf("RunEvals: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 eval result, got %d", len(results))
	}
	if results[0].Type != "json_valid" {
		t.Fatalf("want json_valid result, got %q", results[0].Type)
	}
}

func TestInteractiveSession_RunEvals_Disabled(t *testing.T) {
	sess := newMockSession(t, false)
	ctx := context.Background()

	ch, err := sess.SendUserMessage(ctx, "hi")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for range ch { //nolint:revive // draining
	}

	results, err := sess.RunEvals(ctx)
	if err != nil {
		t.Fatalf("RunEvals: %v", err)
	}
	if results != nil {
		t.Fatalf("want nil results when evals disabled, got %v", results)
	}
}

func TestNewInteractiveSession_PromptConfigVarsByTaskType(t *testing.T) {
	eng, err := NewEngineFromConfigFile(filepath.Clean("testdata/interactive_idvars/config.arena.yaml"))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock",
		TaskType:   "support",
		// No Variables — rely solely on prompt-config vars.
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	if got := sess.promptVars["company"]; got != "FixtureCo" {
		t.Fatalf("want promptVars[company]=FixtureCo, got %q (vars dropped by map-key mismatch?)", got)
	}
}

func TestInteractiveSession_ProviderAndHasConfigEvals(t *testing.T) {
	eng := newFixtureEngine(t)
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	// Exercise the config-evals accessor (value depends on fixture config).
	_ = eng.HasConfigEvals()
	sess, err := eng.NewInteractiveSession(InteractiveSessionOptions{
		ProviderID: "mock",
		TaskType:   "basic",
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	if sess.Provider() == nil {
		t.Error("Provider() = nil, want provider")
	}
}
