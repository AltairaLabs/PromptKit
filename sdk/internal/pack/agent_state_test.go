package pack

import (
	"strings"
	"testing"
)

func stateBackedSDKPack(wf *WorkflowSpec, state string) *Pack {
	return &Pack{
		Prompts:  map[string]*Prompt{"triage": {}, "analyst": {}},
		Workflow: wf,
		Agents: &AgentsConfig{Entry: "analyst", Members: map[string]*AgentDef{
			"analyst": {},
			"triage":  {State: state},
		}},
	}
}

func triageSDKWorkflow() *WorkflowSpec {
	return &WorkflowSpec{
		Version: 1, Entry: "triage",
		States: map[string]*WorkflowState{"triage": {PromptTask: "triage"}},
	}
}

// The SDK validator must accept workflow version 2 (RFC 0009), matching the
// runtime validator. Surfaced because RFC 0011 loads workflows via the SDK.
func TestSDKValidateWorkflow_AcceptsVersion2(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"a": {}},
		Workflow: &WorkflowSpec{
			Version: 2, Entry: "s",
			States: map[string]*WorkflowState{"s": {PromptTask: "a"}},
		},
	}
	if err := validateWorkflowSection(p); err != nil {
		t.Fatalf("workflow version 2 should validate, got %v", err)
	}
}

// RFC 0011: a state-backed agent whose state resolves is valid.
func TestSDKValidateAgents_StateResolves(t *testing.T) {
	if err := validateAgentsSection(stateBackedSDKPack(triageSDKWorkflow(), "triage")); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// RFC 0011 rule 2: state without a top-level workflow is an error.
func TestSDKValidateAgents_StateRequiresWorkflow(t *testing.T) {
	err := validateAgentsSection(stateBackedSDKPack(nil, "triage"))
	if err == nil || !strings.Contains(err.Error(), "requires a top-level workflow") {
		t.Fatalf("expected workflow-required error, got %v", err)
	}
}

// RFC 0011 rule 1: state must reference a key in workflow.states.
func TestSDKValidateAgents_StateMustResolve(t *testing.T) {
	err := validateAgentsSection(stateBackedSDKPack(triageSDKWorkflow(), "ghost"))
	if err == nil || !strings.Contains(err.Error(), "does not reference a valid workflow state") {
		t.Fatalf("expected unresolved-state error, got %v", err)
	}
}
