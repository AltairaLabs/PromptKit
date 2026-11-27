package evals

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// TurnScoped is an optional interface for an EvalTypeHandler whose eval judges
// a single turn rather than the session, such as tools_called or
// tools_not_called.
//
// A turn-triggered eval (every_turn, sample_turns) is handed the history up
// to the turn it judges, so EvalContext.ToolCalls holds every earlier turn's
// calls too. When the handler reports ScopedToTurn, the eval runner and the
// guardrail adapter narrow ToolCalls to the current turn's calls before
// calling it; see ScopeToTurn. A handler that does not implement TurnScoped
// sees the whole history, which is what a session-level check wants.
type TurnScoped interface {
	// ScopedToTurn reports whether the eval, given params, judges one turn.
	// A wrapper decides from the eval it wraps, named in params.
	ScopedToTurn(params map[string]any) bool
}

// ScopeToTurn returns the context handler should judge: evalCtx itself, or,
// when handler is TurnScoped and reports ScopedToTurn for params, a shallow
// copy whose ToolCalls holds only the current turn's calls (CurrentTurnToolCalls).
// evalCtx is never modified.
func ScopeToTurn(handler EvalTypeHandler, params map[string]any, evalCtx *EvalContext) *EvalContext {
	ts, ok := handler.(TurnScoped)
	if !ok || !ts.ScopedToTurn(params) {
		return evalCtx
	}
	return withCurrentTurnToolCalls(evalCtx)
}

// CurrentTurnToolCalls returns the calls in evalCtx.ToolCalls made in the
// current turn: those whose TurnIndex, the index of the assistant message
// that made the call (see ExtractToolCalls), is at or after the last user
// message in evalCtx.Messages. With no user message in Messages the turn
// boundary cannot be seen, and every call is returned.
func CurrentTurnToolCalls(evalCtx *EvalContext) []ToolCallRecord {
	// With no user message start is -1, which every call is at or after.
	start := lastUserIndex(evalCtx.Messages)
	var calls []ToolCallRecord
	for i := range evalCtx.ToolCalls {
		if evalCtx.ToolCalls[i].TurnIndex >= start {
			calls = append(calls, evalCtx.ToolCalls[i])
		}
	}
	return calls
}

// withCurrentTurnToolCalls is a shallow copy of evalCtx with ToolCalls
// narrowed to the current turn.
func withCurrentTurnToolCalls(evalCtx *EvalContext) *EvalContext {
	scoped := *evalCtx
	scoped.ToolCalls = CurrentTurnToolCalls(evalCtx)
	return &scoped
}

// lastUserIndex is the index of the last user message, or -1 when there is
// none.
func lastUserIndex(messages []types.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == roleUser {
			return i
		}
	}
	return -1
}

// ScopedToTurn reports whether the wrapped eval judges one turn.
func (h *AssertionEvalHandler) ScopedToTurn(params map[string]any) bool {
	return innerScopedToTurn(h.registry, params)
}

// ScopedToTurn reports whether the wrapped eval judges one turn.
func (h *GuardrailEvalHandler) ScopedToTurn(params map[string]any) bool {
	return innerScopedToTurn(h.registry, params)
}

// innerScopedToTurn reports whether the eval a wrapper delegates to is
// TurnScoped for its own params.
func innerScopedToTurn(registry *EvalTypeRegistry, params map[string]any) bool {
	evalType, _ := innerEvalType(params)
	handler, err := registry.Get(evalType)
	if err != nil {
		return false
	}
	ts, ok := handler.(TurnScoped)
	return ok && ts.ScopedToTurn(extractEvalParams(params))
}
