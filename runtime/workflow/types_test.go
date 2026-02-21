package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSpecRoundTrip(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*State{
			"intake": {
				PromptTask:  "gather_requirements",
				Description: "Gather user requirements",
				OnEvent: map[string]string{
					"IssueUnderstood": "solving",
					"NeedMoreInfo":    "intake",
				},
				Persistence:   PersistencePersistent,
				Orchestration: OrchestrationInternal,
			},
			"solving": {
				PromptTask: "create_solution",
				OnEvent: map[string]string{
					"SolutionAccepted": "confirmation",
					"SolutionRejected": "solving",
				},
			},
			"confirmation": {
				PromptTask:  "confirm_resolution",
				Description: "Terminal state",
			},
		},
		Engine: map[string]any{
			"timeout": 300,
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Version != spec.Version {
		t.Errorf("Version = %d, want %d", got.Version, spec.Version)
	}
	if got.Entry != spec.Entry {
		t.Errorf("Entry = %q, want %q", got.Entry, spec.Entry)
	}
	if len(got.States) != len(spec.States) {
		t.Fatalf("States count = %d, want %d", len(got.States), len(spec.States))
	}

	intake := got.States["intake"]
	if intake.PromptTask != "gather_requirements" {
		t.Errorf("intake.PromptTask = %q, want %q", intake.PromptTask, "gather_requirements")
	}
	if intake.Persistence != PersistencePersistent {
		t.Errorf("intake.Persistence = %q, want %q", intake.Persistence, PersistencePersistent)
	}
	if intake.Orchestration != OrchestrationInternal {
		t.Errorf("intake.Orchestration = %q, want %q", intake.Orchestration, OrchestrationInternal)
	}
	if intake.OnEvent["IssueUnderstood"] != "solving" {
		t.Errorf("intake.OnEvent[IssueUnderstood] = %q, want %q", intake.OnEvent["IssueUnderstood"], "solving")
	}

	confirmation := got.States["confirmation"]
	if len(confirmation.OnEvent) != 0 {
		t.Errorf("confirmation.OnEvent should be empty, got %v", confirmation.OnEvent)
	}

	if got.Engine["timeout"] != float64(300) {
		t.Errorf("Engine[timeout] = %v, want 300", got.Engine["timeout"])
	}
}

func TestSpecOmitempty(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*State{
			"start": {PromptTask: "task1"},
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["engine"]; ok {
		t.Error("engine field should be omitted when nil")
	}

	// State-level omitempty
	var states map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw["states"], &states); err != nil {
		t.Fatalf("unmarshal states: %v", err)
	}
	start := states["start"]
	if _, ok := start["description"]; ok {
		t.Error("description should be omitted when empty")
	}
	if _, ok := start["persistence"]; ok {
		t.Error("persistence should be omitted when empty")
	}
	if _, ok := start["orchestration"]; ok {
		t.Error("orchestration should be omitted when empty")
	}
}

func TestPersistenceValues(t *testing.T) {
	tests := []struct {
		val  Persistence
		json string
	}{
		{PersistenceTransient, `"transient"`},
		{PersistencePersistent, `"persistent"`},
		{"", `""`},
	}
	for _, tt := range tests {
		data, _ := json.Marshal(tt.val)
		if string(data) != tt.json {
			t.Errorf("Marshal(%q) = %s, want %s", tt.val, data, tt.json)
		}
		var got Persistence
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", data, err)
		}
		if got != tt.val {
			t.Errorf("round-trip: got %q, want %q", got, tt.val)
		}
	}
}

func TestOrchestrationValues(t *testing.T) {
	tests := []struct {
		val  Orchestration
		json string
	}{
		{OrchestrationInternal, `"internal"`},
		{OrchestrationExternal, `"external"`},
		{OrchestrationHybrid, `"hybrid"`},
		{"", `""`},
	}
	for _, tt := range tests {
		data, _ := json.Marshal(tt.val)
		if string(data) != tt.json {
			t.Errorf("Marshal(%q) = %s, want %s", tt.val, data, tt.json)
		}
		var got Orchestration
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", data, err)
		}
		if got != tt.val {
			t.Errorf("round-trip: got %q, want %q", got, tt.val)
		}
	}
}

func TestStateTransitionRoundTrip(t *testing.T) {
	ts := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	tr := StateTransition{
		From:      "intake",
		To:        "solving",
		Event:     "IssueUnderstood",
		Timestamp: ts,
	}

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got StateTransition
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.From != tr.From || got.To != tr.To || got.Event != tr.Event {
		t.Errorf("got %+v, want %+v", got, tr)
	}
	if !got.Timestamp.Equal(tr.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, tr.Timestamp)
	}
}

func TestContextRoundTrip(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := &Context{
		CurrentState: "solving",
		History: []StateTransition{
			{From: "intake", To: "solving", Event: "IssueUnderstood", Timestamp: now},
		},
		Metadata:  map[string]any{"user_id": "u123"},
		StartedAt: now.Add(-time.Hour),
		UpdatedAt: now,
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Context
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.CurrentState != ctx.CurrentState {
		t.Errorf("CurrentState = %q, want %q", got.CurrentState, ctx.CurrentState)
	}
	if len(got.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(got.History))
	}
	if got.History[0].Event != "IssueUnderstood" {
		t.Errorf("History[0].Event = %q, want %q", got.History[0].Event, "IssueUnderstood")
	}
	if got.Metadata["user_id"] != "u123" {
		t.Errorf("Metadata[user_id] = %v, want %q", got.Metadata["user_id"], "u123")
	}
	if !got.StartedAt.Equal(ctx.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, ctx.StartedAt)
	}
}

func TestStateSkillsRoundTrip(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*State{
			"start": {
				PromptTask: "task1",
				Skills:     "skills/support",
			},
			"noSkills": {
				PromptTask: "task2",
				Skills:     "none",
			},
			"empty": {
				PromptTask: "task3",
			},
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Spec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.States["start"].Skills != "skills/support" {
		t.Errorf("start.Skills = %q, want %q", got.States["start"].Skills, "skills/support")
	}
	if got.States["noSkills"].Skills != "none" {
		t.Errorf("noSkills.Skills = %q, want %q", got.States["noSkills"].Skills, "none")
	}
	if got.States["empty"].Skills != "" {
		t.Errorf("empty.Skills = %q, want empty", got.States["empty"].Skills)
	}

	// Verify omitempty works
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	var states map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw["states"], &states); err != nil {
		t.Fatalf("unmarshal states: %v", err)
	}
	if _, ok := states["empty"]["skills"]; ok {
		t.Error("skills field should be omitted when empty")
	}
	if _, ok := states["start"]["skills"]; !ok {
		t.Error("skills field should be present when set")
	}
}

func TestContextMetadataOmitempty(t *testing.T) {
	ctx := &Context{
		CurrentState: "start",
		History:      []StateTransition{},
		StartedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["metadata"]; ok {
		t.Error("metadata field should be omitted when nil")
	}
}
