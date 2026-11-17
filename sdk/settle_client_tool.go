package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// HasToolResultRewriters reports whether the conversation has a tool hook
// that rewrites tool results (hooks.ToolResultRewriter, see WithToolHook).
func (c *Conversation) HasToolResultRewriters() bool {
	return c.hookRegistry.HasToolResultRewriters()
}

// SettleClientTool answers the deferred client tool call callID, as
// SendToolResult does (SendToolResultMultimodal for a non-empty
// []types.ContentPart result, FailClientTool when failure is not nil, with
// result as the partial output), and prepares now, on ctx, the tool result
// Resume will give the model for it: checked against the tool's output schema
// when it sets validate_output, admitted under ToolPolicy.MaxCallsPerMinute,
// and passed through the ToolResultRewriter hooks. It returns that result.
// Resume (or Continue) gives it to the model as it is, without checking,
// admitting or rewriting it again.
//
// Use it to report an answer before the turn resumes with the content the
// model will see, as the AG-UI adapter does for an answer its
// ToolResultProvider gives while other calls are still pending.
//
// A call that already has an answer, recorded with this method or any other,
// is not answered again: answered is false, the result is the zero value, and
// nothing is checked, admitted or rewritten. A call the conversation is not
// waiting on is refused with ErrToolCallNotAwaited, as SendToolResult
// refuses it.
func (c *Conversation) SettleClientTool(
	ctx context.Context, callID string, result any, failure error,
) (prepared types.MessageToolResult, answered bool, err error) {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return types.MessageToolResult{}, false, ErrConversationClosed
	}
	if awaitErr := c.checkAwaited(ctx, callID); awaitErr != nil {
		return types.MessageToolResult{}, false, awaitErr
	}
	res, err := clientResolution(callID, result, failure)
	if err != nil {
		return types.MessageToolResult{}, false, err
	}
	history, err := c.settlementHistory(ctx)
	if err != nil {
		return types.MessageToolResult{}, false, err
	}

	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	if c.resolvedStore.Has(callID) || answeredInHistory(history, callID) {
		return types.MessageToolResult{}, false, nil
	}
	calls := callsIn(history)
	names := callNamesOf(calls)
	batch := []*sdktools.ToolResolution{res}
	c.validateDeferredResults(batch, names)
	c.admitDeferredResults(batch, names)
	prepared = c.resumeResult(ctx, calls, res)

	c.settledMu.Lock()
	if c.prepared == nil {
		c.prepared = make(map[*sdktools.ToolResolution]types.MessageToolResult)
	}
	c.prepared[res] = prepared
	c.settledMu.Unlock()
	c.resolvedStore.Add(res)
	return prepared, true, nil
}

// clientResolution is the resolution SendToolResult, SendToolResultMultimodal
// or FailClientTool records for the answer to callID.
func clientResolution(callID string, result any, failure error) (*sdktools.ToolResolution, error) {
	res := &sdktools.ToolResolution{ID: callID, Error: failure}
	if parts, ok := result.([]types.ContentPart); ok && failure == nil {
		if len(parts) > 0 {
			res.Parts = parts
			return res, nil
		}
		result = nil // every part was dropped: nothing to send as parts
	}
	if failure != nil && result == nil {
		return res, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf(errSerializeClientToolResult, err)
	}
	res.ResultJSON = data
	return res, nil
}

// answeredInHistory reports whether history already answers callID after its
// last assistant message.
func answeredInHistory(history []types.Message, callID string) bool {
	for _, id := range answeredSinceLastAssistant(history) {
		if id == callID {
			return true
		}
	}
	return false
}

// isPrepared reports whether SettleClientTool prepared res's result, and
// admitted it.
func (c *Conversation) isPrepared(res *sdktools.ToolResolution) bool {
	_, ok := c.preparedResult(res)
	return ok
}

// preparedResult is the result SettleClientTool prepared for res, if any.
func (c *Conversation) preparedResult(res *sdktools.ToolResolution) (types.MessageToolResult, bool) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	result, ok := c.prepared[res]
	return result, ok
}
