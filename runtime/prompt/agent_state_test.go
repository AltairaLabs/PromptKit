package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

func stateBackedPack(workflowSpec *workflow.Spec, state string) *Pack {
	return &Pack{
		Prompts:  map[string]*PackPrompt{"triage": {}, "analyst": {}},
		Workflow: workflowSpec,
		Agents: &AgentsConfig{
			Entry: "analyst",
			Members: map[string]*AgentDef{
				"analyst": {},
				"triage":  {State: state},
			},
		},
	}
}

func triageWorkflow() *workflow.Spec {
	return &workflow.Spec{
		Version: 1, Entry: "triage",
		States: map[string]*workflow.State{"triage": {PromptTask: "triage"}},
	}
}

func agentErrsContain(errs []string, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

// RFC 0011: a state-backed agent whose state resolves is valid.
func TestValidateAgents_StateResolves(t *testing.T) {
	p := stateBackedPack(triageWorkflow(), "triage")
	errs, _ := p.ValidateAgents()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

// RFC 0011 rule 2: state without a top-level workflow is an error.
func TestValidateAgents_StateRequiresWorkflow(t *testing.T) {
	p := stateBackedPack(nil, "triage")
	errs, _ := p.ValidateAgents()
	if !agentErrsContain(errs, "requires a top-level workflow") {
		t.Fatalf("expected workflow-required error, got %v", errs)
	}
}

// RFC 0011 rule 1: state must reference a key in workflow.states.
func TestValidateAgents_StateMustResolve(t *testing.T) {
	p := stateBackedPack(triageWorkflow(), "ghost")
	errs, _ := p.ValidateAgents()
	if !agentErrsContain(errs, "does not reference a valid workflow state") {
		t.Fatalf("expected unresolved-state error, got %v", errs)
	}
}

// RFC 0011: AgentDef gains an optional `state` field.
func TestAgentDef_StateField(t *testing.T) {
	var def AgentDef
	if err := json.Unmarshal([]byte(`{"state":"triage","tags":["t"]}`), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.State != "triage" {
		t.Fatalf("State = %q, want triage", def.State)
	}
}
