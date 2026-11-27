package handlers

// The handlers below judge one turn, so they implement evals.TurnScoped: on a
// turn-triggered eval they see only the tool calls of the turn being judged,
// not those of every earlier turn in the history (#2927). Their session-level
// counterparts (tools_called_session, tool_call_sequence, ...) do not, and see
// the whole history.

// ScopedToTurn reports that tools_called judges one turn.
func (h *ToolsCalledHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tools_not_called judges one turn.
func (h *ToolsNotCalledHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tools_offered judges one turn.
func (h *ToolsOfferedHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_args judges one turn.
func (h *ToolArgsHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_call_count judges one turn.
func (h *ToolCallCountHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_calls_with_args judges one turn.
func (h *ToolCallsWithArgsHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_result_includes judges one turn.
func (h *ToolResultIncludesHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_result_matches judges one turn.
func (h *ToolResultMatchesHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_result_has_media judges one turn.
func (h *ToolResultHasMediaHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that tool_result_media_type judges one turn.
func (h *ToolResultMediaTypeHandler) ScopedToTurn(map[string]any) bool { return true }

// ScopedToTurn reports that no_tool_errors judges one turn.
func (h *NoToolErrorsHandler) ScopedToTurn(map[string]any) bool { return true }
