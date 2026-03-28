package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

func TestArtifactExecutor_SetArtifact(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {
				PromptTask: "t",
				Artifacts:  map[string]*ArtifactDef{"sha": {Type: "text/plain"}},
			},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewArtifactExecutor(sm)

	args, _ := json.Marshal(map[string]string{"name": "sha", "value": "abc123"})
	result, err := exec.Execute(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "artifact_set" {
		t.Errorf("status = %q, want artifact_set", resp["status"])
	}

	arts := sm.Artifacts()
	if arts["sha"] != "abc123" {
		t.Errorf("artifact sha = %q, want abc123", arts["sha"])
	}
}

func TestArtifactExecutor_AppendMode(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {
				PromptTask: "t",
				Artifacts:  map[string]*ArtifactDef{"log": {Type: "text/plain", Mode: "append"}},
			},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewArtifactExecutor(sm)

	args1, _ := json.Marshal(map[string]string{"name": "log", "value": "line1\n"})
	if _, err := exec.Execute(context.Background(), nil, args1); err != nil {
		t.Fatalf("Execute 1: %v", err)
	}

	args2, _ := json.Marshal(map[string]string{"name": "log", "value": "line2\n"})
	if _, err := exec.Execute(context.Background(), nil, args2); err != nil {
		t.Fatalf("Execute 2: %v", err)
	}

	arts := sm.Artifacts()
	if arts["log"] != "line1\nline2\n" {
		t.Errorf("artifact log = %q, want 'line1\\nline2\\n'", arts["log"])
	}
}

func TestArtifactExecutor_EmptyNameError(t *testing.T) {
	spec := &Spec{Version: 1, Entry: "a", States: map[string]*State{
		"a": {PromptTask: "t"},
	}}
	sm := NewStateMachine(spec)
	exec := NewArtifactExecutor(sm)

	args, _ := json.Marshal(map[string]string{"name": "", "value": "test"})
	_, err := exec.Execute(context.Background(), nil, args)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegisterArtifactTool_OnlyWhenArtifactsDeclared(t *testing.T) {
	// Spec with no artifacts — should not register
	specNoArt := &Spec{
		Version: 1,
		Entry:   "a",
		States:  map[string]*State{"a": {PromptTask: "t"}},
	}
	reg1 := newTestRegistry(t)
	RegisterArtifactTool(reg1, specNoArt)
	if reg1.Get(ArtifactToolName) != nil {
		t.Error("artifact tool should not be registered when no artifacts declared")
	}

	// Spec with artifacts — should register
	specWithArt := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {
				PromptTask: "t",
				Artifacts:  map[string]*ArtifactDef{"sha": {Type: "text/plain"}},
			},
		},
	}
	reg2 := newTestRegistry(t)
	RegisterArtifactTool(reg2, specWithArt)
	tool := reg2.Get(ArtifactToolName)
	if tool == nil {
		t.Fatal("artifact tool should be registered when artifacts declared")
	}
	if tool.Mode != ArtifactExecutorMode {
		t.Errorf("tool Mode = %q, want %q", tool.Mode, ArtifactExecutorMode)
	}
}

func TestBuildArtifactToolDescriptor_EnumConstraint(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", Artifacts: map[string]*ArtifactDef{
				"sha":    {Type: "text/plain"},
				"report": {Type: "application/json"},
			}},
			"b": {PromptTask: "t", Artifacts: map[string]*ArtifactDef{
				"sha": {Type: "text/plain"}, // duplicate — should be deduped
				"log": {Type: "text/plain"},
			}},
		},
	}
	desc := BuildArtifactToolDescriptor(spec)

	var schema struct {
		Properties struct {
			Name struct {
				Enum []string `json:"enum"`
			} `json:"name"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(desc.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	names := schema.Properties.Name.Enum
	if len(names) != 3 {
		t.Fatalf("expected 3 artifact names, got %d: %v", len(names), names)
	}
	// Should be sorted and deduped
	expected := []string{"log", "report", "sha"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("name[%d] = %q, want %q", i, name, expected[i])
		}
	}
}
