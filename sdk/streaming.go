package sdk

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	streamPkg "github.com/AltairaLabs/PromptKit/sdk/v2/stream"
)

// StreamChunk represents a single chunk in a streaming response.
type StreamChunk struct {
	// Type of this chunk
	Type ChunkType

	// Text content (for ChunkText type)
	Text string

	// Reasoning is a delta of the model's reasoning (for ChunkReasoning
	// type). It is never part of the reply's text.
	Reasoning string

	// ReplacesText marks a ChunkText whose Text replaces all the text streamed
	// before it in this turn: an output guardrail cut the reply and Text is its
	// message, the reply's whole text (the Text of the ChunkDone Response). A
	// client that renders deltas must discard what it showed of the turn and
	// show Text instead; concatenating would show the cut reply's partial text
	// before the message.
	ReplacesText bool

	// MessageID is the stable id (types.MetaMessageID) of the assistant
	// message a ChunkText's text, a ChunkReasoning's reasoning or a
	// ChunkToolCall's call belongs to, or that a ChunkProviderCallEnd ends:
	// the id the conversation stores the message under, so a client
	// streaming the reply names it as the history does. Empty when the
	// pipeline does not know it, as for a provider that does not stream.
	MessageID string

	// Tool call (for ChunkToolCall type)
	ToolCall *types.MessageToolCall

	// ToolResult is the result a tool call's round fed back to the model (for
	// ChunkToolResult type).
	ToolResult *types.MessageToolResult

	// Round is the provider call of the turn a ChunkProviderCallEnd ends, or
	// a ChunkToolCall or ChunkToolResult belongs to: 0 for the turn's first
	// call to the model, 1 for the next, and so on. It is counted per turn,
	// as the turn index is, and a ResumeStream continues it from where the
	// suspended turn left off. Calls with the same Round were made by one
	// model reply; a new Round is a new reply, even when no result came
	// between them.
	Round int

	// FinishReason is why the provider call a ChunkProviderCallEnd ends
	// stopped: the reply's own finish reason (types.FinishReasonStop,
	// types.FinishReasonToolUse, types.FinishReasonSafety, ...), or, for a
	// call that failed, types.FinishReasonInterrupted when its partial reply
	// is kept and types.FinishReasonError when nothing of it is.
	FinishReason string

	// ToolCalls are the calls the reply a ChunkProviderCallEnd ends made, as
	// its stored message holds them. Each also arrives as a ChunkToolCall,
	// after the ChunkProviderCallEnd.
	ToolCalls []types.MessageToolCall

	// Media content (for ChunkMedia type)
	Media *providers.StreamMediaData

	// ClientTool contains a pending client tool request (for ChunkClientTool type).
	// The caller should fulfill it via SendToolResult/RejectClientTool, then call ResumeStream.
	ClientTool *PendingClientTool

	// Complete response (for ChunkDone type). On an error chunk it holds the
	// partial reply when the stream failed after the model started replying:
	// FinishReason is types.FinishReasonInterrupted, and it is otherwise nil.
	Message *Response

	// Error (if any occurred)
	Error error
}

// ChunkType identifies the type of a streaming chunk.
type ChunkType int

const (
	// ChunkText indicates the chunk contains text content.
	ChunkText ChunkType = iota

	// ChunkToolCall indicates the chunk contains a tool call. A tool round's
	// calls arrive when the round's reply has streamed, before they run, so
	// text after them belongs to the model's next round. A round whose calls
	// suspend the turn (client tools, approval holds) sends them too, before
	// its ChunkClientTool chunks.
	ChunkToolCall

	// ChunkMedia indicates the chunk contains media content.
	ChunkMedia

	// ChunkDone indicates streaming is complete.
	ChunkDone

	// ChunkClientTool indicates a client tool request that needs caller fulfillment.
	ChunkClientTool

	// ChunkToolResult indicates the chunk contains a tool result: one for
	// each call of a tool round the turn went on from, once the call ran, in
	// the order the model is given them. The results of a round that ended
	// the turn are not sent; they are in the ChunkDone Response's
	// TurnMessages.
	ChunkToolResult

	// ChunkReasoning indicates the chunk carries a delta of the model's
	// reasoning, in Reasoning, as a reasoning model streams it before (or
	// between) the text of its reply. A stream sends it only for a
	// conversation opened with [WithStreamReasoning]; the reasoning is always
	// on the Response's Message().Reasoning.
	ChunkReasoning

	// ChunkProviderCallEnd ends one provider call of the turn, in stream
	// order: it follows the call's last ChunkText, ChunkReasoning and
	// ChunkMedia, and comes before the call's ChunkToolCall chunks (and so
	// before its tools run and before any ChunkClientTool it suspends on).
	// Every provider call of the turn ends with exactly one, the last call
	// included, carrying Round, MessageID, FinishReason and ToolCalls. A
	// call that fails still sends one, after its partial text and before
	// the error chunk, with FinishReason types.FinishReasonInterrupted or
	// types.FinishReasonError. A turn an input guardrail blocks makes no
	// provider call and sends none, and neither does a provider that does
	// not stream. StreamRaw and StreamWithCallback do not report it.
	ChunkProviderCallEnd
)

// String returns the string representation of the chunk type.
func (t ChunkType) String() string {
	switch t {
	case ChunkText:
		return contentTypeText
	case ChunkToolCall:
		return "tool_call"
	case ChunkMedia:
		return "media"
	case ChunkDone:
		return "done"
	case ChunkClientTool:
		return "client_tool"
	case ChunkToolResult:
		return "tool_result"
	case ChunkReasoning:
		return "reasoning"
	case ChunkProviderCallEnd:
		return "provider_call_end"
	default:
		return "unknown"
	}
}

// streamState tracks state during stream processing.
type streamState struct {
	contentBuilder strings.Builder
	// lastCallToolCalls are the calls the stream's last ended provider call
	// made (ChunkProviderCallEnd), for a response built without a final
	// result.
	lastCallToolCalls []types.MessageToolCall
	finalResult       *rtpipeline.ExecutionResult
	pendingTools      []PendingClientTool
	// heldTools are the suspended calls an OnToolAsync check holds for
	// approval (Response.PendingTools), kept apart from client tools as Send
	// keeps them.
	heldTools []PendingTool
	// round is the Round of the provider call now streaming: the turn's
	// calls ended so far, those of the turn a ResumeStream continues
	// included.
	round int
}

// accumulatedContent returns the accumulated content as a string.
func (s *streamState) accumulatedContent() string {
	return s.contentBuilder.String()
}

// finishReason recovers the assistant FinishReason from the captured final
// result, or "" when no result was captured. The streamed chunks and the
// pipeline's Response type both drop the field; ExecutionResult.Messages keeps
// it.
func (s *streamState) finishReason() string {
	if s.finalResult == nil {
		return ""
	}
	return lastAssistantFinishReason(s.finalResult.Messages)
}

// Stream sends a message and returns a channel of response chunks.
//
// Use this for real-time streaming of LLM responses:
//
//	for chunk := range conv.Stream(ctx, "Tell me a story") {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
//
// The channel is closed when the response is complete or an error occurs.
// The final chunk (Type == ChunkDone) contains the complete Response.
//
// A stream holds the conversation's turn until it completes: drain the
// channel, or cancel ctx to stop early. A stream abandoned with ctx still
// live blocks every later turn on the conversation.
func (c *Conversation) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	ch := make(chan StreamChunk, streamChannelBufferSize)

	// Register the caller's context with the OTel listener before launching the
	// goroutine so the session exists when pipeline events start arriving.
	c.startOTelSession(ctx)

	go func() {
		defer close(ch)
		startTime := time.Now()

		// Hold the conversation's turn until the stream completes or the
		// caller's context ends (#2570).
		release, gateErr := c.turn.acquire(ctx, c)
		if gateErr != nil {
			sendFinalErrorChunk(ctx, ch, StreamChunk{Error: gateErr})
			return
		}
		defer release()
		ctx = c.turn.mark(ctx, c)

		c.mu.RLock()
		if err := c.requireUnary("Stream()"); err != nil {
			c.mu.RUnlock()
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
			return
		}
		c.mu.RUnlock()

		// Build user message with options
		userMsg, sendCfg, err := c.startStreamTurn(ctx, message, opts)
		if err != nil {
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
			return
		}

		// Carry WithJSONInput-bound variables on the context so the send-scoped
		// variable provider resolves them for this turn.
		ctx = withSendVars(ctx, sendCfg.jsonInputVars)

		// Re-select this turn's skills from its text, as Send does.
		c.refreshSelectorBoundCapabilities(ctx, extractUserText(userMsg))

		// Execute streaming pipeline
		if err := c.executeStreamingPipeline(ctx, userMsg, ch, startTime); err != nil {
			sendFinalErrorChunk(ctx, ch, errorChunk(err))
		}
	}()

	return ch
}

// startStreamTurn builds the stream's user message and applies its
// WithHistory and WithClientTools, as Send does.
func (c *Conversation) startStreamTurn(
	ctx context.Context, message any, opts []SendOption,
) (*types.Message, *sendConfig, error) {
	userMsg, sendCfg, err := c.buildStreamMessage(message, opts)
	if err != nil {
		return nil, nil, err
	}
	if err := c.startTurnInput(ctx, sendCfg); err != nil {
		return nil, nil, err
	}
	return userMsg, sendCfg, nil
}

// buildStreamMessage constructs a user message from input and options. It
// returns the parsed sendConfig so the caller can carry any WithJSONInput-bound
// variables on the context.
func (c *Conversation) buildStreamMessage(message any, opts []SendOption) (*types.Message, *sendConfig, error) {
	sendCfg, err := parseSendOptions(opts)
	if err != nil {
		return nil, nil, err
	}

	// Fall back to the JSON input as the user turn when the caller passes an
	// empty message.
	if isEmptyMessage(message) && len(sendCfg.jsonInputRaw) > 0 {
		message = string(sendCfg.jsonInputRaw)
	}

	// Build user message from input
	var userMsg *types.Message
	switch m := message.(type) {
	case string:
		if err := c.validateMessageSize(len(m)); err != nil {
			return nil, nil, err
		}
		userMsg = &types.Message{Role: roleUser}
		userMsg.AddTextPart(m)
	case *types.Message:
		if err := c.validateMessageSize(messageContentSize(m)); err != nil {
			return nil, nil, err
		}
		userMsg = m
	default:
		return nil, nil, fmt.Errorf("message must be string or *types.Message, got %T", message)
	}

	// Add content parts to message
	if err := c.addContentParts(userMsg, sendCfg.parts); err != nil {
		return nil, nil, err
	}
	userMsg = withMessageMetadata(userMsg, sendCfg.messageMetadata)

	return userMsg, sendCfg, nil
}

// addContentParts adds content parts from options to the message.
func (c *Conversation) addContentParts(msg *types.Message, parts []any) error {
	for _, part := range parts {
		if err := addContentPart(msg, part); err != nil {
			return err
		}
	}
	return nil
}

// addContentPart adds one content part from options to the message.
func addContentPart(msg *types.Message, part any) error {
	switch p := part.(type) {
	case imageFilePart:
		if err := msg.AddImagePart(p.path, p.detail); err != nil {
			return fmt.Errorf("failed to add image from file: %w", err)
		}
	case imageURLPart:
		msg.AddImagePartFromURL(p.url, p.detail)
	case imageDataPart:
		base64Data := base64.StdEncoding.EncodeToString(p.data)
		contentPart := types.NewImagePartFromData(base64Data, p.mimeType, p.detail)
		msg.AddPart(contentPart)
	case audioFilePart:
		if err := msg.AddAudioPart(p.path); err != nil {
			return fmt.Errorf("failed to add audio from file: %w", err)
		}
	case audioDataPart:
		base64Data := base64.StdEncoding.EncodeToString(p.data)
		contentPart := types.NewAudioPartFromData(base64Data, p.mimeType)
		msg.AddPart(contentPart)
	case videoFilePart:
		if err := msg.AddVideoPart(p.path); err != nil {
			return fmt.Errorf("failed to add video from file: %w", err)
		}
	case videoDataPart:
		base64Data := base64.StdEncoding.EncodeToString(p.data)
		contentPart := types.NewVideoPartFromData(base64Data, p.mimeType)
		msg.AddPart(contentPart)
	case documentFilePart:
		if err := msg.AddDocumentPart(p.path); err != nil {
			return fmt.Errorf("failed to add document from file: %w", err)
		}
	case documentDataPart:
		base64Data := base64.StdEncoding.EncodeToString(p.data)
		contentPart := types.NewDocumentPartFromData(base64Data, p.mimeType)
		msg.AddPart(contentPart)
	case imageStorageRefPart:
		msg.AddPart(types.NewImagePartFromStorageRef(p.ref, p.mimeType, p.detail))
	case audioStorageRefPart:
		msg.AddPart(types.NewAudioPartFromStorageRef(p.ref, p.mimeType))
	case videoStorageRefPart:
		msg.AddPart(types.NewVideoPartFromStorageRef(p.ref, p.mimeType))
	case documentStorageRefPart:
		docPart := types.NewDocumentPartFromStorageRef(p.ref, p.mimeType)
		if p.name != "" && docPart.Media != nil {
			name := p.name
			docPart.Media.Caption = &name
		}
		msg.AddPart(docPart)
	case filePart:
		// Legacy file part - kept for backward compatibility
		// Try to detect if it's a document by checking the name extension
		msg.AddTextPart(fmt.Sprintf("[File: %s]\n%s", p.name, string(p.data)))
	default:
		return fmt.Errorf("unknown content part type: %T", part)
	}
	return nil
}

// executeStreamingPipeline builds and executes the LLM pipeline in streaming mode.
func (c *Conversation) executeStreamingPipeline(
	ctx context.Context,
	userMsg *types.Message,
	outCh chan<- StreamChunk,
	startTime time.Time,
) error {
	// End the previous turn's render cache — see Send (#1959). The streaming
	// path shares the Conversation's TurnState, so it freezes the same way.
	c.turnState.BeginTurn()
	c.discardAbandonedResolutions()

	// Execute streaming through the unary session (only called from Stream which checks mode)
	streamCh, err := c.unarySession.ExecuteStreamWithMessage(c.withConversationState(ctx), *userMsg)
	if err != nil {
		return fmt.Errorf("pipeline streaming failed: %w", c.wrapTurnError(err))
	}

	// Process stream and finalize
	state := &streamState{}
	if err := c.processAndFinalizeStreamWithState(ctx, streamCh, outCh, startTime, state); err != nil {
		return err
	}

	// Skip lifecycle hooks when pipeline is suspended for pending tools
	if len(state.pendingTools) == 0 {
		c.sessionHooks.IncrementTurn()
		c.sessionHooks.SessionUpdate(ctx)
		c.evalMW.dispatchTurnEvals(ctx)
	}

	return nil
}

// processAndFinalizeStreamWithState processes the streaming response, emits chunks, and sends
// the final ChunkDone. Uses a caller-provided state
// so the caller can inspect pendingTools after the stream completes.
func (c *Conversation) processAndFinalizeStreamWithState(
	ctx context.Context,
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	startTime time.Time,
	state *streamState,
) error {
	// Process all stream chunks
	if err := c.processStreamChunks(ctx, streamCh, outCh, state); err != nil {
		if state.finalResult != nil && endsInterrupted(state.finalResult.Messages) {
			return &interruptedStreamError{err: err, partial: c.buildStreamingResponse(state, startTime)}
		}
		return err
	}

	// Build response from accumulated data
	resp := c.buildStreamingResponse(state, startTime)

	// Emit final ChunkDone with complete response, respecting context cancellation
	// to avoid blocking indefinitely when the consumer has gone away.
	select {
	case outCh <- StreamChunk{Type: ChunkDone, Message: resp}:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// processStreamChunks processes provider chunks and emits SDK chunks.
// It respects context cancellation to avoid goroutine leaks when the consumer
// abandons the output channel.
func (c *Conversation) processStreamChunks(
	ctx context.Context,
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) error {
	var streamErr error
	for chunk := range streamCh {
		if streamErr != nil {
			// After a failure, only the session's final result is wanted: it
			// carries the partial reply the pipeline kept for the turn.
			if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
				state.finalResult = result
			}
			continue
		}
		if chunk.Error != nil {
			streamErr = c.wrapTurnError(chunk.Error)
			continue
		}

		c.emitStreamChunk(ctx, &chunk, outCh, state)
	}
	return streamErr
}

// interruptedStreamError carries a failed stream's partial reply to the
// goroutine that reports the error, which attaches it to the error chunk.
type interruptedStreamError struct {
	err     error
	partial *Response
}

func (e *interruptedStreamError) Error() string { return e.err.Error() }
func (e *interruptedStreamError) Unwrap() error { return e.err }

// canceledErrorSendTimeout bounds the final error send after the caller's
// context is done.
const canceledErrorSendTimeout = 500 * time.Millisecond

// sendFinalErrorChunk delivers the chunk that ends a failed stream. A caller
// that canceled its own context usually still drains the channel, and the
// chunk tells it the stream was cut off and carries the partial reply; a
// caller that has stopped reading must not leak this goroutine, so once ctx is
// done the send waits only briefly.
func sendFinalErrorChunk(ctx context.Context, ch chan<- StreamChunk, chunk StreamChunk) {
	if ctx.Err() == nil {
		select {
		case ch <- chunk:
			return
		case <-ctx.Done():
		}
	}
	timer := time.NewTimer(canceledErrorSendTimeout)
	defer timer.Stop()
	select {
	case ch <- chunk:
	case <-timer.C:
	}
}

// errorChunk builds the chunk reporting a stream failure, carrying the
// partial reply when there is one.
func errorChunk(err error) StreamChunk {
	var ie *interruptedStreamError
	if errors.As(err, &ie) {
		return StreamChunk{Error: ie.err, Message: ie.partial}
	}
	return StreamChunk{Error: err}
}

// sendChunk sends a StreamChunk to outCh, returning immediately if ctx is canceled.
func sendChunk(ctx context.Context, outCh chan<- StreamChunk, chunk StreamChunk) {
	select {
	case outCh <- chunk:
	case <-ctx.Done():
	}
}

// emitStreamChunk converts a provider chunk to SDK chunk(s) and updates state.
// All channel sends respect context cancellation to prevent goroutine leaks.
func (c *Conversation) emitStreamChunk(
	ctx context.Context,
	chunk *providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) {
	if c.config.streamReasoning && chunk.Reasoning != "" {
		sendChunk(ctx, outCh, StreamChunk{Type: ChunkReasoning, Reasoning: chunk.Reasoning, MessageID: chunk.MessageID})
	}
	emitTextDelta(ctx, chunk, outCh, state)

	// Emit media data
	if chunk.MediaData != nil {
		sendChunk(ctx, outCh, StreamChunk{Type: ChunkMedia, Media: chunk.MediaData})
	}

	// A call's tool calls are reported once, from the round the pipeline
	// reports after the call ended (emitRoundMessages), never from the
	// chunks: those of a pipeline stream carry none.
	if chunk.ProviderCallEnd != nil {
		emitProviderCallEnd(ctx, chunk.ProviderCallEnd, outCh, state)
	}

	emitRoundMessages(ctx, chunk.RoundMessages, outCh, state)

	if chunk.FinishReason != nil && *chunk.FinishReason == "pending_tools" && len(chunk.PendingTools) > 0 {
		c.emitSuspendedTools(ctx, chunk.PendingTools, outCh, state)
	}

	// Capture final result
	if chunk.FinishReason != nil {
		if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			state.finalResult = result
		}
	}
}

// emitSuspendedTools sorts the calls a turn suspended on: a client tool is
// sent for the caller to answer; an approval hold is not, since the caller
// resolves it (ResolveTool), not answers it.
func (c *Conversation) emitSuspendedTools(
	ctx context.Context, pending []tools.PendingToolExecution, outCh chan<- StreamChunk, state *streamState,
) {
	for i := range pending {
		if held, ok := c.heldTool(ctx, &pending[i]); ok {
			state.heldTools = append(state.heldTools, held)
			continue
		}
		pct := buildPendingClientToolFromExecution(&pending[i])
		state.pendingTools = append(state.pendingTools, pct)
		sendChunk(ctx, outCh, StreamChunk{Type: ChunkClientTool, ClientTool: &pct})
	}
}

// emitProviderCallEnd forwards the end of a provider call as a
// ChunkProviderCallEnd with the call's Round, and moves the stream on to the
// next call.
func emitProviderCallEnd(
	ctx context.Context, end *providers.ProviderCallEnd, outCh chan<- StreamChunk, state *streamState,
) {
	calls := append([]types.MessageToolCall(nil), end.ToolCalls...)
	state.lastCallToolCalls = calls
	sendChunk(ctx, outCh, StreamChunk{
		Type: ChunkProviderCallEnd, Round: state.round, MessageID: end.MessageID,
		FinishReason: end.FinishReason, ToolCalls: calls,
	})
	state.round++
}

// emitRoundMessages forwards a tool round the pipeline reported: a
// ChunkToolCall for each call an assistant message made, and a
// ChunkToolResult for each tool result. The pipeline reports a call's round
// after the call ended and before the next one starts, so the round is the
// last ended call's.
func emitRoundMessages(ctx context.Context, msgs []types.Message, outCh chan<- StreamChunk, state *streamState) {
	round := max(state.round-1, 0)
	for i := range msgs {
		msg := &msgs[i]
		for j := range msg.ToolCalls {
			call := msg.ToolCalls[j]
			sendChunk(ctx, outCh, StreamChunk{Type: ChunkToolCall, ToolCall: &call, Round: round, MessageID: msg.ID()})
		}
		if msg.ToolResult != nil {
			result := *msg.ToolResult
			sendChunk(ctx, outCh, StreamChunk{Type: ChunkToolResult, ToolResult: &result, Round: round})
		}
	}
}

// emitTextDelta forwards a chunk's text delta. A delta marked ReplacesText
// replaces everything streamed before it in the turn, so the accumulated
// content restarts from it.
func emitTextDelta(ctx context.Context, chunk *providers.StreamChunk, outCh chan<- StreamChunk, state *streamState) {
	if chunk.Delta == "" {
		return
	}
	if chunk.ReplacesText {
		state.contentBuilder.Reset()
	}
	state.contentBuilder.WriteString(chunk.Delta)
	sendChunk(ctx, outCh, StreamChunk{
		Type: ChunkText, Text: chunk.Delta, ReplacesText: chunk.ReplacesText, MessageID: chunk.MessageID,
	})
}

// buildPendingClientToolFromExecution converts a tools.PendingToolExecution to a PendingClientTool.
func buildPendingClientToolFromExecution(pt *tools.PendingToolExecution) PendingClientTool {
	pct := PendingClientTool{
		CallID:   pt.CallID,
		ToolName: pt.ToolName,
		Args:     pt.Args,
	}
	if pt.PendingInfo != nil {
		pct.ConsentMsg = pt.PendingInfo.Message
		if cats, ok := pt.PendingInfo.Metadata["categories"]; ok {
			if catSlice, ok := cats.([]string); ok {
				pct.Categories = catSlice
			}
		}
	}
	return pct
}

// buildStreamingResponse creates a Response from streaming data.
func (c *Conversation) buildStreamingResponse(
	state *streamState,
	startTime time.Time,
) *Response {
	resp := &Response{
		duration: time.Since(startTime),
	}

	result := state.finalResult

	// Use result data if available
	if result != nil && result.Response != nil {
		resp.message = &types.Message{
			Role:     roleAssistant,
			Content:  result.Response.Content,
			Parts:    result.Response.Parts,
			CostInfo: &result.CostInfo,
		}

		if len(result.Response.ToolCalls) > 0 {
			resp.toolCalls = result.Response.ToolCalls
		}

		// Extract validations
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == roleAssistant && len(result.Messages[i].Validations) > 0 {
				resp.validations = result.Messages[i].Validations
				break
			}
		}
	} else {
		// Build from accumulated streaming data
		resp.message = &types.Message{
			Role:    roleAssistant,
			Content: state.accumulatedContent(),
		}
		if len(state.lastCallToolCalls) > 0 {
			resp.toolCalls = state.lastCallToolCalls
		}
	}

	// Neither the streamed chunks nor the pipeline's narrower Response type
	// carry FinishReason, so recover it from the final result exactly as
	// buildResponse does for Send. Without this a streaming caller cannot
	// distinguish a guardrail-blocked turn (marked types.FinishReasonSafety)
	// from a real model reply, nor see max_output_tokens or refusal (#1715).
	resp.message.FinishReason = state.finishReason()

	// Likewise for the reasoning trace: neither the streamed chunks nor the
	// pipeline Response carry it, so recover it from the final result exactly
	// as buildResponse does for Send. Without this, fixing one path would
	// leave the other silently returning nil.
	if state.finalResult != nil {
		resp.message.Reasoning = lastAssistantReasoning(state.finalResult.Messages)
		resp.turnMessages = turnMessagesOf(state.finalResult.Messages)
	}

	// Populate pending client tools from stream state
	if len(state.pendingTools) > 0 {
		resp.clientTools = state.pendingTools
	}
	resp.pendingTools = state.heldTools

	return resp
}

// StreamRaw returns a channel of streaming chunks for use with the stream package.
// This is a lower-level API that returns stream.Chunk types.
//
// Most users should use [Conversation.Stream] instead.
// StreamRaw is useful when working with [stream.Process] or [stream.CollectText].
// As with Stream, drain the channel or cancel ctx: the stream holds the
// conversation's turn until it ends.
//
//	err := stream.Process(ctx, conv, "Hello", func(chunk stream.Chunk) error {
//	    fmt.Print(chunk.Text)
//	    return nil
//	})
func (c *Conversation) StreamRaw(ctx context.Context, message any) (<-chan streamPkg.Chunk, error) {
	ch := make(chan streamPkg.Chunk, streamChannelBufferSize)

	go func() {
		defer close(ch)

		for sdkChunk := range c.Stream(ctx, message) {
			// Convert SDK StreamChunk to stream.Chunk
			chunk := streamPkg.Chunk{
				Error: sdkChunk.Error,
			}

			switch sdkChunk.Type {
			case ChunkText:
				chunk.Type = streamPkg.ChunkText
				chunk.Text = sdkChunk.Text
				chunk.ReplacesText = sdkChunk.ReplacesText
			case ChunkToolCall:
				chunk.Type = streamPkg.ChunkToolCall
				chunk.ToolCall = sdkChunk.ToolCall
			case ChunkMedia:
				chunk.Type = streamPkg.ChunkMedia
				chunk.Media = sdkChunk.Media
			case ChunkClientTool, ChunkToolResult, ChunkReasoning, ChunkProviderCallEnd:
				// Client tool requests, tool results, reasoning and provider
				// call ends are SDK-level; skip in raw stream
				continue
			case ChunkDone:
				chunk.Done = true
			}

			select {
			case ch <- chunk:
			case <-ctx.Done():
				// Keep draining Stream so its goroutine sees ctx end and
				// releases the conversation's turn.
			}
		}
	}()

	return ch, nil
}
