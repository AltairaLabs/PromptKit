package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// This file prepares the results a host settles outside the turn (an
// approved call, a client tool's answer) for the model, as the provider stage
// prepares the result of a call it runs: rewritten by the conversation's
// ToolResultRewriter hooks with the stage's request, and, where that fails,
// answered with the stage's error result and a tool.call.failed event.

// toolResultMessageSubmitter is a duplex session that records the given
// result messages in the history, in place of ones built from the responses.
type toolResultMessageSubmitter interface {
	SubmitToolResultMessages(
		ctx context.Context, responses []providers.ToolResponse, results []types.MessageToolResult,
	) error
}

// continuedResult is the tool result Continue and ContinueDuplex give the
// model for res: the rejection, the error, or the result as modelResult
// prepares it. The result names the tool it answers, as an in-turn result
// does: a provider can match a result to its call by name (Gemini's
// functionResponse carries only the name).
func (c *Conversation) continuedResult(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	call, known := c.callFor(calls, res)
	switch {
	case res.Rejected:
		result := types.NewTextToolResult(res.ID, call.Name, fmt.Sprintf("Tool call rejected: %s", res.RejectionReason))
		result.Error = "rejected by user"
		return result
	case res.Error != nil:
		errStr := toolResultErrorText(res.Error)
		result := types.NewTextToolResult(res.ID, call.Name, "Tool error: "+errStr)
		result.Error = errStr
		return result
	}
	rewrite, err := c.rewriterFor(ctx, call, known)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	given, err := c.modelResult(call, res, rewrite)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	if given.Parts != nil {
		return types.MessageToolResult{ID: res.ID, Name: call.Name, Parts: given.Parts}
	}
	return types.NewTextToolResult(res.ID, call.Name, given.Text)
}

// clientAnswer is the tool result Resume gives the model for a client's
// answer res: what the client sent, unformatted and not truncated (it is also
// the AG-UI TOOL_CALL_RESULT), passed through the conversation's
// ToolResultRewriter hooks.
func (c *Conversation) clientAnswer(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	call, known := c.callFor(calls, res)
	rewrite, err := c.rewriterFor(ctx, call, known)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	if len(res.Parts) > 0 {
		parts, partsErr := rewriteTextParts(res.Parts, rewrite)
		if partsErr != nil {
			return c.settledFailure(ctx, call, partsErr)
		}
		return types.MessageToolResult{ID: res.ID, Parts: parts}
	}
	text := string(res.ResultJSON)
	if rewrite != nil {
		if text, err = rewrite(text); err != nil {
			return c.settledFailure(ctx, call, err)
		}
	}
	return types.NewTextToolResult(res.ID, "", text)
}

// rewriteTextParts passes each text part of parts through rewrite, keeping
// media parts where they are. Nil rewrite returns parts as they are.
func rewriteTextParts(parts []types.ContentPart, rewrite func(string) (string, error)) ([]types.ContentPart, error) {
	if rewrite == nil {
		return parts, nil
	}
	out := make([]types.ContentPart, len(parts))
	for i, p := range parts {
		if p.Type != types.ContentTypeText || p.Text == nil {
			out[i] = p
			continue
		}
		text, err := rewrite(*p.Text)
		if err != nil {
			return nil, err
		}
		out[i] = types.NewTextPart(text)
	}
	return out, nil
}

// toolResponseOf is the response a realtime session is given for tr, with
// its parts when multimodal.
func toolResponseOf(tr types.MessageToolResult, multimodal bool) providers.ToolResponse {
	resp := providers.ToolResponse{ToolCallID: tr.ID, Result: tr.GetTextContent(), IsError: tr.Error != ""}
	if multimodal {
		resp.Parts = tr.Parts
	}
	return resp
}

// settledFailure is the stage's error result for call (tools.ErrorResult),
// reported with a tool.call.failed event as the stage reports a failed call.
func (c *Conversation) settledFailure(
	ctx context.Context, call types.MessageToolCall, err error,
) types.MessageToolResult {
	if c.config != nil && c.config.eventBus != nil {
		var labels map[string]string
		if c.toolRegistry != nil {
			if desc := c.toolRegistry.Get(call.Name); desc != nil {
				labels = desc.Labels
			}
		}
		events.NewEmitter(c.config.eventBus, "", c.ID(), c.ID()).WithUserID(c.config.userID).
			ToolCallEventCtx(ctx, events.EventToolCallFailed, &events.ToolCallEventData{
				ToolName: call.Name, CallID: call.ID, Error: err, Labels: labels,
			})
	}
	return tools.ErrorResult(call.ID, call.Name, err)
}

// modelResult is an approved or answered call's result as the model is given
// the result of a call the pipeline runs (tools.BuildModelResult): formatted,
// passed through rewrite, then capped at the registry's size limit, its parts
// kept. The error is rewrite's.
func (c *Conversation) modelResult(
	call types.MessageToolCall, res *sdktools.ToolResolution, rewrite func(string) (string, error),
) (tools.ModelResult, error) {
	raw := res.ResultJSON
	if raw == nil && len(res.Parts) == 0 {
		if marshaled, err := json.Marshal(res.Result); err == nil {
			raw = marshaled
		} else {
			raw = json.RawMessage(fmt.Sprintf("%v", res.Result))
		}
	}
	opts := tools.ModelResultOptions{ToolName: call.Name, Rewrite: rewrite}
	if c.toolRegistry != nil {
		opts.MaxSize = c.toolRegistry.MaxToolResultSize()
	}
	return tools.BuildModelResult(raw, res.Parts, opts)
}

// rewriterFor passes a result's text for call through the conversation's
// ToolResultRewriter hooks, with the request the provider stage gives them
// for a call it runs: the call's name, ID and arguments, the caller and the
// tool's declaration, the call ID on the context. Nil when no hook rewrites.
// When the call is not known (callFor), its request cannot be given, so the
// result is refused with an error rather than delivered unrewritten.
func (c *Conversation) rewriterFor(
	ctx context.Context, call types.MessageToolCall, known bool,
) (func(string) (string, error), error) {
	if !c.hookRegistry.HasToolResultRewriters() {
		return nil, nil
	}
	if !known {
		return nil, fmt.Errorf(
			"the result of call %s was not delivered: no call it answers is known to rewrite it for", call.ID)
	}
	req := hooks.ToolRequest{Name: call.Name, Args: call.Args, CallID: call.ID, Caller: c.callerIdentity()}
	if c.toolRegistry != nil {
		if desc := c.toolRegistry.Get(call.Name); desc != nil {
			req.Declaration = desc.Declaration
		}
	}
	ctx = tools.WithCallID(ctx, call.ID)
	return func(content string) (string, error) {
		return c.hookRegistry.RunToolResultRewriters(ctx, req, content)
	}, nil
}

// callFor is the call res settles: the call the suspended assistant message
// made under its ID, else the held call the conversation admitted it from
// (recordAdmitted), with the arguments an approval with edits ran with. known
// is false when neither names it; the call then carries only the ID.
func (c *Conversation) callFor(
	calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) (call types.MessageToolCall, known bool) {
	call, known = calls()[res.ID]
	if held, ok := c.takeAdmitted(res.ID); ok && !known {
		call, known = held, true
	}
	call.ID = res.ID
	if res.Edited && res.Arguments != nil {
		if args, err := json.Marshal(res.Arguments); err == nil {
			call.Args = args
		}
	}
	return call, known
}

// recordAdmitted records the held call id of tool name, with the arguments
// the model proposed, as approved: its result's request is taken from it
// when the history no longer names the call.
func (c *Conversation) recordAdmitted(id, name string, args map[string]any) {
	call := types.MessageToolCall{ID: id, Name: name}
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			call.Args = raw
		}
	}
	c.admittedMu.Lock()
	defer c.admittedMu.Unlock()
	if c.admitted == nil {
		c.admitted = make(map[string]types.MessageToolCall)
	}
	c.admitted[id] = call
}

// takeAdmitted returns and forgets the call recordAdmitted recorded under id.
func (c *Conversation) takeAdmitted(id string) (types.MessageToolCall, bool) {
	c.admittedMu.Lock()
	defer c.admittedMu.Unlock()
	call, ok := c.admitted[id]
	delete(c.admitted, id)
	return call, ok
}

// forgetAdmitted drops every call recordAdmitted recorded.
func (c *Conversation) forgetAdmitted() {
	c.admittedMu.Lock()
	defer c.admittedMu.Unlock()
	c.admitted = nil
}
