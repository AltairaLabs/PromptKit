package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// preparedAnswer is an answer SettleClientTool prepared: the tool result it
// gives the model, rewritten, and release, which takes back the admissions
// the answer took when it is discarded unused (nil when it took none).
type preparedAnswer struct {
	result  types.MessageToolResult
	release func()
}

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
// when it sets validate_output, admitted under ToolPolicy.MaxCallsPerMinute
// and the tool registry's rate limit, and passed through the
// ToolResultRewriter hooks. It returns that result. Resume gives it to the
// model as it is, and Continue formats and caps it as it does any result,
// neither of them rewriting it again. If the turn is abandoned instead (Send
// starts a new one), the answer is discarded and its admissions taken back.
//
// Use it to report an answer before the turn resumes with the content the
// model will see, as the AG-UI adapter does for an answer its
// ToolResultProvider gives while other calls are still pending.
//
// It runs as a turn of the conversation, as Resume does: it waits for a turn
// in progress, and no other answer can be recorded for the call while it
// prepares this one. A call that already has an answer, recorded with this
// method or any other, is not answered again: answered is false, the result
// is the zero value, and nothing is checked, admitted or rewritten. A call
// the conversation is not waiting on is refused with ErrToolCallNotAwaited,
// as SendToolResult refuses it.
func (c *Conversation) SettleClientTool(
	ctx context.Context, callID string, result any, failure error,
) (prepared types.MessageToolResult, answered bool, err error) {
	var parts []types.ContentPart
	if p, ok := result.([]types.ContentPart); ok && failure == nil {
		result, parts = nil, p
	}
	res, err := clientResolution(callID, result, parts, failure)
	if err != nil {
		return types.MessageToolResult{}, false, err
	}
	return c.settle(ctx, res)
}

// SettleClientToolRejection is SettleClientTool for a rejection, as
// RejectClientTool records it: the result it returns is the rejection the
// model is given, in the words of the tool's decline strategy.
func (c *Conversation) SettleClientToolRejection(
	ctx context.Context, callID, reason string,
) (prepared types.MessageToolResult, answered bool, err error) {
	return c.settle(ctx, &sdktools.ToolResolution{ID: callID, Rejected: true, RejectionReason: reason})
}

// settle records res and prepares the result Resume gives the model for it
// (SettleClientTool).
func (c *Conversation) settle(
	ctx context.Context, res *sdktools.ToolResolution,
) (types.MessageToolResult, bool, error) {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return types.MessageToolResult{}, false, ErrConversationClosed
	}
	release, gateErr := c.turn.acquire(ctx, c)
	if gateErr != nil {
		return types.MessageToolResult{}, false, gateErr
	}
	defer release()
	ctx = c.turn.mark(ctx, c)

	history, err := c.settlementHistory(ctx)
	if err != nil {
		return types.MessageToolResult{}, false, err
	}
	if awaitErr := c.checkAwaitedIn(history, res.ID); awaitErr != nil {
		return types.MessageToolResult{}, false, awaitErr
	}

	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	if c.resolvedStore.Has(res.ID) || answeredInHistory(history, res.ID) {
		return types.MessageToolResult{}, false, nil
	}
	calls := callsIn(history)
	names := callNamesOf(calls)
	c.validateDeferredResults([]*sdktools.ToolResolution{res}, names)
	admission := c.admitPrepared(res, names()[res.ID])
	prepared := c.resumeResult(ctx, calls, res)

	c.settledMu.Lock()
	if c.prepared == nil {
		c.prepared = make(map[*sdktools.ToolResolution]preparedAnswer)
	}
	c.prepared[res] = preparedAnswer{result: prepared, release: admission}
	c.settledMu.Unlock()
	c.resolvedStore.Add(res)
	return prepared, true, nil
}

// admitPrepared admits res, an answer to a call of tool name, as
// admitDeferredResults does, and returns the func that takes its admissions
// back (nil when it took none). Over a limit, the limit's error replaces the
// answer.
func (c *Conversation) admitPrepared(res *sdktools.ToolResolution, name string) func() {
	if res.Rejected || name == "" {
		return nil
	}
	release, err := c.admitSettledRelease(name, c.callRateLimit())
	if err != nil {
		res.Result, res.ResultJSON, res.Parts, res.Error = nil, nil, nil, err
		return nil
	}
	return release
}

// formattedPrepared is a result SettleClientTool prepared as Continue gives a
// result: its text formatted and capped at the registry's size limit
// (tools.BuildModelResult), not rewritten again. A failure is given as it is.
func (c *Conversation) formattedPrepared(prepared types.MessageToolResult) types.MessageToolResult {
	if prepared.Error != "" {
		return prepared
	}
	opts := tools.ModelResultOptions{ToolName: prepared.Name}
	if c.toolRegistry != nil {
		opts.MaxSize = c.toolRegistry.MaxToolResultSize()
	}
	var given tools.ModelResult
	var err error
	if prepared.HasMedia() {
		given, err = tools.BuildModelResult(nil, prepared.Parts, opts)
	} else {
		given, err = tools.BuildModelResult(json.RawMessage(prepared.GetTextContent()), nil, opts)
	}
	if err != nil { // only a rewrite fails, and there is none
		return prepared
	}
	if given.Parts != nil {
		return types.MessageToolResult{ID: prepared.ID, Name: prepared.Name, Parts: given.Parts}
	}
	return types.NewTextToolResult(prepared.ID, prepared.Name, given.Text)
}

// clientResolution is the resolution recorded for an answer to callID:
// failure, with result as the partial output, when failure is not nil;
// otherwise parts when there are any, or result encoded as JSON.
func clientResolution(
	callID string, result any, parts []types.ContentPart, failure error,
) (*sdktools.ToolResolution, error) {
	res := &sdktools.ToolResolution{ID: callID, Error: failure}
	if failure == nil && len(parts) > 0 {
		res.Parts = parts
		return res, nil
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

// isPrepared reports whether SettleClientTool prepared res's result: it was
// checked and admitted then.
func (c *Conversation) isPrepared(res *sdktools.ToolResolution) bool {
	_, ok := c.preparedResult(res)
	return ok
}

// preparedResult is the result SettleClientTool prepared for res, if any.
func (c *Conversation) preparedResult(res *sdktools.ToolResolution) (types.MessageToolResult, bool) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	p, ok := c.prepared[res]
	return p.result, ok
}

// runReleases takes back the admissions of answers discarded unused.
func runReleases(releases []func()) {
	for _, release := range releases {
		if release != nil {
			release()
		}
	}
}
