package workflow

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// TransitionAuthorizer decides whether a workflow transition may happen.
//
// PromptKit calls it before every transition it would make, whether the model
// requested it through workflow__transition or the host fired the event itself.
// The host implements its own policy, typically over the two states'
// declarations and their extensions (RFC 0016). A non-nil error refuses the
// transition and its text is the reason; PromptKit ships no policy and, with
// no authorizer configured, allows every transition.
//
// A host-fired transition is authorized while the workflow conversation holds
// its lock, so an authorizer must not call back into the conversation (its
// CurrentState, Context, Transition, ...): that deadlocks. Everything it needs
// about the transition is in the TransitionRequest.
type TransitionAuthorizer interface {
	AuthorizeTransition(ctx context.Context, req TransitionRequest) error
}

// TransitionRequest describes a transition about to happen.
type TransitionRequest struct {
	// From and To are state names. To is the state the current state's
	// on_event names for Event. A max_visits redirect at commit time is the
	// state machine's own safety limit and is not re-authorized.
	From, To string
	// FromState and ToState are copies of the two states' declarations, so
	// an authorizer cannot change the loaded workflow through them.
	FromState, ToState *packspec.WorkflowState
	// Event is the event that fires the transition.
	Event string
}

// CheckTransition asks a whether event may fire from sm's current state.
//
// It returns nil when a is nil, and when event does not resolve from the
// current state: such a transition fails in ProcessEvent on its own, so there
// is nothing for a host to authorize.
func CheckTransition(
	ctx context.Context, a TransitionAuthorizer, sm *StateMachine, spec *Spec, event string,
) error {
	if a == nil || sm == nil || spec == nil {
		return nil
	}
	from := sm.CurrentState()
	fromState := spec.States[from]
	if fromState == nil {
		return nil
	}
	to, ok := fromState.OnEvent[event]
	if !ok {
		return nil
	}
	return a.AuthorizeTransition(ctx, TransitionRequest{
		From:      from,
		To:        to,
		FromState: packspec.Clone(fromState),
		ToState:   packspec.Clone(spec.States[to]),
		Event:     event,
	})
}
