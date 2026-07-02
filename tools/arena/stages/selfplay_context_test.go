package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

func TestNewSelfPlayUserTurnContextStageWithHintAndTurnState(t *testing.T) {
	scenario := &arenaconfig.Scenario{ID: "s1"}
	ts := stage.NewTurnState()
	stg := NewSelfPlayUserTurnContextStageWithHintAndTurnState(scenario, 2, "persona1", ts)
	if stg == nil {
		t.Fatal("expected non-nil stage")
	}
	if stg.scenario != scenario || stg.turnIndexHint != 2 || stg.personaID != "persona1" || stg.turnState != ts {
		t.Errorf("stage fields not set correctly: %+v", stg)
	}
}
