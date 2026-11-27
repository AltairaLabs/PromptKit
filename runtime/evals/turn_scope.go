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
	return scopeToTurnFrom(handler, params, evalCtx, lastUserIndex(evalCtx.Messages))
}

// CurrentTurnToolCalls returns the calls in evalCtx.ToolCalls made in the
// current turn: those whose TurnIndex, the index of the assistant message
// that made the call (see ExtractToolCalls), is at or after the last user
// message in evalCtx.Messages. With no user message in Messages the turn
// boundary cannot be seen, and every call is returned.
func CurrentTurnToolCalls(evalCtx *EvalContext) []ToolCallRecord {
	return toolCallsFrom(evalCtx.ToolCalls, lastUserIndex(evalCtx.Messages))
}

// scopeToTurnFrom is ScopeToTurn with the turn starting at message index start.
func scopeToTurnFrom(
	handler EvalTypeHandler, params map[string]any, evalCtx *EvalContext, start int,
) *EvalContext {
	ts, ok := handler.(TurnScoped)
	if !ok || !ts.ScopedToTurn(params) {
		return evalCtx
	}
	scoped := *evalCtx
	scoped.ToolCalls = toolCallsFrom(evalCtx.ToolCalls, start)
	return &scoped
}

// toolCallsFrom returns the calls made by a message at or after index start.
// A negative start keeps every call.
func toolCallsFrom(calls []ToolCallRecord, start int) []ToolCallRecord {
	var from []ToolCallRecord
	for i := range calls {
		if calls[i].TurnIndex >= start {
			from = append(from, calls[i])
		}
	}
	return from
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

// repliedTurnStart is where the turn a turn eval judges begins in messages.
//
// A turn eval runs once the turn's reply is stored. Usually the turn begins at
// its user message, the last one. A provider that delivers the caller's
// transcript after the reply (OpenAI Realtime, without transcript reordering)
// stores it after that reply instead, and the turn is reported once it
// arrives, so user messages after the last assistant message are the judged
// turn's own. The turn then begins after the previous turn's reply: the last
// assistant message without tool calls before this turn's reply, or the
// start of the history when there is none.
func repliedTurnStart(messages []types.Message) int {
	reply := len(messages) - 1
	for reply >= 0 && messages[reply].Role == roleUser {
		reply--
	}
	if reply == len(messages)-1 || reply < 0 || messages[reply].Role != roleAssistant {
		return lastUserIndex(messages)
	}
	for i := reply - 1; i >= 0; i-- {
		if messages[i].Role == roleAssistant && len(messages[i].ToolCalls) == 0 {
			return i + 1
		}
	}
	return 0
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
// TurnScoped for its own params, after the defaults and aliases the wrapper
// applies before calling it (prepareInner).
func innerScopedToTurn(registry *EvalTypeRegistry, params map[string]any) bool {
	evalType, _ := innerEvalType(params)
	handler, err := registry.Get(evalType)
	if err != nil {
		return false
	}
	ts, ok := handler.(TurnScoped)
	return ok && ts.ScopedToTurn(NormalizeParams(evalType, ApplyDefaults(evalType, extractEvalParams(params))))
}
