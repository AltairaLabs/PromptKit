package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

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

// resumeResult is the tool result Resume gives the model for a client's
// answer res: the rejection, the failure (with the partial output the tool
// produced, rewritten), or what the client sent, unformatted and not
// truncated (it is also the AG-UI TOOL_CALL_RESULT), passed through the
// conversation's ToolResultRewriter hooks. It names the tool the suspended
// turn called under the answer's ID.
func (c *Conversation) resumeResult(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	var result types.MessageToolResult
	switch {
	case res.Rejected:
		result = types.NewTextToolResult(res.ID, "", c.rejectionText(calls()[res.ID].Name, res.RejectionReason))
	case res.Error != nil:
		result = c.failedClientAnswer(ctx, calls, res)
	default:
		result = c.clientAnswer(ctx, calls, res)
	}
	// A provider matches a result to its call by the tool's name as well as
	// the call ID (Gemini's functionResponse carries only the name).
	result.Name = calls()[res.ID].Name
	return result
}

// failedClientAnswer is the result Resume gives the model for a client tool
// that failed: its partial output, rewritten, when it produced one, then the
// error.
func (c *Conversation) failedClientAnswer(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	errText := toolResultErrorText(res.Error)
	content := "Tool error: " + errText
	if len(res.ResultJSON) > 0 {
		call, known := c.callFor(calls, res)
		partial, _, err := c.rewriteAnswer(ctx, call, known, string(res.ResultJSON), nil)
		if err != nil {
			return c.settledFailure(ctx, call, err)
		}
		content = partial + "\n\n" + content
	}
	result := types.NewTextToolResult(res.ID, "", content)
	result.Error = errText
	return result
}

// clientAnswer is the result Resume gives the model for what a client sent.
func (c *Conversation) clientAnswer(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	call, known := c.callFor(calls, res)
	text, parts, err := c.rewriteAnswer(ctx, call, known, string(res.ResultJSON), res.Parts)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	if len(res.Parts) > 0 {
		return types.MessageToolResult{ID: res.ID, Parts: parts}
	}
	return types.NewTextToolResult(res.ID, "", text)
}

// rewriteAnswer passes a client's answer to call, its text or (when given)
// its parts, through the conversation's ToolResultRewriter hooks. The
// outcome for the same answer is kept until the answer is used, so the
// hooks see an answer once however often it is prepared
// (ToolResultForModel, then Resume).
func (c *Conversation) rewriteAnswer(
	ctx context.Context, call types.MessageToolCall, known bool, text string, parts []types.ContentPart,
) (string, []types.ContentPart, error) {
	if kept, ok := c.keptRewrite(call.ID, text, parts); ok {
		return kept.outText, kept.outParts, kept.err
	}
	out := answerRewrite{inText: text, inParts: parts, outText: text, outParts: parts}
	rewrite, err := c.rewriterFor(ctx, call, known)
	switch {
	case err != nil:
		out.err = err
	case rewrite == nil:
	case len(parts) > 0:
		out.outParts, out.err = rewriteTextParts(parts, rewrite)
	default:
		out.outText, out.err = rewrite(text)
	}
	c.keepRewrite(call.ID, out)
	return out.outText, out.outParts, out.err
}

// answerRewrite is the outcome of rewriting a client's answer.
type answerRewrite struct {
	inText   string
	inParts  []types.ContentPart
	outText  string
	outParts []types.ContentPart
	err      error
}

// keptRewrite returns the outcome kept for the answer to call id, when it was
// for the same answer.
func (c *Conversation) keptRewrite(id, text string, parts []types.ContentPart) (answerRewrite, bool) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	kept, ok := c.rewrites[id]
	if !ok || kept.inText != text || !reflect.DeepEqual(kept.inParts, parts) {
		return answerRewrite{}, false
	}
	return kept, true
}

// keepRewrite keeps the outcome of rewriting the answer to call id.
func (c *Conversation) keepRewrite(id string, out answerRewrite) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	if c.rewrites == nil {
		c.rewrites = make(map[string]answerRewrite)
	}
	c.rewrites[id] = out
}

// rewriteTextParts passes each text part of parts through rewrite, keeping
// media parts where they are and each part's other fields. Nil rewrite
// returns parts as they are.
func rewriteTextParts(parts []types.ContentPart, rewrite func(string) (string, error)) ([]types.ContentPart, error) {
	if rewrite == nil {
		return parts, nil
	}
	out := make([]types.ContentPart, len(parts))
	for i, p := range parts {
		out[i] = p
		if p.Type != types.ContentTypeText || p.Text == nil {
			continue
		}
		text, err := rewrite(*p.Text)
		if err != nil {
			return nil, err
		}
		out[i].Text = &text
	}
	return out, nil
}

// RewritesToolResults reports whether the conversation has a tool hook that
// rewrites tool results (hooks.ToolResultRewriter, see [WithToolHook]).
func (c *Conversation) RewritesToolResults() bool {
	return c.hookRegistry.HasToolResultRewriters()
}

// ToolResultForModel returns the tool result [Conversation.Resume] will give
// the model for the answer stored for the client tool call callID
// ([Conversation.SendToolResult], [Conversation.SendToolResultMultimodal],
// [Conversation.FailClientTool] or [Conversation.RejectClientTool]): the
// answer as the conversation's ToolResultRewriter hooks rewrite it. The hooks
// see the answer once: Resume gives the model this same result. It returns
// false when no answer is stored for callID. An adapter that shows a client
// what the model is given before the turn resumes (the AG-UI adapter, when
// only some calls are answered) uses it.
func (c *Conversation) ToolResultForModel(ctx context.Context, callID string) (*types.MessageToolResult, bool) {
	c.settledMu.Lock()
	res, ok := c.answers[callID]
	c.settledMu.Unlock()
	if !ok {
		return nil, false
	}
	preview := *res
	calls := c.lazyCalls(ctx)
	c.validateDeferredResults([]*sdktools.ToolResolution{&preview}, callNamesOf(calls))
	result := c.resumeResult(ctx, calls, &preview)
	return &result, true
}

// addAnswer stores res, a client's answer, for Resume (or Continue) to give
// the model, and keeps it by call for ToolResultForModel.
func (c *Conversation) addAnswer(res *sdktools.ToolResolution) {
	c.resolvedStore.Add(res)
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	if c.answers == nil {
		c.answers = make(map[string]*sdktools.ToolResolution)
	}
	c.answers[res.ID] = res
}

// settled forgets what was kept for resolutions, now given to the model: the
// answers stored for their calls, their rewrites and the approvals recorded
// for them.
func (c *Conversation) settled(resolutions []*sdktools.ToolResolution) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	for _, res := range resolutions {
		delete(c.answers, res.ID)
		delete(c.rewrites, res.ID)
		delete(c.admitted, res.ID)
	}
}

// forgetSettled forgets everything kept for answers and approvals not yet
// given to the model: the turn they belong to was abandoned.
func (c *Conversation) forgetSettled() {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	c.answers, c.rewrites, c.admitted = nil, nil, nil
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
	var emitter *events.Emitter
	if c.config != nil {
		emitter = c.newEmitter(c.config.eventBus)
	}
	if emitter != nil {
		var labels map[string]string
		if c.toolRegistry != nil {
			if desc := c.toolRegistry.Get(call.Name); desc != nil {
				labels = desc.Labels
			}
		}
		emitter.ToolCallEventCtx(ctx, events.EventToolCallFailed, &events.ToolCallEventData{
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

// callFor is the call res settles, from the first that knows it:
//
//   - the duplex session's record of the calls the model made (the current
//     call under a reused ID, and known before the tool-call message reaches
//     the history);
//   - the held call the conversation approved in the current turn
//     (recordAdmitted);
//   - the suspended turn's last assistant message in the history.
//
// An approval with edits gives the arguments it ran with. known is false when
// none names the call; the call then carries only the ID.
func (c *Conversation) callFor(
	calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) (call types.MessageToolCall, known bool) {
	if recorder, ok := c.duplexSession.(toolCallRecorder); ok {
		call, known = recorder.ToolCall(res.ID)
	}
	if !known {
		call, known = c.admittedCall(res.ID)
	}
	if !known {
		call, known = calls()[res.ID]
	}
	call.ID = res.ID
	if res.Edited && res.Arguments != nil {
		if args, err := json.Marshal(res.Arguments); err == nil {
			call.Args = args
		}
	}
	return call, known
}

// toolCallRecorder is a duplex session that records the calls the model made
// and counts the turns it completed.
type toolCallRecorder interface {
	ToolCall(id string) (types.MessageToolCall, bool)
	TurnsCompleted() int64
}

// duplexTurn is how many turns the duplex session has completed; zero for a
// unary conversation.
func (c *Conversation) duplexTurn() int64 {
	if recorder, ok := c.duplexSession.(toolCallRecorder); ok {
		return recorder.TurnsCompleted()
	}
	return 0
}

// admittedCall is a held call approved in the current turn.
type admittedCall struct {
	call types.MessageToolCall
	turn int64
}

// recordAdmitted records the held call id of tool name, with the arguments
// the model proposed, as approved in the current turn: its result's request
// is taken from it when no other record names the call.
func (c *Conversation) recordAdmitted(id, name string, args map[string]any) {
	call := types.MessageToolCall{ID: id, Name: name}
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			call.Args = raw
		}
	}
	turn := c.duplexTurn()
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	if c.admitted == nil {
		c.admitted = make(map[string]admittedCall)
	}
	c.admitted[id] = admittedCall{call: call, turn: turn}
}

// admittedCall returns the call recordAdmitted recorded under id in the
// current turn. One recorded in a turn that has since ended is dropped: a
// later response may reuse its ID for another call.
func (c *Conversation) admittedCall(id string) (types.MessageToolCall, bool) {
	turn := c.duplexTurn()
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	rec, ok := c.admitted[id]
	if !ok {
		return types.MessageToolCall{}, false
	}
	if rec.turn != turn {
		delete(c.admitted, id)
		return types.MessageToolCall{}, false
	}
	return rec.call, true
}
