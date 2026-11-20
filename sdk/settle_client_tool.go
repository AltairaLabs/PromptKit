package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/clienttool"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// preparedAnswer is an answer settleClientTool prepared: the tool result it
// gives the model, rewritten, and release, which takes back the admissions
// the answer took when it is discarded unused (nil when it took none).
type preparedAnswer struct {
	result  types.MessageToolResult
	release func()
}

// The AG-UI adapter settles answers through clienttool.SettlerOf, which keeps
// these methods off the Conversation's public API.
//
//nolint:gochecknoinits // registration is how the internal package reaches the SDK's settler.
func init() {
	clienttool.Register(func(conv any) clienttool.Settler {
		if c, ok := conv.(*Conversation); ok && c != nil {
			return clientToolSettler{c}
		}
		return nil
	})
}

// clientToolSettler is a conversation's clienttool.Settler.
type clientToolSettler struct{ c *Conversation }

// HasToolResultRewriters is the conversation's hasToolResultRewriters.
func (s clientToolSettler) HasToolResultRewriters() bool { return s.c.hasToolResultRewriters() }

// SettleClientTool is the conversation's settleClientTool.
func (s clientToolSettler) SettleClientTool(
	ctx context.Context, callID string, result any, failure error,
) (types.MessageToolResult, bool, error) {
	return s.c.settleClientTool(ctx, callID, result, failure)
}

// SettleClientToolRejection is the conversation's settleClientToolRejection.
func (s clientToolSettler) SettleClientToolRejection(
	ctx context.Context, callID, reason string,
) (types.MessageToolResult, bool, error) {
	return s.c.settleClientToolRejection(ctx, callID, reason)
}

// hasToolResultRewriters reports whether the conversation has a tool hook
// that rewrites tool results (hooks.ToolResultRewriter, see WithToolHook).
func (c *Conversation) hasToolResultRewriters() bool {
	return c.hookRegistry.HasToolResultRewriters()
}

// settleClientTool answers the deferred client tool call callID, as
// SendToolResult does (SendToolResultMultimodal for a non-empty
// []types.ContentPart result, FailClientTool when failure is not nil, with
// result as the partial output), and prepares now, on ctx, the tool result
// Resume will give the model for it: checked against the tool's output schema
// when it sets validate_output, admitted under ToolPolicy.MaxCallsPerMinute
// and the tool registry's rate limit, and passed through the
// ToolResultRewriter hooks, then capped at the registry's result size limit.
// It returns that result, which Resume and Continue give the model as it is. If the turn is abandoned instead (Send
// starts a new one), the answer is discarded and its admissions taken back.
//
// The AG-UI adapter uses it to report an answer before the turn resumes with
// the content the model will see, for an answer its ToolResultProvider gives
// while other calls are still pending.
//
// It runs as a turn of the conversation, as Resume does: it waits for a turn
// in progress, and no other answer can be recorded for the call while it
// prepares this one. A call that already has an answer, recorded with this
// method or any other, is not answered again: answered is false, the result
// is the zero value, and nothing is checked, admitted or rewritten. A call
// the conversation is not waiting on is refused with ErrToolCallNotAwaited,
// as SendToolResult refuses it.
func (c *Conversation) settleClientTool(
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

// settleClientToolRejection is settleClientTool for a rejection, as
// RejectClientTool records it: the result it returns is the rejection the
// model is given, in the words of the tool's decline strategy.
func (c *Conversation) settleClientToolRejection(
	ctx context.Context, callID, reason string,
) (prepared types.MessageToolResult, answered bool, err error) {
	return c.settle(ctx, &sdktools.ToolResolution{ID: callID, Rejected: true, RejectionReason: reason})
}

// settle records res and prepares the result Resume gives the model for it
// (settleClientTool).
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

	// Claim the call under settleMu, then prepare its result without the
	// lock: rewriter hooks may record other answers, which take it.
	if !c.claimSettling(res.ID, history) {
		return types.MessageToolResult{}, false, nil
	}
	// Every exit frees the claim, a panicking hook's included; one that did
	// not record the answer also gives back the admissions it took.
	var admission func()
	recorded := false
	defer func() {
		c.releaseSettling(res.ID)
		if !recorded && admission != nil {
			admission()
		}
	}()
	calls := callsIn(history)
	names := callNamesOf(calls)
	c.validateDeferredResults([]*sdktools.ToolResolution{res}, names)
	admission = c.admitPrepared(res, names()[res.ID])
	prepared := c.cappedPrepared(c.resumeResult(ctx, calls, res))

	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	c.settledMu.Lock()
	if c.prepared == nil {
		c.prepared = make(map[*sdktools.ToolResolution]preparedAnswer)
	}
	c.prepared[res] = preparedAnswer{result: prepared, release: admission}
	c.settledMu.Unlock()
	c.resolvedStore.Add(res)
	recorded = true
	return prepared, true, nil
}

// releaseSettling frees the claim claimSettling took on call id.
func (c *Conversation) releaseSettling(id string) {
	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	delete(c.settling, id)
}

// claimSettling claims call id for a settle, under settleMu: it fails when
// the call already has an answer (recorded, in history, or being settled).
// While claimed, another answer to the call is a second answer
// (addResolution drops it).
func (c *Conversation) claimSettling(id string, history []types.Message) bool {
	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	if c.settling[id] || c.resolvedStore.Has(id) || answeredInHistory(history, id) {
		return false
	}
	if c.settling == nil {
		c.settling = make(map[string]bool)
	}
	c.settling[id] = true
	return true
}

// cappedPrepared is prepared with each text capped at the registry's result
// size limit, so the text the client is shown is final: Continue and Resume
// give the model the prepared result as it is. A failure is left as it is.
func (c *Conversation) cappedPrepared(prepared types.MessageToolResult) types.MessageToolResult {
	if prepared.Error != "" || c.toolRegistry == nil || c.toolRegistry.MaxToolResultSize() <= 0 {
		return prepared
	}
	limit := c.toolRegistry.MaxToolResultSize()
	parts := make([]types.ContentPart, len(prepared.Parts))
	for i, p := range prepared.Parts {
		if p.Type == types.ContentTypeText && p.Text != nil {
			text := tools.TruncateResultText(prepared.Name, *p.Text, limit)
			p.Text = &text
		}
		parts[i] = p
	}
	prepared.Parts = parts
	return prepared
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

// isPrepared reports whether settleClientTool prepared res's result: it was
// checked and admitted then.
func (c *Conversation) isPrepared(res *sdktools.ToolResolution) bool {
	_, ok := c.preparedResult(res)
	return ok
}

// preparedResult is the result settleClientTool prepared for res, if any.
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
