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
// answered with the stage's error result and a tool.call.failed event. A
// result is rewritten once, when it enters the conversation (prepareAnswer,
// prepareApproved); Continue, ContinueDuplex and Resume give the model what
// was stored then.

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
	prep, prepared := c.takePrepared(res)
	call, known := prep.call, true
	if !prepared {
		call, known = c.callFor(calls, res)
	}
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
	case prepared && prep.err != nil:
		return c.settledFailure(ctx, call, prep.err)
	}
	var given tools.ModelResult
	var err error
	if prepared {
		given = c.cappedPrepared(call, res, prep)
	} else {
		var rewrite func(string) (string, error)
		if rewrite, err = c.rewriterFor(ctx, call, known); err == nil {
			given, err = c.modelResult(call, res, rewrite, c.maxResultSize())
		}
	}
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	if given.Parts != nil {
		return types.MessageToolResult{ID: res.ID, Name: call.Name, Parts: given.Parts}
	}
	return types.NewTextToolResult(res.ID, call.Name, given.Text)
}

// cappedPrepared is the result Continue gives the model for res, whose
// rewritten form prep holds: that form, capped at the registry's size limit.
func (c *Conversation) cappedPrepared(
	call types.MessageToolCall, res *sdktools.ToolResolution, prep preparedAnswer,
) tools.ModelResult {
	opts := tools.ModelResultOptions{ToolName: call.Name, MaxSize: c.maxResultSize()}
	if prep.parts != nil {
		given, _ := tools.BuildModelResult(nil, prep.parts, opts) // no rewrite: no error
		return given
	}
	opts.Rewrite = func(string) (string, error) { return prep.text, nil }
	given, _ := c.modelResult(call, res, opts.Rewrite, opts.MaxSize) // the rewrite cannot fail
	return given
}

// maxResultSize is the registry's tool result size limit, zero for none.
func (c *Conversation) maxResultSize() int {
	if c.toolRegistry == nil {
		return 0
	}
	return c.toolRegistry.MaxToolResultSize()
}

// resumeResult is the tool result Resume gives the model for a client's
// answer res: the rejection, the failure (with the partial output the tool
// produced), or what the client sent, unformatted and not truncated (it is
// also the AG-UI TOOL_CALL_RESULT), as rewritten when it was given
// (prepareAnswer). It names the tool of the call the answer settles.
func (c *Conversation) resumeResult(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	prep, prepared := c.takePrepared(res)
	call := prep.call
	if !prepared {
		var known bool
		call, known = c.callFor(calls, res)
		prep, prepared = c.rewrittenAnswer(ctx, call, known, res)
	}
	result, err := c.resumeMessage(call, res, prep, prepared)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	return result
}

// resumeMessage is the tool result Resume gives the model for res, the answer
// to call, with its rewritten form prep when prepared. The error, when the
// rewrite failed, takes the result's place.
func (c *Conversation) resumeMessage(
	call types.MessageToolCall, res *sdktools.ToolResolution, prep preparedAnswer, prepared bool,
) (types.MessageToolResult, error) {
	var result types.MessageToolResult
	switch {
	case res.Rejected:
		result = types.NewTextToolResult(res.ID, "", c.rejectionText(call.Name, res.RejectionReason))
	case res.Error != nil:
		errText := toolResultErrorText(res.Error)
		content := "Tool error: " + errText
		if len(res.ResultJSON) > 0 {
			partial := string(res.ResultJSON)
			if prepared {
				if prep.err != nil {
					return types.MessageToolResult{}, prep.err
				}
				partial = prep.text
			}
			content = partial + "\n\n" + content
		}
		result = types.NewTextToolResult(res.ID, "", content)
		result.Error = errText
	case prepared && prep.err != nil:
		return types.MessageToolResult{}, prep.err
	case len(res.Parts) > 0:
		parts := res.Parts
		if prepared {
			parts = prep.parts
		}
		result = types.MessageToolResult{ID: res.ID, Parts: parts}
	default:
		text := string(res.ResultJSON)
		if prepared {
			text = prep.text
		}
		result = types.NewTextToolResult(res.ID, "", text)
	}
	// A provider matches a result to its call by the tool's name as well as
	// the call ID (Gemini's functionResponse carries only the name).
	result.Name = call.Name
	return result, nil
}

// preparedAnswer is a settled result as the conversation's rewriters
// rewrote it when it was given.
type preparedAnswer struct {
	call types.MessageToolCall
	// text is the rewritten text: of a client's answer as it was sent, of a
	// failed client tool's partial output, or of an approved call's result
	// as formatted for the model.
	text string
	// parts are a multimodal result's parts, their text rewritten.
	parts []types.ContentPart
	// err is why the result cannot be given: the rewrite failed, or no call
	// it answers is known.
	err error
}

// prepareAnswer rewrites res, a client's answer (or a failed client tool's
// partial output) just stored, once, and keeps the rewritten form for the
// result Continue, ContinueDuplex or Resume gives the model. Nothing is kept
// when no hook rewrites results.
func (c *Conversation) prepareAnswer(ctx context.Context, res *sdktools.ToolResolution) {
	if !c.hookRegistry.HasToolResultRewriters() {
		return
	}
	call, known := c.callFor(c.lazyCalls(ctx), res)
	// Validated against the tool's output schema first, as the pipeline
	// validates a result before rewriting it: a rewriter's output is not
	// checked against the schema, and an answer that fails it is not rewritten.
	c.validateAnswer(res, call.Name)
	if prep, prepared := c.rewrittenAnswer(ctx, call, known, res); prepared {
		c.keepPrepared(res, prep)
	}
}

// storeAnswer stores res, a client's answer, for Resume (or Continue) to give
// the model, rewritten once now (prepareAnswer).
func (c *Conversation) storeAnswer(ctx context.Context, res *sdktools.ToolResolution) {
	c.prepareAnswer(ctx, res)
	c.resolvedStore.Add(res)
}

// rewrittenAnswer is res, a client's answer to call, as the conversation's
// rewriters rewrite it; false when none rewrites results or there is nothing
// to rewrite.
func (c *Conversation) rewrittenAnswer(
	ctx context.Context, call types.MessageToolCall, known bool, res *sdktools.ToolResolution,
) (preparedAnswer, bool) {
	if !c.hookRegistry.HasToolResultRewriters() || res.Rejected || (res.Error != nil && len(res.ResultJSON) == 0) {
		return preparedAnswer{}, false
	}
	prep := preparedAnswer{call: call}
	prep.text, prep.parts, prep.err = c.rewriteAnswer(ctx, call, known, string(res.ResultJSON), res.Parts)
	return prep, true
}

// prepareApproved rewrites res, an approved call's result, once, as the
// provider stage rewrites a result it runs (formatted, before the size
// limit), and keeps the rewritten form for Continue or ContinueDuplex.
func (c *Conversation) prepareApproved(ctx context.Context, res *sdktools.ToolResolution) {
	if !c.hookRegistry.HasToolResultRewriters() || res.Rejected || res.Error != nil {
		return
	}
	call, known := c.callFor(c.lazyCalls(ctx), res)
	prep := preparedAnswer{call: call}
	rewrite, err := c.rewriterFor(ctx, call, known)
	if err == nil {
		var given tools.ModelResult
		if given, err = c.modelResult(call, res, rewrite, 0); err == nil {
			prep.text, prep.parts = given.Text, given.Parts
		}
	}
	prep.err = err
	c.keepPrepared(res, prep)
}

// keepPrepared keeps prep for res.
func (c *Conversation) keepPrepared(res *sdktools.ToolResolution, prep preparedAnswer) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	if c.prepared == nil {
		c.prepared = make(map[*sdktools.ToolResolution]preparedAnswer)
	}
	c.prepared[res] = prep
}

// takePrepared returns and forgets what prepareAnswer or prepareApproved kept
// for res.
func (c *Conversation) takePrepared(res *sdktools.ToolResolution) (preparedAnswer, bool) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	prep, ok := c.prepared[res]
	delete(c.prepared, res)
	return prep, ok
}

// RewrittenToolResult returns the text the model will be given for the
// answer stored for the client tool call callID ([Conversation.SendToolResult],
// [Conversation.SendToolResultMultimodal] or [Conversation.FailClientTool]),
// as the conversation's ToolResultRewriter hooks rewrote it when it was
// stored. It returns false when no hook rewrote it: the answer reaches the
// model as it was given. An adapter that shows a client an answer before the
// turn resumes (the AG-UI adapter, when only some calls are answered) uses it.
func (c *Conversation) RewrittenToolResult(callID string) (string, bool) {
	c.settledMu.Lock()
	var res *sdktools.ToolResolution
	var prep preparedAnswer
	for r, p := range c.prepared {
		if r.ID == callID {
			res, prep = r, p
		}
	}
	c.settledMu.Unlock()
	if res == nil {
		return "", false
	}
	result, err := c.resumeMessage(prep.call, res, prep, true)
	if err != nil {
		result = tools.ErrorResult(callID, prep.call.Name, err)
	}
	return result.GetTextContent(), true
}

// rewriteAnswer passes a client's answer to call, its text or (when given)
// its parts, through the conversation's ToolResultRewriter hooks.
func (c *Conversation) rewriteAnswer(
	ctx context.Context, call types.MessageToolCall, known bool, text string, parts []types.ContentPart,
) (string, []types.ContentPart, error) {
	rewrite, err := c.rewriterFor(ctx, call, known)
	switch {
	case err != nil:
		return "", nil, err
	case rewrite == nil:
		return text, parts, nil
	case len(parts) > 0:
		parts, err = rewriteTextParts(parts, rewrite)
		return text, parts, err
	}
	text, err = rewrite(text)
	return text, parts, err
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

// settled forgets what was kept for resolutions, now given to the model:
// their approvals and their rewritten forms.
func (c *Conversation) settled(resolutions []*sdktools.ToolResolution) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	for _, res := range resolutions {
		delete(c.admitted, res.ID)
		delete(c.prepared, res)
	}
}

// forgetSettled forgets everything kept for results not yet given to the
// model: the turn they belong to was abandoned.
func (c *Conversation) forgetSettled() {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	c.admitted, c.prepared = nil, nil
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
		// The emitter the conversation's pipeline reports tool calls with.
		emitter := events.NewEmitter(c.config.eventBus, "", c.ID(), c.ID()).WithUserID(c.config.userID)
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
	call types.MessageToolCall, res *sdktools.ToolResolution, rewrite func(string) (string, error), maxSize int,
) (tools.ModelResult, error) {
	raw := res.ResultJSON
	if raw == nil && len(res.Parts) == 0 {
		if marshaled, err := json.Marshal(res.Result); err == nil {
			raw = marshaled
		} else {
			raw = json.RawMessage(fmt.Sprintf("%v", res.Result))
		}
	}
	return tools.BuildModelResult(raw, res.Parts, tools.ModelResultOptions{
		ToolName: call.Name, Rewrite: rewrite, MaxSize: maxSize,
	})
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
