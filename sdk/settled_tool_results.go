package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
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
// functionResponse carries only the name). A result SettleClientTool prepared
// is formatted and capped as any result Continue gives, but not rewritten
// again (formattedPrepared).
func (c *Conversation) continuedResult(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	if prepared, ok := c.preparedResult(res); ok {
		return c.formattedPrepared(prepared)
	}
	call, known := c.callFor(calls, res)
	switch {
	case res.Rejected:
		result := types.NewTextToolResult(res.ID, call.Name, fmt.Sprintf("Tool call rejected: %s", res.RejectionReason))
		result.Error = "rejected by user"
		return result
	case res.Error != nil:
		return failedResult(call, res)
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

// failedResult is the result Continue gives the model for res, which failed.
// An approved call's failure is worded as the stage words the failure of a
// call it runs: "Tool execution failed: <error>", or the call-rate limit's
// refusal as it is, classified as an approval error. Any other is
// "Tool error: <error>".
func failedResult(call types.MessageToolCall, res *sdktools.ToolResolution) types.MessageToolResult {
	errStr := toolResultErrorText(res.Error)
	if res.Arguments == nil { // only sdktools.ResolveApproved sets Arguments
		result := types.NewTextToolResult(res.ID, call.Name, "Tool error: "+errStr)
		result.Error = errStr
		return result
	}
	var limitErr *stage.CallRateLimitError
	if errors.As(res.Error, &limitErr) {
		result := types.NewTextToolResult(res.ID, call.Name, errStr)
		result.Error = errStr
		result.ErrorType = types.ToolErrorApproval
		return result
	}
	result := types.NewTextToolResult(res.ID, call.Name, "Tool execution failed: "+errStr)
	result.Error = errStr
	return result
}

// resumeResult is the tool result Resume gives the model for a client's
// answer res: the rejection, the failure (with the partial output the tool
// produced, rewritten), or what the client sent, unformatted and not
// truncated (it is also the AG-UI TOOL_CALL_RESULT), passed through the
// conversation's ToolResultRewriter hooks. It names the tool of the call the
// answer settles (callFor). An approved call's result is given as Continue
// gives it (continuedResult), and one SettleClientTool prepared as it was
// prepared, as Resume gives any answer.
func (c *Conversation) resumeResult(
	ctx context.Context, calls func() map[string]types.MessageToolCall, res *sdktools.ToolResolution,
) types.MessageToolResult {
	if result, ok := c.preparedResult(res); ok {
		return result
	}
	if res.Arguments != nil && !res.Rejected {
		return c.continuedResult(ctx, calls, res)
	}
	call, known := c.callFor(calls, res)
	var result types.MessageToolResult
	switch {
	case res.Rejected:
		result = types.NewTextToolResult(res.ID, "", c.rejectionText(call.Name, res.RejectionReason))
	case res.Error != nil:
		result = c.failedClientAnswer(ctx, call, known, res)
	default:
		result = c.clientAnswer(ctx, call, known, res)
	}
	// A provider matches a result to its call by the tool's name as well as
	// the call ID (Gemini's functionResponse carries only the name).
	result.Name = call.Name
	return result
}

// failedClientAnswer is the result Resume gives the model for a client tool
// that failed: its partial output, rewritten, when it produced one, then the
// error.
func (c *Conversation) failedClientAnswer(
	ctx context.Context, call types.MessageToolCall, known bool, res *sdktools.ToolResolution,
) types.MessageToolResult {
	errText := toolResultErrorText(res.Error)
	content := "Tool error: " + errText
	if len(res.ResultJSON) > 0 {
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
	ctx context.Context, call types.MessageToolCall, known bool, res *sdktools.ToolResolution,
) types.MessageToolResult {
	text, parts, err := c.rewriteAnswer(ctx, call, known, string(res.ResultJSON), res.Parts)
	if err != nil {
		return c.settledFailure(ctx, call, err)
	}
	if len(res.Parts) > 0 {
		return types.MessageToolResult{ID: res.ID, Parts: parts}
	}
	return types.NewTextToolResult(res.ID, "", text)
}

// rewriteAnswer prepares a client's answer to call, its text or (when given)
// its parts, as the pipeline prepares a result (tools.BuildModelResult): each
// text part passed through the conversation's ToolResultRewriter hooks, and
// `""` for an empty text, rewriter or not; without formatting or the size
// limit, so the answer stays as the client sent it.
func (c *Conversation) rewriteAnswer(
	ctx context.Context, call types.MessageToolCall, known bool, text string, parts []types.ContentPart,
) (string, []types.ContentPart, error) {
	rewrite, err := c.rewriterFor(ctx, call, known)
	if err != nil {
		return text, parts, err
	}
	given, err := tools.BuildModelResult(json.RawMessage(text), parts, tools.ModelResultOptions{
		ToolName: call.Name, Rewrite: rewrite, Unformatted: true,
	})
	return given.Text, given.Parts, err
}

// settled forgets the approvals recorded for resolutions, now given to the
// model.
func (c *Conversation) settled(resolutions []*sdktools.ToolResolution) {
	c.settledMu.Lock()
	defer c.settledMu.Unlock()
	for _, res := range resolutions {
		delete(c.admitted, res.ID)
		delete(c.prepared, res) // delivered: its admissions stand
	}
}

// forgetSettled forgets every approval and prepared answer not yet given to
// the model: the turn it belongs to was abandoned. A prepared answer's
// admissions are taken back, so it does not keep a slot it never used.
func (c *Conversation) forgetSettled() {
	c.settledMu.Lock()
	releases := make([]func(), 0, len(c.prepared))
	for _, p := range c.prepared {
		releases = append(releases, p.release)
	}
	c.admitted = nil
	c.prepared = nil
	c.settledMu.Unlock()
	runReleases(releases)
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
//     (recordAdmittedRaw);
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
		// The history holds the arguments as the model sent them.
		if made, found := calls()[res.ID]; known && found && made.Name == call.Name {
			call.Args = made.Args
		}
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

// admitCall records the held call id of tool name, approved now, with its
// arguments as the model sent them when the duplex session's record has them
// (else args, the decoded arguments the hold kept, encoded; callFor prefers
// the history's bytes when it settles the call).
func (c *Conversation) admitCall(id, name string, args map[string]any) {
	var raw json.RawMessage
	if recorder, ok := c.duplexSession.(toolCallRecorder); ok {
		if call, found := recorder.ToolCall(id); found && call.Name == name {
			raw = call.Args
		}
	}
	c.recordAdmittedRaw(id, name, raw, args)
}

// recordAdmittedRaw records the held call id of tool name as approved in the
// current turn, with its arguments as the model sent them, raw, when known,
// else args encoded: its result's request is taken from it when no other
// record names the call.
func (c *Conversation) recordAdmittedRaw(id, name string, raw json.RawMessage, args map[string]any) {
	call := types.MessageToolCall{ID: id, Name: name, Args: raw}
	if raw == nil && args != nil {
		if encoded, err := json.Marshal(args); err == nil {
			call.Args = encoded
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
