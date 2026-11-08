package sdk

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

func TestWithEvalTriggers_Validates(t *testing.T) {
	cfg := &config{}
	if err := WithEvalTriggers(evals.TriggerEveryTurn, evals.TriggerOnWorkflowStep)(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.evalTriggers) != 2 {
		t.Fatalf("evalTriggers = %v", cfg.evalTriggers)
	}
	err := WithEvalTriggers("every_tunr")(&config{})
	if err == nil || !strings.Contains(err.Error(), `"every_tunr"`) || !strings.Contains(err.Error(), "on_workflow_step") {
		t.Fatalf("unknown trigger: %v", err)
	}
	if err := WithEvalTriggers()(&config{}); err == nil {
		t.Fatal("no triggers should be an error")
	}
}

func TestFilterByTriggers(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "t", Trigger: evals.TriggerEveryTurn},
		{ID: "s", Trigger: evals.TriggerOnSessionComplete},
		{ID: "c", Trigger: evals.TriggerOnConversationComplete},
	}
	if got := filterByTriggers(defs, nil); len(got) != 3 {
		t.Errorf("unset filter kept %d defs, want 3", len(got))
	}
	got := filterByTriggers(defs, []evals.EvalTrigger{evals.TriggerEveryTurn, evals.TriggerOnConversationComplete})
	if len(got) != 2 || got[0].ID != "t" || got[1].ID != "c" {
		t.Errorf("filter kept %v", got)
	}
}
