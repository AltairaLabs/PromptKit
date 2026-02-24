package assertions

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleTool         = "tool"
	roleAssistant    = "assistant"
	sourceStatestore = "statestore"
)

// TurnToolCall represents a single tool call within a turn, paired with its result.
// This provides the ordered, result-paired trace needed for turn-level tool assertions.
type TurnToolCall struct {
	CallID     string                 // from MessageToolCall.ID
	Name       string                 // tool name
	Args       map[string]interface{} // parsed arguments
	RawArgs    json.RawMessage        // original JSON arguments
	Result     string                 // from MessageToolResult.Content
	Error      string                 // from MessageToolResult.Error
	LatencyMs  int64                  // from MessageToolResult.LatencyMs
	RoundIndex int                    // which tool-use round within the turn (0-based)
	resolved   bool                   // whether a result was matched
}

// resolveTurnToolTrace extracts an ordered, result-paired tool call trace from
// _turn_messages params. Returns the trace and a bool indicating whether tool
// trace data was available (false means duplex path — data not available).
func resolveTurnToolTrace(params map[string]interface{}) ([]TurnToolCall, bool) {
	messages, ok := params["_turn_messages"].([]types.Message)
	if !ok {
		return nil, false
	}

	trace, roundIndex := extractTraceFromMessages(messages)
	_ = roundIndex // final value unused but needed during extraction
	return trace, true
}

// extractTraceFromMessages walks messages and builds an ordered tool call trace.
func extractTraceFromMessages(messages []types.Message) (trace []TurnToolCall, lastRound int) {
	roundIndex := 0
	prevWasToolRole := false

	for idx := range messages {
		msg := &messages[idx]
		if msg.Source == sourceStatestore {
			continue
		}

		if msg.Role == roleAssistant && len(msg.ToolCalls) > 0 {
			if prevWasToolRole {
				roundIndex++
			}
			trace = appendToolCalls(trace, msg.ToolCalls, roundIndex)
			prevWasToolRole = false
		} else if msg.Role == roleTool && msg.ToolResult != nil {
			matchToolResultToTrace(trace, msg.ToolResult)
			prevWasToolRole = true
		} else {
			prevWasToolRole = false
		}
	}

	lastRound = roundIndex
	return trace, lastRound
}

// appendToolCalls adds tool calls from an assistant message to the trace.
func appendToolCalls(trace []TurnToolCall, calls []types.MessageToolCall, roundIndex int) []TurnToolCall {
	for idx := range calls {
		tc := &calls[idx]
		var args map[string]interface{}
		if len(tc.Args) > 0 {
			_ = json.Unmarshal(tc.Args, &args)
		}
		trace = append(trace, TurnToolCall{
			CallID:     tc.ID,
			Name:       tc.Name,
			Args:       args,
			RawArgs:    tc.Args,
			RoundIndex: roundIndex,
		})
	}
	return trace
}

// matchToolResultToTrace matches a tool result to the first unresolved trace entry,
// using ID-first then name-fallback matching (same as context_builder.go).
func matchToolResultToTrace(trace []TurnToolCall, result *types.MessageToolResult) {
	// Try matching by ID first
	if result.ID != "" {
		for i := range trace {
			if trace[i].CallID == result.ID && !trace[i].resolved {
				populateTraceResult(&trace[i], result)
				return
			}
		}
	}

	// Fall back to forward name matching (first unresolved record)
	for i := range trace {
		if trace[i].Name == result.Name && !trace[i].resolved {
			populateTraceResult(&trace[i], result)
			return
		}
	}
}

// populateTraceResult fills a TurnToolCall with data from a MessageToolResult.
func populateTraceResult(tc *TurnToolCall, result *types.MessageToolResult) {
	tc.Result = result.Content
	tc.Error = result.Error
	tc.LatencyMs = result.LatencyMs
	tc.resolved = true
}
