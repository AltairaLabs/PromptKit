package assertions

import (
	"context"
	"testing"
)

// --- state_is ---

func TestWorkflowStateIs_Pass(t *testing.T) {
	t.Parallel()
	v := NewWorkflowStateIsValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_current_state": "processing",
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, map[string]interface{}{"state": "processing"})
	if !r.Passed {
		t.Fatalf("expected pass, got: %s", r.Message)
	}
}

func TestWorkflowStateIs_Fail(t *testing.T) {
	t.Parallel()
	v := NewWorkflowStateIsValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_current_state": "intake",
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, map[string]interface{}{"state": "processing"})
	if r.Passed {
		t.Fatal("expected fail")
	}
	if r.Details["expected"] != "processing" || r.Details["actual"] != "intake" {
		t.Fatalf("unexpected details: %v", r.Details)
	}
}

func TestWorkflowStateIs_MissingParam(t *testing.T) {
	t.Parallel()
	v := NewWorkflowStateIsValidator()
	r := v.ValidateConversation(context.Background(), &ConversationContext{}, map[string]interface{}{})
	if r.Passed {
		t.Fatal("expected fail for missing param")
	}
}

func TestWorkflowStateIs_NoWorkflowState(t *testing.T) {
	t.Parallel()
	v := NewWorkflowStateIsValidator()
	r := v.ValidateConversation(context.Background(), &ConversationContext{}, map[string]interface{}{"state": "foo"})
	if r.Passed {
		t.Fatal("expected fail for no workflow state")
	}
}

func TestWorkflowStateIs_Type(t *testing.T) {
	t.Parallel()
	v := NewWorkflowStateIsValidator()
	if v.Type() != "state_is" {
		t.Fatalf("unexpected type: %s", v.Type())
	}
}

// --- transitioned_to ---

func TestWorkflowTransitionedTo_Pass(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_transitions": []interface{}{
					map[string]interface{}{"from": "intake", "to": "processing", "event": "Next"},
					map[string]interface{}{"from": "processing", "to": "done", "event": "Finish"},
				},
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, map[string]interface{}{"state": "processing"})
	if !r.Passed {
		t.Fatalf("expected pass, got: %s", r.Message)
	}
}

func TestWorkflowTransitionedTo_Fail(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_transitions": []interface{}{
					map[string]interface{}{"from": "intake", "to": "processing", "event": "Next"},
				},
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, map[string]interface{}{"state": "done"})
	if r.Passed {
		t.Fatal("expected fail")
	}
}

func TestWorkflowTransitionedTo_MissingParam(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	r := v.ValidateConversation(context.Background(), &ConversationContext{}, map[string]interface{}{})
	if r.Passed {
		t.Fatal("expected fail for missing param")
	}
}

func TestWorkflowTransitionedTo_NoTransitions(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	r := v.ValidateConversation(context.Background(), &ConversationContext{}, map[string]interface{}{"state": "foo"})
	if r.Passed {
		t.Fatal("expected fail for no transitions")
	}
}

func TestWorkflowTransitionedTo_InvalidTransitionsData(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_transitions": "not-a-slice",
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, map[string]interface{}{"state": "foo"})
	if r.Passed {
		t.Fatal("expected fail for invalid transitions data")
	}
}

func TestWorkflowTransitionedTo_Type(t *testing.T) {
	t.Parallel()
	v := NewWorkflowTransitionedToValidator()
	if v.Type() != "transitioned_to" {
		t.Fatalf("unexpected type: %s", v.Type())
	}
}

// --- workflow_complete ---

func TestWorkflowComplete_Pass(t *testing.T) {
	t.Parallel()
	v := NewWorkflowCompleteValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_complete":      true,
				"workflow_current_state": "done",
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, nil)
	if !r.Passed {
		t.Fatalf("expected pass, got: %s", r.Message)
	}
}

func TestWorkflowComplete_NotComplete(t *testing.T) {
	t.Parallel()
	v := NewWorkflowCompleteValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_complete": false,
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, nil)
	if r.Passed {
		t.Fatal("expected fail")
	}
}

func TestWorkflowComplete_NoStatus(t *testing.T) {
	t.Parallel()
	v := NewWorkflowCompleteValidator()
	r := v.ValidateConversation(context.Background(), &ConversationContext{}, nil)
	if r.Passed {
		t.Fatal("expected fail for no status")
	}
}

func TestWorkflowComplete_InvalidType(t *testing.T) {
	t.Parallel()
	v := NewWorkflowCompleteValidator()
	convCtx := &ConversationContext{
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"workflow_complete": "not-a-bool",
			},
		},
	}
	r := v.ValidateConversation(context.Background(), convCtx, nil)
	if r.Passed {
		t.Fatal("expected fail for invalid type")
	}
}

func TestWorkflowComplete_Type(t *testing.T) {
	t.Parallel()
	v := NewWorkflowCompleteValidator()
	if v.Type() != "workflow_complete" {
		t.Fatalf("unexpected type: %s", v.Type())
	}
}

// --- registry integration ---

func TestConversationRegistry_WorkflowAssertionsRegistered(t *testing.T) {
	t.Parallel()
	r := NewConversationAssertionRegistry()
	for _, name := range []string{"state_is", "transitioned_to", "workflow_complete"} {
		if !r.Has(name) {
			t.Errorf("expected %q to be registered", name)
		}
	}
}
