package evals

import "testing"

func TestShouldRun_EveryTurn(t *testing.T) {
	ctx := &TriggerContext{SessionID: "s1", TurnIndex: 0}
	if !ShouldRun(TriggerEveryTurn, 0, ctx) {
		t.Error("every_turn should always return true")
	}
}

func TestShouldRun_OnSessionComplete(t *testing.T) {
	tests := []struct {
		name      string
		complete  bool
		wantRun   bool
	}{
		{"session complete", true, true},
		{"session not complete", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TriggerContext{
				SessionID:         "s1",
				IsSessionComplete: tt.complete,
			}
			got := ShouldRun(TriggerOnSessionComplete, 0, ctx)
			if got != tt.wantRun {
				t.Errorf("got %v, want %v", got, tt.wantRun)
			}
		})
	}
}

func TestShouldRun_UnknownTrigger(t *testing.T) {
	ctx := &TriggerContext{SessionID: "s1"}
	if ShouldRun("bogus_trigger", 100, ctx) {
		t.Error("unknown trigger should return false")
	}
}

func TestShouldRun_SampleTurns_Deterministic(t *testing.T) {
	ctx := &TriggerContext{SessionID: "session-abc", TurnIndex: 5}
	// Same input must always produce the same result.
	first := ShouldRun(TriggerSampleTurns, 50, ctx)
	for range 100 {
		if ShouldRun(TriggerSampleTurns, 50, ctx) != first {
			t.Fatal("sampling should be deterministic")
		}
	}
}

func TestShouldRun_SampleTurns_DifferentTurns(t *testing.T) {
	// With 50% sampling over many turns, we expect some true and some false.
	trueCount := 0
	for i := range 200 {
		ctx := &TriggerContext{SessionID: "sess-x", TurnIndex: i}
		if ShouldRun(TriggerSampleTurns, 50, ctx) {
			trueCount++
		}
	}
	// Expect roughly 100, but allow wide range to avoid flaky test.
	if trueCount == 0 || trueCount == 200 {
		t.Errorf(
			"expected mixed results with 50%% sampling, got %d/200 true",
			trueCount,
		)
	}
}

func TestShouldRun_SampleSessions_SameTurnDecision(t *testing.T) {
	// Session sampling should give the same decision regardless of turn.
	ctx0 := &TriggerContext{SessionID: "stable-session", TurnIndex: 0}
	result := ShouldRun(TriggerSampleSessions, 50, ctx0)

	for i := 1; i <= 20; i++ {
		ctx := &TriggerContext{SessionID: "stable-session", TurnIndex: i}
		if ShouldRun(TriggerSampleSessions, 50, ctx) != result {
			t.Fatalf(
				"session sampling should be same for all turns, "+
					"turn 0=%v but turn %d differs",
				result, i,
			)
		}
	}
}

func TestShouldRun_SampleTurns_ZeroPercent(t *testing.T) {
	for i := range 50 {
		ctx := &TriggerContext{SessionID: "s", TurnIndex: i}
		if ShouldRun(TriggerSampleTurns, 0, ctx) {
			t.Fatal("0% sampling should never fire")
		}
	}
}

func TestShouldRun_SampleTurns_HundredPercent(t *testing.T) {
	for i := range 50 {
		ctx := &TriggerContext{SessionID: "s", TurnIndex: i}
		if !ShouldRun(TriggerSampleTurns, 100, ctx) {
			t.Fatal("100% sampling should always fire")
		}
	}
}

func TestSampleHit_Boundary(t *testing.T) {
	// Directly test sampleHit at boundaries.
	if sampleHit("x", 0, 0) {
		t.Error("0% should never hit")
	}
	if !sampleHit("x", 0, 100) {
		t.Error("100% should always hit")
	}
}
