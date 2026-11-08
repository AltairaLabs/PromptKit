package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// A host runner's own registry decides which defs are usable: a type it does
// not hold is skipped, as Open skips it with the SDK's runner.
func TestNewEvalMiddleware_ExplicitRunnerFiltersAgainstItsRegistry(t *testing.T) {
	conv := &Conversation{
		config: &config{evalRunner: evals.NewEvalRunner(evals.NewEmptyEvalTypeRegistry())},
		pack: &pack.Pack{Pack: packspec.Pack{Evals: []*evals.EvalDef{
			{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn, Params: map[string]any{"patterns": []any{"x"}}},
		}}},
		prompt: &pack.Prompt{},
	}
	if mw := newEvalMiddleware(conv); mw != nil {
		t.Fatalf("a def the runner cannot run survived: %v", mw.defs)
	}
}

// Evaluate reports a def it cannot run as an error result only for the
// trigger class it dispatches.
func TestEvaluate_UnrunnableDefsFollowTheTriggerClass(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "turn-typo", Type: "contains", Trigger: evals.TriggerEveryTurn, Params: map[string]any{"patern": "x"}},
		{ID: "session-typo", Type: "contains", Trigger: evals.TriggerOnSessionComplete,
			Params: map[string]any{"patern": "x"}},
	}
	msgs := []types.Message{types.NewAssistantMessage("x")}
	for trigger, want := range map[evals.EvalTrigger]string{
		evals.TriggerEveryTurn:         "turn-typo",
		evals.TriggerOnSessionComplete: "session-typo",
	} {
		results, err := Evaluate(context.Background(), EvaluateOpts{EvalDefs: defs, Messages: msgs, Trigger: trigger})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].EvalID != want || !strings.Contains(results[0].Error, `"patern"`) {
			t.Errorf("%s: got %+v, want one error result for %s", trigger, results, want)
		}
	}
}
