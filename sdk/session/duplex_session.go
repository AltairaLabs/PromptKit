// Package session provides session abstractions for managing conversations.
// Sessions wrap pipelines and provide convenient APIs for text and duplex streaming interactions.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const errSessionClosed = "session is closed"

// duplexSession implements BidirectionalSession with stage-based StreamPipeline.
// It manages streaming input/output through the stage pipeline.
//
// Two modes:
// - ASM mode: DuplexProviderStage creates session lazily, one long-running pipeline execution
// - VAD mode: No providerSession, VAD triggers multiple pipeline executions with one-shot calls
type duplexSession struct {
	id       string
	store    statestore.Store
	pipeline *stage.StreamPipeline
	provider providers.Provider // Provider for LLM calls
	// streamConfig is the realtime (ASM) streaming config the session was
	// opened with, nil in pipeline mode; ForkSession passes it on.
	streamConfig *providers.StreamingInputConfig

	// calls maps each tool call the model made and nobody has answered yet,
	// by ID, to the call (its name and arguments as sent), so a result
	// submitted by call ID alone can be named (#2589) and the request it
	// answers known. A later response that reuses an ID replaces the earlier
	// call under it. A call is dropped once it is answered, by the pipeline,
	// the session or SubmitToolResults. Calls held for the caller are kept
	// until answered; the others are bounded (maxRecordedCalls).
	callsMu sync.Mutex
	calls   map[string]recordedCall
	// callOrder is the IDs of the calls not held, oldest first, for the
	// bound.
	callOrder []string
	// unheld counts the recorded calls not held.
	unheld int
	// turnsCompleted counts the replies the model completed (see
	// TurnsCompleted).
	turnsCompleted atomic.Int64
	// Note: Session is NOT stored here - DuplexProviderStage creates and manages it
	// pipelineRunsTools: the pipeline runs the session's tool calls. Otherwise
	// a session with toolRegistry runs them, gated by asyncToolChecker, and
	// one without forwards them to the caller. See handleToolCalls.
	pipelineRunsTools bool
	toolRegistry      *tools.Registry
	asyncToolChecker  AsyncToolChecker
	caller            types.Caller // who the conversation acts for; on every tool call the session runs
	// onTurnComplete is called for each turn the session completes
	// (DuplexSessionConfig.OnTurnComplete); nil for none.
	onTurnComplete func(ctx context.Context)
	variables      map[string]string
	varsMu         sync.RWMutex
	closeMu        sync.Mutex
	closed         bool

	// Internal pipeline channel (stage.StreamElement)
	stageInput chan stage.StreamElement // Feeds converted elements to pipeline

	// inputMu guards sends on stageInput against Close closing it: every send
	// holds the read lock across its channel send, and Close takes the write
	// lock to close the channel, so no send can land on a closed channel
	// (#2606). inputClosed is set under the write lock.
	inputMu     sync.RWMutex
	inputClosed bool

	// External API channel (providers.StreamChunk)
	streamOutput chan providers.StreamChunk // Consumer receives chunks here

	// responseTaken records whether Response() has been called. Until it is, no
	// one can read streamOutput, so the forwarding loop must not block on it —
	// otherwise a caller that reacts only through OnClientTool handlers (and never
	// reads Response()) silently stalls the whole pipeline once the buffer fills.
	// See forwardChunk and issue #1638.
	responseTaken atomic.Bool
	// droppedChunks counts output chunks discarded because Response() was never
	// taken. Its first increment emits a one-time warning.
	droppedChunks atomic.Int64
	// warnedSlowConsumer guards the one-time "consumer is not draining" warning
	// for a taken-but-stalled Response().
	warnedSlowConsumer atomic.Bool

	// Pipeline execution control
	executionStarted bool
	executionMu      sync.Mutex
	// warnedLateVar guards the one-time warning for a SetVar made after the
	// pipeline started, when the value can no longer reach the provider.
	warnedLateVar atomic.Bool

	// sessionCtx is a session-level context created during NewDuplexSession.
	// It is used for pipeline execution instead of the first SendChunk's context,
	// ensuring the pipeline outlives any single RPC call.
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	// pipelineDone is closed when executePipeline returns, signaling that the session has ended.
	// This is a separate signal from streamOutput so that Done() does not drain the output channel.
	pipelineDone chan struct{}

	// Done channel (initialized once via sync.Once)
	doneCh   chan struct{}
	doneOnce sync.Once
}

const streamBufferSize = 100 // Size of buffered channels for streaming

// initConversationState seeds initial metadata for a new conversation if
// any is provided. Otherwise the conversation is created lazily on the
// first typed write (AppendMessages, MergeMetadata, SaveSummary). No bulk
// Save needed.
func initConversationState(ctx context.Context, store statestore.Store, cfg *DuplexSessionConfig, convID string) error {
	if err := seedUserID(ctx, store, convID, cfg.UserID); err != nil {
		return err
	}
	if len(cfg.Metadata) == 0 {
		return nil
	}
	accessor, ok := store.(statestore.MetadataAccessor)
	if !ok {
		return fmt.Errorf("session: store does not implement MetadataAccessor; cannot seed initial metadata")
	}
	if err := accessor.MergeMetadata(ctx, convID, cfg.Metadata); err != nil {
		return fmt.Errorf("failed to seed conversation metadata: %w", err)
	}
	return nil
}

// NewDuplexSession creates a bidirectional session from a config.
// PipelineBuilder and Provider are required.
//
// If Config is provided (ASM mode):
//   - Passes streaming provider + base config to PipelineBuilder
//   - DuplexProviderStage creates the session lazily using TurnState.SystemPrompt
//   - Single long-running pipeline execution
//
// If Config is nil (VAD mode):
//   - No streaming provider or config
//   - Calls PipelineBuilder with nil streaming provider
//   - VAD middleware triggers multiple pipeline executions
func NewDuplexSession(ctx context.Context, cfg *DuplexSessionConfig) (DuplexSession, error) {
	if cfg.PipelineBuilder == nil {
		return nil, fmt.Errorf("pipeline builder is required")
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}

	conversationID := cfg.ConversationID
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	store := cfg.StateStore
	if store == nil {
		store = statestore.NewMemoryStore()
	}

	// Initialize conversation state if it doesn't exist
	if err := initConversationState(ctx, store, cfg, conversationID); err != nil {
		return nil, err
	}

	// For ASM mode, extract the streaming provider (session is created lazily by pipeline)
	var streamProvider providers.StreamInputSupport
	if cfg.Config != nil {
		// ASM mode: provider must support streaming
		var ok bool
		streamProvider, ok = cfg.Provider.(providers.StreamInputSupport)
		if !ok {
			return nil, fmt.Errorf("provider must implement StreamInputSupport for ASM mode")
		}
		// Note: Session is NOT created here — DuplexProviderStage creates it
		// lazily using TurnState.SystemPrompt (published by PromptAssemblyStage
		// and TemplateStage).
	}

	// Build pipeline with streaming provider + config (provider creates session lazily)
	builtPipeline, err := cfg.PipelineBuilder(ctx, cfg.Provider, streamProvider, cfg.Config, conversationID, store)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	// Initialize variables
	vars := make(map[string]string)
	for k, v := range cfg.Variables {
		vars[k] = v
	}

	// Create streaming channels
	// stageInput feeds stage.StreamElement to the pipeline
	// streamOutput receives providers.StreamChunk for the external API
	stageInput := make(chan stage.StreamElement, streamBufferSize)
	streamOutput := make(chan providers.StreamChunk, streamBufferSize)

	// The session keeps ctx's values — tool calls see the host's caller
	// identity and policy, as a text turn's do (#2690) — but owns its own
	// cancellation, so it outlives the call that opened it.
	sessionCtx, sessionCancel := context.WithCancel(context.WithoutCancel(ctx))

	sess := &duplexSession{
		id:                conversationID,
		store:             store,
		pipeline:          builtPipeline,
		provider:          cfg.Provider,
		streamConfig:      cfg.Config,
		pipelineRunsTools: cfg.PipelineRunsTools,
		toolRegistry:      cfg.ToolRegistry,
		asyncToolChecker:  cfg.AsyncToolChecker,
		caller:            cfg.Caller,
		onTurnComplete:    cfg.OnTurnComplete,
		variables:         vars,
		stageInput:        stageInput,
		streamOutput:      streamOutput,
		sessionCtx:        sessionCtx,
		sessionCancel:     sessionCancel,
		pipelineDone:      make(chan struct{}),
	}

	return sess, nil
}

// ID returns the unique session identifier.
func (s *duplexSession) ID() string {
	return s.id
}

// SendChunk sends a chunk to the session via the Pipeline.
// Converts providers.StreamChunk to stage.StreamElement at the boundary.
func (s *duplexSession) SendChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if chunk == nil {
		return fmt.Errorf("chunk cannot be nil")
	}

	// Start Pipeline execution on first chunk using session-level context
	s.executionMu.Lock()
	if !s.executionStarted {
		s.executionStarted = true
		s.executionMu.Unlock()

		// Start Pipeline execution in background using the session-level context
		// rather than the first SendChunk's context, so the pipeline outlives any
		// single caller.
		go s.executePipeline(s.sessionCtx)
	} else {
		s.executionMu.Unlock()
	}

	// Convert StreamChunk to StreamElement at the boundary
	elem := streamChunkToStreamElement(chunk)
	if chunk.Source != "" {
		elem.WithSource(chunk.Source)
	}

	return s.sendInput(ctx, elem)
}

// sendInput delivers elem to the pipeline, or returns the session-closed error
// once Close has run. A send blocked on a full channel is woken by Close
// canceling the session context, so it never holds Close off.
func (s *duplexSession) sendInput(ctx context.Context, elem stage.StreamElement) error {
	s.inputMu.RLock()
	defer s.inputMu.RUnlock()
	if s.inputClosed {
		return errors.New(errSessionClosed)
	}
	select {
	case s.stageInput <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.sessionCtx.Done():
		return errors.New(errSessionClosed)
	}
}

// SendText is a convenience method for sending text.
func (s *duplexSession) SendText(ctx context.Context, text string) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	chunk := &providers.StreamChunk{
		Content: text,
	}
	return s.SendChunk(ctx, chunk)
}

// SendFrame sends an image frame to the session for realtime video scenarios.
// This is a convenience method that wraps SendChunk with proper image formatting.
func (s *duplexSession) SendFrame(ctx context.Context, frame *ImageFrame) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if frame == nil || len(frame.Data) == 0 {
		return fmt.Errorf("frame data is required")
	}

	chunk := &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:     frame.Data,
			MIMEType: frame.MIMEType,
			Width:    frame.Width,
			Height:   frame.Height,
			FrameNum: frame.FrameNum,
		},
	}
	return s.SendChunk(ctx, chunk)
}

// SendVideoChunk sends a video chunk to the session for encoded video streaming.
// This is a convenience method that wraps SendChunk with proper video formatting.
func (s *duplexSession) SendVideoChunk(ctx context.Context, vchunk *VideoChunk) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if vchunk == nil || len(vchunk.Data) == 0 {
		return fmt.Errorf("video chunk data is required")
	}

	chunk := &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:       vchunk.Data,
			MIMEType:   vchunk.MIMEType,
			Width:      vchunk.Width,
			Height:     vchunk.Height,
			FrameNum:   int64(vchunk.ChunkIndex),
			IsKeyFrame: vchunk.IsKeyFrame,
		},
	}
	return s.SendChunk(ctx, chunk)
}

// executePipeline runs the stage pipeline with streaming input/output.
// This is called once when the first chunk is received.
// Converts stage.StreamElement output to providers.StreamChunk at the boundary.
//
// Tool calls are run by the pipeline (see handleToolCalls).
//
//nolint:gocognit // Complexity is acceptable for pipeline orchestration with tool handling
func (s *duplexSession) executePipeline(ctx context.Context) {
	defer close(s.pipelineDone)
	defer close(s.streamOutput)

	// Start the stage pipeline with our input channel
	stageOutput, err := s.pipeline.Execute(ctx, s.stageInput)
	if err != nil {
		finishReason := "error"
		select {
		case s.streamOutput <- providers.StreamChunk{
			Error:        err,
			FinishReason: &finishReason,
		}:
		default:
		}
		return
	}

	// Forward stage output to streamOutput, converting StreamElement to StreamChunk.
	for {
		select {
		case <-ctx.Done():
			errChunk := providers.StreamChunk{Error: ctx.Err()}
			select {
			case s.streamOutput <- errChunk:
			default:
			}
			return
		case elem, ok := <-stageOutput:
			if !ok {
				// Stage pipeline finished
				return
			}

			action, actionErr := streaming.ProcessResponseElement(&elem, "duplexSession")
			s.recordElementCalls(&elem)

			switch action {
			case streaming.ResponseActionToolCalls:
				if err := s.handleToolCalls(ctx, &elem); err != nil {
					logger.Error("duplexSession: tool handling failed", "error", err)
					errChunk := providers.StreamChunk{Error: err}
					select {
					case s.streamOutput <- errChunk:
					case <-ctx.Done():
					}
					return
				}

			case streaming.ResponseActionError:
				// Forward the error to the caller and keep reading: the stage
				// reports some errors (a tool turn a limit stopped) and goes on,
				// and one it ends on closes its output, which ends this loop.
				// Returning here would leave the stage blocked on an output
				// nobody reads, and the caller's later input unanswered.
				if !s.forwardChunk(ctx, providers.StreamChunk{Error: actionErr}) {
					return
				}

			case streaming.ResponseActionContinue, streaming.ResponseActionComplete:
				// The caller's own typed turn: recorded and saved upstream, and
				// not output to hand back to the caller who sent it.
				if elem.Source == stage.TextInputTurnSource {
					continue
				}
				if action == streaming.ResponseActionComplete {
					s.turnsCompleted.Add(1)
				}
				s.noteTurnComplete(ctx, &elem)
				chunk := streamElementToStreamChunk(&elem)
				if !s.forwardChunk(ctx, chunk) {
					return
				}
			}
		}
	}
}

// handleToolCalls handles an assistant turn that made tool calls.
//
//   - The pipeline runs the calls (PipelineRunsTools): its DuplexProviderStage
//     answers them and surfaces those held for approval on an element of their
//     own, so the turn is not passed on and nothing runs here.
//   - The pipeline does not, and the session has a registry: the session runs
//     them (runToolCallsInSession).
//   - Neither: the turn is forwarded for the caller to answer.
func (s *duplexSession) handleToolCalls(ctx context.Context, elem *stage.StreamElement) error {
	switch {
	case s.pipelineRunsTools:
		return nil
	case s.toolRegistry != nil:
		return s.runToolCallsInSession(ctx, elem)
	}
	if !s.forwardChunk(ctx, streamElementToStreamChunk(elem)) {
		return ctx.Err()
	}
	return nil
}

// Response returns the channel of streamed output chunks. A chunk with Error
// set does not end it; it closes when the pipeline ends.
//
// The caller MUST drain this channel for the lifetime of the session, even if it
// consumes results by other means (e.g. OnClientTool handlers) and discards the
// streamed content — spin up a goroutine that ranges over it. The pipeline's
// output stage sends here; if the channel is never read it fills and back-pressures
// the whole pipeline to a silent halt.
//
// As a safety net, a session whose Response() is never called does NOT stall: the
// forwarding loop drops output chunks once the buffer fills (logging a one-time
// warning) rather than blocking. Because of that, call Response() before the
// session produces output — chunks emitted before the first call may be dropped.
func (s *duplexSession) Response() <-chan providers.StreamChunk {
	s.responseTaken.Store(true)
	return s.streamOutput
}

// outputBlockWarnThreshold is how long a taken Response() consumer may stall the
// output send before forwardChunk warns that it is not draining fast enough. A
// var (not const) so tests can lower it; not meant to change at runtime.
var outputBlockWarnThreshold = 3 * time.Second

// forwardChunk delivers one chunk to the response channel and reports whether the
// forwarding loop should keep running (false means the session context is done).
//
// If Response() has been taken, the send blocks (bounded by ctx) so no output is
// lost, but a consumer that stalls past outputBlockWarnThreshold is warned once.
// If Response() has NOT been taken, nothing will ever read the channel, so the
// send must never block: it buffers while there is room and then drops, keeping
// the pipeline alive instead of deadlocking it (issue #1638). Dropping warns once.
func (s *duplexSession) forwardChunk(ctx context.Context, chunk providers.StreamChunk) bool {
	if !s.responseTaken.Load() {
		select {
		case <-ctx.Done():
			return false
		case s.streamOutput <- chunk:
			return true
		default:
			if s.droppedChunks.Add(1) == 1 {
				logger.Warn("duplexSession: Response() was never read; dropping streamed output. " +
					"Call Response() and drain the channel (even a discard goroutine) to receive output.")
			}
			return true
		}
	}

	// Fast path: room in the buffer, or the session is already done.
	select {
	case <-ctx.Done():
		return false
	case s.streamOutput <- chunk:
		return true
	default:
	}

	// The consumer is behind. Wait for room, but warn once past the threshold so a
	// stalled reader is a diagnostic rather than a silent hang.
	timer := time.NewTimer(outputBlockWarnThreshold)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case s.streamOutput <- chunk:
			return true
		case <-timer.C:
			if !s.warnedSlowConsumer.Swap(true) {
				logger.Warn("duplexSession: Response() consumer is not draining fast enough; " +
					"the output stage is blocked. Read Response() promptly to avoid stalling the pipeline.")
			}
		}
	}
}

// DefaultDrainTimeout is the maximum time to wait for pipeline completion during Drain.
const DefaultDrainTimeout = 30 * time.Second

// drainPlayoutGrace bounds how long Drain waits for a graceful finish before
// cutting off output-audio playout on close. Turn state is flushed by the drain
// signal well within this window; the remainder is just the pacing stage playing
// buffered audio at realtime, which a closing session should not wait on.
const drainPlayoutGrace = 750 * time.Millisecond

// Drain gracefully stops the session by sending an EndOfStream signal to the
// pipeline and waiting for it to finish processing. If the context expires
// before the pipeline completes, Drain falls back to a hard Close.
func (s *duplexSession) Drain(ctx context.Context) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closeMu.Unlock()

	// A session that never received a chunk never started executePipeline, so
	// pipelineDone will never be closed and there is nothing to drain. Waiting
	// would block for the full timeout and then report a spurious failure.
	s.executionMu.Lock()
	started := s.executionStarted
	s.executionMu.Unlock()
	if !started {
		return s.Close()
	}

	// Send EndOfStream so the pipeline knows input is done. AllResponsesReceived
	// marks this as a graceful drain (not an end-of-turn boundary), so a streaming
	// provider stage closes its session immediately instead of waiting out its
	// final-response timeout — otherwise draining a live voice session blocks ~30s.
	if err := s.sendInput(ctx, stage.StreamElement{
		EndOfStream: true,
		Meta:        stage.ElementMetadata{AllResponsesReceived: true},
	}); err != nil {
		// The context expired before the send, or Close won the race: fall
		// back to a hard close.
		return s.Close()
	}

	// Wait for the pipeline to complete, but not for the output pacing stage to
	// play out its buffered audio at realtime — the drain signal already flushed
	// turn state, and on close there is no point spending seconds playing audio
	// nobody will hear. After a short grace, cut it off (Close cancels the session
	// context, which stops pacing immediately). A genuine hang still errors at the
	// caller's timeout.
	graceTimer := time.NewTimer(drainPlayoutGrace)
	defer graceTimer.Stop()
	select {
	case <-s.pipelineDone:
		// Pipeline finished gracefully — now close.
		return s.Close()
	case <-graceTimer.C:
		// Buffered audio still draining; state is flushed, so close promptly.
		return s.Close()
	case <-ctx.Done():
		// Caller's outer timeout — force close.
		_ = s.Close()
		return fmt.Errorf("drain timed out: %w", ctx.Err())
	}
}

// Close ends the session.
func (s *duplexSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	// Cancel first: it wakes any send blocked on a full input channel, so the
	// write lock below is not held off by it.
	s.sessionCancel()
	s.inputMu.Lock()
	s.inputClosed = true
	close(s.stageInput)
	s.inputMu.Unlock()

	// executePipeline closes streamOutput and pipelineDone, but it starts on
	// the first send. A session closed before any send must close them here,
	// or Done() and ranging over Response() block forever (#2579). Marking
	// execution started keeps a racing first send from starting it after.
	s.executionMu.Lock()
	if !s.executionStarted {
		s.executionStarted = true
		close(s.streamOutput)
		close(s.pipelineDone)
	}
	s.executionMu.Unlock()
	// Note: DuplexProviderStage manages the provider session and closes it when pipeline completes

	return nil
}

// Done returns a channel that's closed when the session ends.
// The channel is initialized once on first call; subsequent calls return the same channel.
// Done monitors the session's done signal rather than draining streamOutput, so it
// does not compete with Response() for output chunks.
func (s *duplexSession) Done() <-chan struct{} {
	s.doneOnce.Do(func() {
		s.doneCh = make(chan struct{})
		go func() {
			// Wait for the pipeline to finish by monitoring streamOutput closure
			// without consuming any elements. This avoids stealing chunks from
			// consumers reading via Response().
			<-s.pipelineDone
			close(s.doneCh)
		}()
	})
	return s.doneCh
}

// Error returns any error from the session.
// In Pipeline mode, errors are sent as chunks in the response stream.
func (s *duplexSession) Error() error {
	return nil
}

// SubmitToolResults sends resolved/rejected tool results back into the
// duplex pipeline so they flow to the provider via ToolResponseSupport.
func (s *duplexSession) SubmitToolResults(ctx context.Context, responses []providers.ToolResponse) error {
	return s.SubmitToolResultMessages(ctx, responses, nil)
}

// SubmitToolResultMessages is SubmitToolResults that records results, one
// per response and in the same order, in the history in place of the result
// messages it would build from the responses (so a result keeps its error
// and error type). A result without a name is given the call's. With no
// results, the messages are built from the responses.
func (s *duplexSession) SubmitToolResultMessages(
	ctx context.Context, responses []providers.ToolResponse, results []types.MessageToolResult,
) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	result := &streaming.ToolExecutionResult{
		ProviderResponses: responses,
		ResultMessages:    make([]types.Message, 0, len(responses)),
	}

	for i, resp := range responses {
		// The response carries only the call ID. The result message joins
		// the conversation history, and a provider that replays history keyed
		// by name (unary Gemini's functionResponse.name) rejects an empty one.
		name := s.takeCallName(resp.ToolCallID)
		var toolResult types.MessageToolResult
		if i < len(results) {
			toolResult = results[i]
			if toolResult.Name == "" {
				toolResult.Name = name
			}
		} else {
			toolResult = toolResultOf(resp, name)
		}
		result.ResultMessages = append(result.ResultMessages, types.NewToolResultMessage(toolResult))
	}

	return s.sendInput(ctx, streaming.BuildToolResponseElement(result))
}

// toolResultOf is the history's result message for resp, a response to a
// call of tool name.
func toolResultOf(resp providers.ToolResponse, name string) types.MessageToolResult {
	toolResult := types.NewTextToolResult(resp.ToolCallID, name, resp.Result)
	if len(resp.Parts) > 0 {
		// A multimodal result's media joins the history with its text.
		toolResult = types.MessageToolResult{ID: resp.ToolCallID, Name: name, Parts: resp.Parts}
	}
	if resp.IsError {
		toolResult.Error = resp.Result
	}
	return toolResult
}

// copyStreamConfig returns a shallow copy of cfg, or nil.
func copyStreamConfig(cfg *providers.StreamingInputConfig) *providers.StreamingInputConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

// maxRecordedCalls bounds the calls not held that the session keeps for
// SubmitToolResults: past it, the oldest is dropped. Held calls are kept
// until answered.
const maxRecordedCalls = 256

// recordedCall is a call the model made that is not answered yet.
type recordedCall struct {
	call types.MessageToolCall
	// held reports that the call is held for the caller to answer.
	held bool
}

// recordElementCalls keeps the session's record of calls in line with an
// element of the pipeline's output: the calls it makes (their names and
// arguments as sent), then the calls it holds for the caller, so a held
// call's record wins, and drops a call its tool result answers.
func (s *duplexSession) recordElementCalls(elem *stage.StreamElement) {
	if elem.Message != nil {
		s.rememberCalls(elem.Message.ToolCalls)
		if tr := elem.Message.ToolResult; tr != nil {
			s.forgetCall(tr.ID)
		}
	}
	if len(elem.Meta.PendingTools) > 0 {
		s.holdCalls(elem.Meta.PendingTools)
	}
}

// holdCalls records the calls of pending, held for SubmitToolResults, with
// the name each pending entry carries and the arguments as the model sent
// them (encoded from the decoded arguments only for an entry without them).
func (s *duplexSession) holdCalls(pending []tools.PendingToolExecution) {
	calls := make([]types.MessageToolCall, 0, len(pending))
	for i := range pending {
		call := types.MessageToolCall{ID: pending[i].CallID, Name: pending[i].ToolName, Args: pending[i].RawArgs}
		if call.Args == nil && pending[i].Args != nil {
			if args, err := json.Marshal(pending[i].Args); err == nil {
				call.Args = args
			}
		}
		calls = append(calls, call)
	}
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	s.rememberCallsLocked(calls, true)
}

// rememberCalls records each call by its ID, not held, replacing any earlier
// call under the same ID.
func (s *duplexSession) rememberCalls(calls []types.MessageToolCall) {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	s.rememberCallsLocked(calls, false)
}

// rememberCallsLocked records calls, held or not, with callsMu held, and
// drops the oldest calls not held past maxRecordedCalls. A call recorded again
// is fresh: it moves to the end of the order. A held call stays held when an
// element that does not hold it records it again.
func (s *duplexSession) rememberCallsLocked(calls []types.MessageToolCall, held bool) {
	if len(calls) == 0 {
		return
	}
	if s.calls == nil {
		s.calls = make(map[string]recordedCall, len(calls))
	}
	for _, tc := range calls {
		prev, ok := s.calls[tc.ID]
		isHeld := held || (ok && prev.held)
		if ok && !prev.held {
			s.unheld--
			s.dropFromOrderLocked(tc.ID)
		}
		s.calls[tc.ID] = recordedCall{call: tc, held: isHeld}
		if !isHeld {
			s.unheld++
			s.callOrder = append(s.callOrder, tc.ID)
		}
	}
	for s.unheld > maxRecordedCalls && len(s.callOrder) > 0 {
		oldest := s.callOrder[0]
		s.callOrder = s.callOrder[1:]
		delete(s.calls, oldest)
		s.unheld--
	}
}

// dropFromOrderLocked removes id from callOrder, with callsMu held.
func (s *duplexSession) dropFromOrderLocked(id string) {
	for i, recorded := range s.callOrder {
		if recorded == id {
			s.callOrder = append(s.callOrder[:i], s.callOrder[i+1:]...)
			return
		}
	}
}

// forgetCall drops the call recorded under id: it is answered.
func (s *duplexSession) forgetCall(id string) {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	s.forgetCallLocked(id)
}

// forgetCallLocked is forgetCall with callsMu held.
func (s *duplexSession) forgetCallLocked(id string) {
	rc, ok := s.calls[id]
	if !ok {
		return
	}
	delete(s.calls, id)
	if !rc.held {
		s.unheld--
		s.dropFromOrderLocked(id)
	}
}

// ToolCall returns the call the model made under id that is not answered
// yet: the latest response's call when a response reused the ID.
func (s *duplexSession) ToolCall(id string) (types.MessageToolCall, bool) {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	rc, ok := s.calls[id]
	return rc.call, ok
}

// TurnsCompleted returns how many replies the model has completed in the
// session: a reply ends the model's turn.
func (s *duplexSession) TurnsCompleted() int64 {
	return s.turnsCompleted.Load()
}

// takeCallName returns the tool name of the call with id and forgets it, or
// "" if the session never saw it. A call is answered once, so the entry is
// dropped rather than kept for the life of the session.
func (s *duplexSession) takeCallName(id string) string {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	name := s.calls[id].call.Name
	s.forgetCallLocked(id)
	return name
}

// Variables returns a copy of the current variables.
func (s *duplexSession) Variables() map[string]string {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()

	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return vars
}

// SetVar sets a session variable.
//
// A duplex pipeline renders the system prompt once, when the first input
// starts it, and creates the provider session with that render as its system
// instruction. A variable set before the first input is rendered; one set
// afterwards is stored (GetVar still returns it) but never reaches the
// provider, so the first such call is reported at Warn rather than dropped
// silently.
func (s *duplexSession) SetVar(name, value string) {
	s.varsMu.Lock()
	s.variables[name] = value
	s.varsMu.Unlock()

	s.executionMu.Lock()
	started := s.executionStarted
	s.executionMu.Unlock()
	if started && s.warnedLateVar.CompareAndSwap(false, true) {
		logger.Warn("duplexSession: variable set after the session started will not reach the provider; "+
			"the system prompt is rendered once, when the first input arrives — set variables before sending input",
			"variable", name, "session_id", s.id)
	}
}

// GetVar retrieves a session variable.
func (s *duplexSession) GetVar(name string) (string, bool) {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()
	val, ok := s.variables[name]
	return val, ok
}

// Messages implements BaseSession.
func (s *duplexSession) Messages(ctx context.Context) ([]types.Message, error) {
	return loadMessages(ctx, s.store, s.id)
}

// Clear implements BaseSession.
func (s *duplexSession) Clear(ctx context.Context) error {
	return clearSession(ctx, s.store, s.id)
}

// ForkSession implements DuplexSession. The fork reports no completed turns;
// ForkDuplexSession gives it a function to report them to.
func (s *duplexSession) ForkSession(
	ctx context.Context,
	forkID string,
	pipelineBuilder PipelineBuilder,
) (DuplexSession, error) {
	return s.fork(ctx, forkID, pipelineBuilder, nil)
}

// ForkDuplexSession forks s as s.ForkSession does, with onTurnComplete as the
// fork's DuplexSessionConfig.OnTurnComplete. A DuplexSession this package did
// not create is forked with its own ForkSession and a warning: such a session
// must report its fork's completed turns itself (its ForkSession wiring an
// OnTurnComplete), or the fork runs no per-turn lifecycle.
func ForkDuplexSession(
	ctx context.Context,
	s DuplexSession,
	forkID string,
	pipelineBuilder PipelineBuilder,
	onTurnComplete func(ctx context.Context),
) (DuplexSession, error) {
	if ds, ok := s.(*duplexSession); ok {
		return ds.fork(ctx, forkID, pipelineBuilder, onTurnComplete)
	}
	logger.Warn("session: forking a DuplexSession this package did not create; "+
		"its fork runs no per-turn lifecycle unless the session wires OnTurnComplete itself",
		"fork", forkID)
	return s.ForkSession(ctx, forkID, pipelineBuilder)
}

// fork creates the fork, reporting its completed turns to onTurnComplete.
func (s *duplexSession) fork(
	ctx context.Context,
	forkID string,
	pipelineBuilder PipelineBuilder,
	onTurnComplete func(ctx context.Context),
) (DuplexSession, error) {
	if err := forkOrCreate(ctx, s.store, s.id, forkID); err != nil {
		return nil, err
	}

	// Copy variables
	s.varsMu.RLock()
	forkVars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		forkVars[k] = v
	}
	s.varsMu.RUnlock()

	// Create new duplex session with the builder. The fork keeps the
	// parent's context values, so its tool calls run as the same caller.
	return NewDuplexSession(context.WithoutCancel(s.sessionCtx), &DuplexSessionConfig{
		ConversationID:  forkID,
		StateStore:      s.store,
		PipelineBuilder: pipelineBuilder,
		Provider:        s.provider,
		// The parent's streaming config, so a realtime session forks into a
		// realtime session rather than a text pipeline (#2582). A copy, so
		// the fork's config is its own.
		Config:            copyStreamConfig(s.streamConfig),
		ToolRegistry:      s.toolRegistry,
		PipelineRunsTools: s.pipelineRunsTools,
		AsyncToolChecker:  s.asyncToolChecker,
		Caller:            s.caller,
		Variables:         forkVars,
		OnTurnComplete:    onTurnComplete,
	})
}

// applyVideoMetadata extracts video-specific metadata from chunk metadata into VideoData.
func applyVideoMetadata(video *stage.VideoData, metadata map[string]any) {
	if metadata == nil {
		return
	}
	if w, ok := metadata["width"].(int); ok {
		video.Width = w
	}
	if h, ok := metadata["height"].(int); ok {
		video.Height = h
	}
	if ts, ok := metadata["timestamp"].(time.Time); ok {
		video.Timestamp = ts
	}
	if kf, ok := metadata["is_key_frame"].(bool); ok {
		video.IsKeyFrame = kf
	}
	if fn, ok := metadata["frame_num"].(int64); ok {
		video.FrameNum = fn
	}
}

// applyImageMetadata extracts image-specific metadata from chunk metadata into ImageData.
func applyImageMetadata(image *stage.ImageData, metadata map[string]any) {
	if metadata == nil {
		return
	}
	if w, ok := metadata["width"].(int); ok {
		image.Width = w
	}
	if h, ok := metadata["height"].(int); ok {
		image.Height = h
	}
	if ts, ok := metadata["timestamp"].(time.Time); ok {
		image.Timestamp = ts
	}
	if fn, ok := metadata["frame_num"].(int64); ok {
		image.FrameNum = fn
	}
}

// convertMediaData converts a chunk's MediaData to the appropriate stage element field.
func convertMediaData(elem *stage.StreamElement, chunk *providers.StreamChunk) {
	if chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return
	}

	mimeType := chunk.MediaData.MIMEType
	mediaData := chunk.MediaData.Data // already []byte, no copy

	switch {
	case strings.HasPrefix(mimeType, "video/"):
		elem.Video = &stage.VideoData{
			Data:       mediaData,
			MIMEType:   mimeType,
			Width:      chunk.MediaData.Width,
			Height:     chunk.MediaData.Height,
			FrameRate:  chunk.MediaData.FrameRate,
			IsKeyFrame: chunk.MediaData.IsKeyFrame,
			FrameNum:   chunk.MediaData.FrameNum,
		}
		elem.Priority = stage.PriorityHigh
		applyVideoMetadata(elem.Video, chunk.Metadata)

	case strings.HasPrefix(mimeType, "image/"):
		elem.Image = &stage.ImageData{
			Data:     mediaData,
			MIMEType: mimeType,
			Width:    chunk.MediaData.Width,
			Height:   chunk.MediaData.Height,
			FrameNum: chunk.MediaData.FrameNum,
		}
		applyImageMetadata(elem.Image, chunk.Metadata)

	default:
		// Audio handling (default for backwards compatibility)
		sampleRate := chunk.MediaData.SampleRate
		if sampleRate == 0 {
			sampleRate = 16000
		}
		channels := chunk.MediaData.Channels
		if channels == 0 {
			channels = 1
		}
		elem.Audio = &stage.AudioData{
			Samples:    mediaData,
			SampleRate: sampleRate,
			Channels:   channels,
			Format:     stage.AudioFormatPCM16,
		}
	}
}

// streamChunkToStreamElement converts a providers.StreamChunk to stage.StreamElement.
// This is the boundary conversion for input data.
// Routes media based on MIME type: video/* -> VideoData, image/* -> ImageData, audio/* -> AudioData.
func streamChunkToStreamElement(chunk *providers.StreamChunk) stage.StreamElement {
	elem := stage.StreamElement{}

	convertMediaData(&elem, chunk)

	// Convert text content
	if chunk.Delta != "" {
		elem.Text = &chunk.Delta
	} else if chunk.Content != "" {
		elem.Text = &chunk.Content
	}

	// Check for end_of_stream signal in chunk metadata
	if chunk.Metadata != nil {
		if eos, ok := chunk.Metadata["end_of_stream"].(bool); ok && eos {
			elem.EndOfStream = true
		}
	}

	return elem
}

// streamElementToStreamChunk converts a stage.StreamElement to providers.StreamChunk.
// This is the boundary conversion for output data.
func streamElementToStreamChunk(elem *stage.StreamElement) providers.StreamChunk {
	chunk := providers.StreamChunk{}

	// Convert audio data from Audio to MediaData
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		chunk.MediaData = &providers.StreamMediaData{
			Data:       elem.Audio.Samples,
			MIMEType:   "audio/pcm",
			SampleRate: elem.Audio.SampleRate,
			Channels:   elem.Audio.Channels,
		}
	}

	// Convert text content. A synthesized-speech element carries a reference copy
	// of the text it spoke, but that text already reached the caller as streaming
	// deltas — presenting it again as Delta/Content would deliver the reply twice
	// (and the submitted text is not the true spoken text once a provider strips
	// markup). Its audio is delivered via MediaData above; the text is dropped.
	if elem.Text != nil && *elem.Text != "" && !elem.Meta.SynthesizedSpeech {
		chunk.Delta = *elem.Text
		chunk.Content = *elem.Text
	}

	// Surface the faithful spoken text of a synthesized-speech element (post
	// markup-lowering) under a stable key, so a voice UI can caption exactly what
	// was said. Kept OFF Delta/Content (which mean new model output) — mirrors the
	// input_transcription key. Empty when the provider can't report it. (#1657)
	if elem.Meta.SpokenText != "" {
		setChunkMetadata(&chunk, "spoken_text", elem.Meta.SpokenText)
	}

	// Handle errors
	if elem.Error != nil {
		chunk.Error = elem.Error
	}

	// Surface barge-in so the caller can flush buffered playback. VAD mode
	// signals it with an Interrupt control element (AudioTurnStage, forwarded by
	// the TTS stage); a realtime provider's interruption arrives as
	// Meta.Interrupted from DuplexProviderStage.
	if elem.Interrupt || elem.Meta.Interrupted {
		chunk.Interrupted = true
	}

	// Surface the user's input transcription to the caller.
	//
	// In continuous (streaming) realtime voice the DuplexProviderStage
	// materializes the user's spoken turn as a user Message element (Role=user
	// with Content) once the provider reports the transcription and the turn
	// completes. The scenario path instead carries the transcript on
	// elem.Meta.Transcription. Either way, expose it under a single stable key
	// so realtime callers can render the user's turn.
	if transcript := userTranscriptFromElement(elem); transcript != "" {
		setChunkMetadata(&chunk, "input_transcription", transcript)
	}

	// Detect pending client tools (pipeline suspended)
	if len(elem.Meta.PendingTools) > 0 {
		chunk.PendingTools = elem.Meta.PendingTools
		chunk.FinishReason = strPtr("pending_tools")
	}

	// Signal an assistant turn boundary so callers can delimit turns (e.g. a
	// display closing the assistant's line before the next turn). Deliberately NOT
	// via FinishReason: continuous realtime callers loop over Response() and treat
	// a FinishReason as end-of-response and stop reading, which would end the
	// conversation after one turn. A dedicated metadata flag is additive and safe.
	if isAssistantTurnBoundary(elem) {
		setChunkMetadata(&chunk, "assistant_turn_complete", true)
		// Why the turn ended (stop, max_output_tokens, safety, ...), when the
		// provider reported it. A key alongside the flag for the same reason (#2307).
		if fr := elem.Meta.FinishReason; fr != nil && *fr != "" {
			setChunkMetadata(&chunk, "finish_reason", *fr)
		}
	}

	return chunk
}

// noteTurnComplete calls onTurnComplete when elem completes a turn
// (completesTurn) the state store holds, before the element's output reaches
// the caller. A turn whose write failed (Meta.TurnStored false) completes no
// lifecycle.
func (s *duplexSession) noteTurnComplete(ctx context.Context, elem *stage.StreamElement) {
	if s.onTurnComplete == nil || !completesTurn(elem) {
		return
	}
	if stored := elem.Meta.TurnStored; stored != nil && !*stored {
		return
	}
	s.onTurnComplete(ctx)
}

// completesTurn reports whether elem completes a turn of the conversation, as
// a returning Send completes a text turn.
//
//   - Cascaded (VAD) and ingestion sessions: the EndOfTurn boundary the
//     streaming ProviderStage emits once the turn's tool loop has finished,
//     unless it carries FinishReasonInterrupted: a turn whose stream failed or
//     a barge-in cut off after the model had begun keeps its partial reply
//     and closes with that boundary, but did not complete. A turn that failed
//     or was cut off before the model said anything, or holds a call pending,
//     emits no boundary at all.
//   - Realtime sessions: the EndOfStream element carrying an assistant reply
//     that was not cut short (a reply kept when the session or the context
//     ended mid-turn is marked FinishReasonInterrupted). A reply that makes
//     tool calls never gets here: the turn continues with their results
//     (handleToolCalls).
func completesTurn(elem *stage.StreamElement) bool {
	if elem.EndOfTurn {
		fr := elem.Meta.FinishReason
		return fr == nil || *fr != types.FinishReasonInterrupted
	}
	m := elem.Message
	return elem.EndOfStream && m != nil && m.Role == streamRoleAssistant &&
		m.FinishReason != types.FinishReasonInterrupted
}

// isAssistantTurnBoundary reports whether an element closes an assistant turn.
//
// The realtime DuplexProviderStage closes a turn with an EndOfStream element
// carrying the assistant message. The composed VAD topology (and WithIngestion)
// never does: its streaming ProviderStage emits the turn's messages and then an
// EndOfTurn control element, which TTS forwards after the reply's audio (or
// after an unspoken reply cut short by barge-in). EndOfStream must not be
// borrowed for that: the session's response loop treats an EndOfStream without
// content as a fatal empty response. (#2287)
func isAssistantTurnBoundary(elem *stage.StreamElement) bool {
	if elem.EndOfTurn {
		return true
	}
	return elem.EndOfStream && elem.Message != nil && elem.Message.Role == "assistant"
}

// setChunkMetadata sets key on the chunk's metadata, creating the map on
// first use.
func setChunkMetadata(chunk *providers.StreamChunk, key string, value interface{}) {
	if chunk.Metadata == nil {
		chunk.Metadata = make(map[string]interface{})
	}
	chunk.Metadata[key] = value
}

// userTranscriptFromElement extracts the user's input transcription from a
// stage element, whether it arrives as a materialized user Message (continuous
// streaming) or as typed transcription metadata (scenario path). Returns "" when
// the element carries no user transcript.
func userTranscriptFromElement(elem *stage.StreamElement) string {
	// GetContent, not Content: the STT stage builds the user message from a text
	// Part and leaves Content empty, so VAD-mode transcripts never surfaced (#2287).
	if elem.Message != nil && elem.Message.Role == "user" {
		if text := elem.Message.GetContent(); text != "" {
			return text
		}
	}
	if elem.Meta.Transcription != nil {
		return elem.Meta.Transcription.Text
	}
	return ""
}
