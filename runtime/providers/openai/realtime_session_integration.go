// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ErrConnectionLost is the error type emitted on the Response channel
// when the WebSocket connection drops mid-session (network failure,
// heartbeat timeout, server-initiated close with a non-graceful code).
// Callers can check for this with errors.Is to distinguish recoverable
// connection loss from clean session termination or server-side errors.
var ErrConnectionLost = errors.New("websocket connection lost")

// Common error messages
const (
	errSessionClosed = "session is closed"
)

// Configuration constants
const (
	responseChannelSize = 10
	heartbeatInterval   = 30 * time.Second
	setupTimeout        = 10 * time.Second

	// receiveLoopBlockWarn is the threshold above which receiveLoop logs that an
	// inbound event blocked while being forwarded to responseCh. receiveLoop is
	// single-threaded and every handler does a blocking send on responseCh, so a
	// slow downstream consumer (e.g. real-time audio pacing) back-pressures here
	// and stalls processing of LATER events — including the
	// input_audio_buffer.speech_started that signals server-side barge-in. A
	// non-trivial block here is direct evidence of that coupling.
	receiveLoopBlockWarn = 100 * time.Millisecond
)

// Ensure RealtimeSession implements StreamInputSession (which now includes the
// out-of-band BargeIn() barge-in signal).
var _ providers.StreamInputSession = (*RealtimeSession)(nil)

// RealtimeSession implements StreamInputSession for OpenAI Realtime API.
type RealtimeSession struct {
	ws     *RealtimeWebSocket
	ctx    context.Context
	cancel context.CancelFunc

	// emitCh is where receiveLoop's handlers write response chunks; the shared
	// StreamPump drains it (through an unbounded queue) into Response(). The
	// session owns and closes emitCh; the pump owns Response(). This decouples
	// socket reading from the downstream consumer so control events (barge-in)
	// are processed promptly. See providers.StreamPump.
	emitCh chan providers.StreamChunk

	// StreamPump provides Response(), BargeIn(), and the barge-in audio drop,
	// shared identically across providers. Embedded so those methods promote.
	*providers.StreamPump

	// responseActive is guarded by recvMu (handleMessage and
	// InterruptResponse). It tracks whether a model response is
	// in progress (response.created..response.done), which distinguishes real
	// barge-in (user spoke over the agent) from a normal turn start. The audio-
	// drop state lives on the StreamPump (Dropping/Barge/ClearDrop).
	responseActive bool
	// pendingRequest is the response.create sent and not yet answered;
	// waitingRequests are those asked for while a response was in progress,
	// sent in order once nothing is; speechTurn is set while the caller
	// speaks and the server will start the response to it; announceGens are
	// the replies an Announce asked for (realtime_response_queue.go). Guarded
	// by recvMu.
	pendingRequest  *responseRequest
	waitingRequests []*responseRequest
	speechTurn      bool
	speechSeq       int64
	announceGens    []int64
	// speechGrace overrides speechResponseGrace (tests).
	speechGrace time.Duration

	// playout estimates the delivered audio the caller is still hearing, so
	// speech after response.done still barges in. now is its clock (time.Now
	// unless a test injects one). truncates tracks the
	// conversation.item.truncate events awaiting the server's answer, so a
	// rejected one is logged instead of failing the turn
	// (realtime_playout.go). Guarded by recvMu.
	playout   *audio.Playout
	now       func() time.Time
	truncates truncateTracker
	// cancels tracks the response.cancel events InterruptResponse sent, so
	// one the server rejects (the response had ended) is not a session error.
	cancels truncateTracker
	// cancelGen is the generation of a response InterruptResponse named
	// before the server created it: it is canceled when it is created
	// (realtime_interrupt.go). A new turn clears it.
	cancelGen int64
	// emitClosed is set once the receive loop has closed emitCh: nothing may
	// be emitted after it.
	emitClosed bool

	// recvMu is held while a server event is handled and while
	// InterruptResponse runs, so the state above, otherwise the receive
	// goroutine's alone, is never touched by two goroutines at once.
	recvMu sync.Mutex

	// gen numbers the session's responses (StreamChunk.Generation): one per
	// response, from response.created; respGens maps a response ID to its
	// number, for the last maxRespGens responses (a canceled response's
	// events can trail in after the next began). Guarded by recvMu.
	gen       int64
	respGens  map[string]int64
	respOrder []string

	errCh   chan error
	mu      sync.Mutex
	closed  bool
	eventID atomic.Int64

	// Configuration stored for the session
	config RealtimeSessionConfig

	// newLoader makes the media loader for one tool round (nil: a loader with
	// no media storage); providerID names the provider in its logs.
	newLoader  func() *providers.MediaLoader
	providerID string

	// Cost tracking (can be overridden via provider config)
	inputCostPer1K  float64
	outputCostPer1K float64

	// Session state
	sessionInfo *SessionInfo // Received from session.created

	// Tracks function-call item IDs already emitted on responseCh so that the
	// GA dispatcher doesn't double-emit when both response.function_call_arguments.done
	// and response.output_item.done arrive for the same call.
	emittedToolCalls sync.Map

	// toolMediaItems holds the IDs of the user message items the session
	// created to carry tool media (realtime_tool_media.go); toolMediaSeq
	// numbers them. A transcript of such an item is not the caller's turn.
	toolMediaItems sync.Map
	toolMediaSeq   atomic.Int64
}

// realtimeSessionOpts carries internal/test-only construction overrides for a
// RealtimeSession. Production callers use NewRealtimeSession (zero opts); tests
// inject a fake WebSocket endpoint to exercise the receive/back-pressure
// behavior without the real API.
type realtimeSessionOpts struct {
	// endpoint overrides RealtimeAPIEndpoint when non-empty.
	endpoint string
	// dialHeaders, when set, authenticates each handshake in place of apiKey.
	dialHeaders func(context.Context) (http.Header, error)
	// now, when set, is the session's clock for the playout estimate.
	now func() time.Time
	// mediaLoader, when set, makes the loader a tool round's media is read
	// through (the provider's, with its media storage); providerID names the
	// provider in what the session logs about that media.
	mediaLoader func() *providers.MediaLoader
	providerID  string
}

// NewRealtimeSession creates a new OpenAI Realtime streaming session.
func NewRealtimeSession(ctx context.Context, apiKey string, config *RealtimeSessionConfig) (*RealtimeSession, error) {
	return newRealtimeSession(ctx, apiKey, config, realtimeSessionOpts{})
}

// newRealtimeSession is NewRealtimeSession with internal construction overrides.
func newRealtimeSession(
	ctx context.Context,
	apiKey string,
	config *RealtimeSessionConfig,
	opts realtimeSessionOpts,
) (*RealtimeSession, error) {
	if config == nil {
		defaultConfig := DefaultRealtimeSessionConfig()
		config = &defaultConfig
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	// Create WebSocket connection
	endpoint := opts.endpoint
	if endpoint == "" {
		endpoint = RealtimeAPIEndpoint
	}
	ws := newRealtimeWebSocketAt(config.Model, apiKey, endpoint, opts.dialHeaders)
	if err := ws.ConnectWithRetry(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// The session owns emitCh (handlers write to it; receiveLoop closes it); the
	// shared pump drains it into Response() and provides barge-in.
	emitCh := make(chan providers.StreamChunk, responseChannelSize)
	session := &RealtimeSession{
		ws:         ws,
		ctx:        sessionCtx,
		cancel:     cancel,
		emitCh:     emitCh,
		StreamPump: providers.NewStreamPump(sessionCtx, emitCh, responseChannelSize),
		errCh:      make(chan error, 1),
		config:     *config,
		now:        opts.now,
		newLoader:  opts.mediaLoader,
		providerID: opts.providerID,
	}

	// Wait for session.created event
	if err := session.waitForSessionCreated(); err != nil {
		_ = ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to receive session.created: %w", err)
	}

	// Send session.update with configuration
	if err := session.sendSessionUpdate(); err != nil {
		_ = ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to send session.update: %w", err)
	}

	// Start heartbeat
	ws.StartHeartbeat(sessionCtx, heartbeatInterval)

	// Start the shared pump (emitCh -> Response()) and the receive loop.
	session.Start()
	go session.receiveLoop()

	return session, nil
}

// waitForSessionCreated waits for the initial session.created event from the server.
func (s *RealtimeSession) waitForSessionCreated() error {
	ctx, cancel := context.WithTimeout(s.ctx, setupTimeout)
	defer cancel()

	logger.Debug("OpenAI Realtime: waiting for session.created event")
	data, err := s.ws.Receive(ctx)
	if err != nil {
		logger.Error("OpenAI Realtime: failed to receive session.created", "error", err)
		return fmt.Errorf("failed to receive session.created: %w", err)
	}
	logger.Debug("OpenAI Realtime: received data from WebSocket", "length", len(data), "data", string(data))

	event, err := ParseServerEvent(data)
	if err != nil {
		// Truncate data for logging to avoid huge error messages
		const maxLogDataLen = 200
		truncatedData := data
		if len(data) > maxLogDataLen {
			truncatedData = data[:maxLogDataLen]
		}
		logger.Error("OpenAI Realtime: failed to parse event", "error", err, "data", string(truncatedData))
		return fmt.Errorf("failed to parse session.created: %w", err)
	}

	created, ok := event.(*SessionCreatedEvent)
	if !ok {
		logger.Error("OpenAI Realtime: unexpected event type", "got", event)
		return fmt.Errorf("expected session.created, got: %T", event)
	}

	s.sessionInfo = &created.Session
	logger.Info("OpenAI Realtime: session created",
		"session_id", created.Session.ID,
		"model", created.Session.Model)

	return nil
}

// sendSessionUpdate sends a session.update event to configure the session.
func (s *RealtimeSession) sendSessionUpdate() error {
	sessionConfig := s.buildSessionConfig()

	event := SessionUpdateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventSessionUpdate,
		},
		Session: sessionConfig,
	}

	if configJSON, err := json.MarshalIndent(event, "", "  "); err == nil {
		logger.Debug("OpenAI Realtime: sending session.update", "config", string(configJSON))
	}

	return s.ws.Send(event)
}

// buildSessionConfig delegates to the pure buildRealtimeSessionConfig; the GA
// config-translation logic lives in realtime_config.go so it can be unit-tested
// without a live session.
func (s *RealtimeSession) buildSessionConfig() SessionConfig {
	return buildRealtimeSessionConfig(s.config)
}

// nextEventID generates a unique event ID.
func (s *RealtimeSession) nextEventID() string {
	return fmt.Sprintf("evt_%d", s.eventID.Add(1))
}

// SendChunk sends an audio chunk to the server.
func (s *RealtimeSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	if chunk == nil || len(chunk.Data) == 0 {
		return nil
	}

	// Base64 encode the audio data
	audioBase64 := base64.StdEncoding.EncodeToString(chunk.Data)

	event := InputAudioBufferAppendEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.append",
		},
		Audio: audioBase64,
	}

	return s.ws.Send(event)
}

// SendText sends a text message and triggers a response.
func (s *RealtimeSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	// Create a user message with text content
	item := ConversationItem{
		Type: typeMessage,
		Role: roleUser,
		Content: []ConversationContent{
			{
				Type: "input_text",
				Text: text,
			},
		},
	}

	createEvent := ConversationItemCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventItemCreate,
		},
		Item: item,
	}

	if err := s.ws.Send(createEvent); err != nil {
		return fmt.Errorf("failed to send text: %w", err)
	}

	// Trigger a response: a new turn once it is sent, which waits for a
	// response in progress to end (requestResponse).
	responseEvent := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventResponseCreate,
		},
	}

	return s.requestResponse(responseEvent, false)
}

// SendSystemContext sends a partial session.update that only modifies
// instructions, leaving codec/voice/VAD untouched. The GA API merges
// partial session.update events; type is included because GA requires
// it on every session.update.
func (s *RealtimeSession) SendSystemContext(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := SessionUpdateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventSessionUpdate,
		},
		Session: SessionConfig{
			Type:         sessionTypeRealtime,
			Instructions: text,
		},
	}

	return s.ws.Send(event)
}

// CommitAudioBuffer commits the current audio buffer for processing.
func (s *RealtimeSession) CommitAudioBuffer() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := InputAudioBufferCommitEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.commit",
		},
	}

	return s.ws.Send(event)
}

// ClearAudioBuffer clears the current audio buffer.
func (s *RealtimeSession) ClearAudioBuffer() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := InputAudioBufferClearEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.clear",
		},
	}

	return s.ws.Send(event)
}

// TriggerResponse manually triggers a response from the model.
func (s *RealtimeSession) TriggerResponse(config *ResponseConfig) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventResponseCreate,
		},
		Response: config,
	}

	return s.requestResponse(event, false)
}

// CancelResponse cancels an in-progress response.
func (s *RealtimeSession) CancelResponse() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := ResponseCancelEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventResponseCancel,
		},
	}

	return s.ws.Send(event)
}

// Response() and BargeIn() are provided by the embedded providers.StreamPump;
// the session feeds it via emitCh and calls Barge() on server-side
// speech_started while a response is in progress or its audio still plays.

// Config returns the session configuration. Callers can use this to
// create a new session with the same settings after a connection loss:
//
//	if errors.Is(session.Error(), openai.ErrConnectionLost) {
//	    newSession, _ := openai.NewRealtimeSession(ctx, apiKey, session.Config())
//	}
//
// Note: server-side conversation state is lost on reconnection —
// this only preserves the client-side configuration.
func (s *RealtimeSession) Config() *RealtimeSessionConfig {
	cfg := s.config
	return &cfg
}

// Done returns a channel that's closed when the session ends.
func (s *RealtimeSession) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Error returns any error that occurred during the session.
func (s *RealtimeSession) Error() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
	}
}

// Close closes the session.
func (s *RealtimeSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	return s.ws.Close()
}

// EndInput signals the end of user input.
//
// In server_vad mode the server auto-commits when it detects end-of-speech
// in the audio buffer and (with create_response: true) auto-creates a
// response. Sending manual input_audio_buffer.commit + response.create from
// the client races with that auto-handling and causes the server to reject
// audio chunks with a cryptic "Invalid 'audio'... got an invalid value"
// error (verified via integration bisection). Callers that want server_vad
// behavior must include trailing silence in the streamed audio so the VAD
// can detect end-of-speech — pumpTTSChunks already does this via
// streamSilenceTail.
//
// In manual turn-control mode (TurnDetection == nil, i.e. vad_disabled),
// the client owns turn boundaries: commit the buffer and trigger a response.
func (s *RealtimeSession) EndInput() {
	if s.config.TurnDetection != nil {
		logger.Debug("OpenAI Realtime: EndInput no-op (server_vad mode — server auto-commits and auto-responds)")
		return
	}

	logger.Debug("OpenAI Realtime: EndInput called (manual turn mode)")

	if err := s.CommitAudioBuffer(); err != nil {
		logger.Error("OpenAI Realtime: EndInput failed to commit audio", "error", err)
		return
	}
	if err := s.TriggerResponse(nil); err != nil {
		logger.Error("OpenAI Realtime: EndInput failed to trigger response", "error", err)
	}
}

// receiveLoop continuously receives and processes messages from the server.
func (s *RealtimeSession) receiveLoop() {
	logger.Debug("OpenAI Realtime: receiveLoop started")
	defer func() {
		logger.Debug("OpenAI Realtime: receiveLoop exiting, draining pump")
		// Signal the pump that no more chunks are coming; it flushes the
		// remaining queue and closes responseCh. Wait for that to finish before
		// canceling the context so a terminal chunk (e.g. connection-lost) is
		// delivered to a reading consumer first. cancel() is idempotent — Close()
		// may have already called it.
		s.recvMu.Lock()
		s.emitClosed = true
		close(s.emitCh)
		s.recvMu.Unlock()
		s.Wait()
		s.cancel()
	}()

	msgCh := make(chan []byte, responseChannelSize)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.ws.ReceiveLoop(s.ctx, msgCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			logger.Debug("OpenAI Realtime: receiveLoop context done")
			return
		case err := <-errCh:
			s.handleReceiveLoopEnd(err)
			return
		case data := <-msgCh:
			// Instrumentation + regression guard for the barge-in path. Handlers
			// hand chunks to the pump (emitCh) rather than sending to responseCh
			// directly, so a slow downstream consumer must NOT block this loop —
			// that's what keeps control events (input_audio_buffer.speech_started)
			// processed promptly. If handleMessage ever blocks here again the
			// decoupling has regressed; log it with the buffer depths so a real
			// run shows where it stalled.
			emitDepth, msgDepth := len(s.emitCh), len(msgCh)
			start := time.Now()
			s.handleMessage(data)
			if blocked := time.Since(start); blocked >= receiveLoopBlockWarn {
				logger.Debug("OpenAI Realtime: receiveLoop stalled forwarding event (back-pressure regression?)",
					"blocked_ms", blocked.Milliseconds(),
					"emitCh_depth", emitDepth,
					"emitCh_cap", cap(s.emitCh),
					"msgCh_depth", msgDepth,
					"msgCh_cap", cap(msgCh))
			}
		}
	}
}

// handleReceiveLoopEnd reports how the WebSocket receive loop ended: a
// connection loss queues a terminal error chunk and propagates the error; a
// clean end (or cancellation) is only logged.
func (s *RealtimeSession) handleReceiveLoopEnd(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		connErr := fmt.Errorf("%w: %w", ErrConnectionLost, err)
		logger.Error("OpenAI Realtime: connection lost", "error", err)

		// Queue a terminal error chunk so callers reading
		// Response() learn the session died with a
		// distinguishable error type. errors.Is(chunk.Error,
		// ErrConnectionLost) returns true. It flows through the
		// pump like any other chunk; the deferred drain guarantees
		// it reaches a reading consumer before responseCh closes.
		reason := finishError
		// ctx-guarded so a blocked send can't outlive the session.
		select {
		case s.emitCh <- providers.StreamChunk{Error: connErr, FinishReason: &reason}:
		case <-s.ctx.Done():
		}

		// Propagate to errCh for callers using Error().
		select {
		case s.errCh <- connErr:
		default:
		}

		// Don't cancel here: the deferred drain closes responseCh
		// and then cancels the context, so Done() fires only after
		// the terminal chunk has been delivered. Callers watching
		// Done() + Error() still get the complete signal.
	} else {
		logger.Debug("OpenAI Realtime: WebSocket receive loop ended", "error", err)
	}
}

// handleMessage processes a single message from the server.
func (s *RealtimeSession) handleMessage(data []byte) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	event, err := ParseServerEvent(data)
	if err != nil {
		logger.Warn("OpenAI Realtime: failed to parse event", "error", err)
		return
	}

	switch e := event.(type) {
	case *ErrorEvent:
		s.handleError(e)
	case *SessionUpdatedEvent:
		s.handleSessionUpdated(e)
	case *ResponseTextDeltaEvent:
		s.handleTextDelta(e)
	case *ResponseTextDoneEvent:
		s.handleTextDone(e)
	case *ResponseAudioDeltaEvent:
		s.handleAudioDelta(e)
	case *ResponseAudioDoneEvent:
		s.handleAudioDone(e)
	case *ResponseAudioTranscriptDeltaEvent:
		s.handleTranscriptDelta(e)
	case *ResponseAudioTranscriptDoneEvent:
		s.handleTranscriptDone(e)
	case *ResponseFunctionCallArgumentsDoneEvent:
		s.handleFunctionCallDone(e)
	case *ResponseOutputItemDoneEvent:
		s.handleOutputItemDone(e)
	case *ResponseCreatedEvent:
		// A model response is now in progress. Used to tell real barge-in
		// (speech_started while the agent is responding) from a normal turn
		// start (speech_started during silence). Clears any stale drop state.
		logger.Debug("OpenAI Realtime: response started", "response_id", e.Response.ID)
		s.startGeneration(e.Response.ID)
		s.responseActive = true
		s.responseCreated(&e.Response)
		s.ClearDrop()
		s.cancelPending()
	case *ResponseDoneEvent:
		s.handleResponseDone(e)
	case *InputAudioBufferSpeechStartedEvent:
		s.handleSpeechStarted(e)
	case *conversationItemTruncatedEvent:
		s.truncates.done(e.ItemID)
	case *InputAudioBufferSpeechStoppedEvent:
		logger.Debug("OpenAI Realtime: speech stopped", "item_id", e.ItemID)
		s.speechStopped()
	case *InputAudioBufferClearedEvent:
		s.endSpeechTurn("the input audio was cleared")
	case *ConversationItemInputAudioTranscriptionCompletedEvent:
		s.handleInputTranscription(e)
	case *RateLimitsUpdatedEvent:
		s.handleRateLimits(e)
	default:
		// Log unknown events at debug level
		var base ServerEvent
		if json.Unmarshal(data, &base) == nil {
			logger.Debug("OpenAI Realtime: unhandled event", "type", base.Type)
		}
	}
}

// startGeneration numbers a new response: its chunks carry the number.
func (s *RealtimeSession) startGeneration(responseID string) {
	s.playoutEstimate().NewResponse()
	s.gen++
	if s.respGens == nil {
		s.respGens = map[string]int64{}
	}
	s.respGens[responseID] = s.gen
	s.respOrder = append(s.respOrder, responseID)
	if len(s.respOrder) > maxRespGens {
		delete(s.respGens, s.respOrder[0])
		s.respOrder = s.respOrder[1:]
	}
}

// maxRespGens bounds the responses whose generation the session remembers.
const maxRespGens = 32

// emit hands a chunk to the pump, tagged with the generation of the response
// it names (Metadata response_id), or the response in progress.
func (s *RealtimeSession) emit(chunk providers.StreamChunk) {
	if chunk.Generation == 0 {
		chunk.Generation = s.gen
		if id, ok := chunk.Metadata[keyResponseID].(string); ok {
			if g, known := s.respGens[id]; known {
				chunk.Generation = g
			}
		}
	}
	if s.isAnnouncement(chunk.Generation) {
		meta := make(map[string]interface{}, len(chunk.Metadata)+1)
		for k, v := range chunk.Metadata {
			meta[k] = v
		}
		meta[providers.MetaKeyAnnouncement] = true
		chunk.Metadata = meta
	}
	s.emitCh <- chunk
}

// playoutEstimate returns the session's playout estimate, creating it on
// the session's clock on first use.
func (s *RealtimeSession) playoutEstimate() *audio.Playout {
	if s.playout == nil {
		s.playout = audio.NewPlayout(s.now)
	}
	return s.playout
}

// handleSpeechStarted handles server VAD's speech_started, which arrives on
// every user utterance. It is barge-in while a response is in progress or
// its delivered audio is still playing; otherwise it is the normal start of
// the user's turn and there is nothing to interrupt.
//
// On barge-in: fire the out-of-band notification (so a consumer paced to
// real-time playback reacts at once rather than after the audio backlog
// drains), drop the reply's queued audio and skip what still arrives, and cut
// the model's copy of the reply to what the caller heard. A reply that had
// finished generating gets no canceled response.done to end it, so the
// interruption is also reported in-band, as an Interrupted chunk.
func (s *RealtimeSession) handleSpeechStarted(e *InputAudioBufferSpeechStartedEvent) {
	s.cancelGen = 0 // the caller's new turn: a pending cancel was the last one's
	if td := s.config.TurnDetection; td != nil && td.CreateResponse {
		// The server starts the response to this speech; nothing waiting is
		// sent meanwhile (realtime_response_queue.go).
		s.speechTurn = true
		s.speechSeq++
	}
	playing := s.playoutEstimate().Playing()
	if !s.responseActive && !playing {
		logger.Debug("OpenAI Realtime: speech started (turn start)", "item_id", e.ItemID)
		return
	}
	logger.Debug("OpenAI Realtime: barge-in", "item_id", e.ItemID,
		"response_active", s.responseActive, "playing", playing)
	s.Barge()
	s.emit(providers.BargeMarker())
	s.truncateHeard()
	if !s.responseActive {
		s.emit(providers.StreamChunk{Interrupted: true})
	}
}

// truncateHeard ends the playout estimate and cuts each audio item the
// caller had not fully heard to what they had heard, with
// conversation.item.truncate.
func (s *RealtimeSession) truncateHeard() {
	for _, cut := range s.playoutEstimate().Stop() {
		event := conversationItemTruncateEvent{
			ClientEvent:  ClientEvent{EventID: s.nextEventID(), Type: realtimeEventItemTruncate},
			ItemID:       cut.ItemID,
			ContentIndex: cut.ContentIndex,
			AudioEndMs:   int(cut.Heard.Milliseconds()),
		}
		s.truncates.sent(event.EventID, cut.ItemID)
		if err := s.ws.Send(event); err != nil {
			logger.Warn("OpenAI Realtime: failed to truncate the interrupted reply", "item_id", cut.ItemID, "error", err)
		}
	}
}

func (s *RealtimeSession) handleError(e *ErrorEvent) {
	// A rejected truncate leaves the model's copy of the reply uncut; the
	// barge-in itself succeeded, so the turn goes on.
	if s.truncates.rejected(e.Error.EventID) {
		logger.Warn("OpenAI Realtime: the server rejected truncating the interrupted reply",
			"code", e.Error.Code, "message", e.Error.Message)
		return
	}
	if s.cancelRejected(e) {
		return
	}
	handled, ended := s.requestRejected(e)
	if handled {
		return
	}
	switch {
	case ended != nil && ended.announce:
		// The spoken message failed: the stage is told, not the session.
		logger.Warn("OpenAI Realtime: the server refused to speak the blocked message",
			"code", e.Error.Code, "message", e.Error.Message)
		s.emit(announcementFailed())
		s.sendNextRequestLocked()
		return
	case ended != nil:
		// The server refused the request for another reason: it is reported,
		// and the next waiting one goes.
		defer s.sendNextRequestLocked()
	case s.speechTurn:
		// Most likely the server's own response to the caller's speech
		// failed: no response is coming for it.
		defer s.endSpeechTurn("a server error during the caller's speech")
	}
	logger.Error("OpenAI Realtime: server error",
		"type", e.Error.Type,
		"code", e.Error.Code,
		"message", e.Error.Message)

	s.emit(providers.StreamChunk{
		Error: fmt.Errorf("server error: %s", e.Error.Message),
	})
}

func (s *RealtimeSession) handleSessionUpdated(e *SessionUpdatedEvent) {
	s.sessionInfo = &e.Session
	logger.Debug("OpenAI Realtime: session updated", "modalities", e.Session.Modalities)
}

// handleTextDelta emits a text-only reply's text as it arrives, on Delta
// only, as transcript deltas are; Content stays for the whole text
// (handleTextDone).
func (s *RealtimeSession) handleTextDelta(e *ResponseTextDeltaEvent) {
	s.emit(providers.StreamChunk{
		Delta: e.Delta,
		Metadata: map[string]interface{}{
			keyItemID:       e.ItemID,
			keyResponseID:   e.ResponseID,
			keyContentIndex: e.ContentIndex,
		},
	})
}

// handleTextDone emits a text-only reply's whole text on Content, marked
// text_done.
func (s *RealtimeSession) handleTextDone(e *ResponseTextDoneEvent) {
	s.emit(providers.StreamChunk{
		Content: e.Text,
		Metadata: map[string]interface{}{
			keyItemID:       e.ItemID,
			keyResponseID:   e.ResponseID,
			keyContentIndex: e.ContentIndex,
			"text_done":     true,
		},
	})
}

// outputSampleRate returns the configured output sample rate, defaulting to 24kHz.
func (s *RealtimeSession) outputSampleRate() int {
	if s.config.OutputSampleRate > 0 {
		return s.config.OutputSampleRate
	}
	return DefaultRealtimeSampleRate
}

func (s *RealtimeSession) handleAudioDelta(e *ResponseAudioDeltaEvent) {
	// After barge-in, skip the interrupted response's still-arriving audio so it
	// isn't queued for playback. Cleared when the next response begins.
	if s.Dropping() {
		return
	}

	rawBytes, err := base64.StdEncoding.DecodeString(e.Delta)
	if err != nil {
		logger.Warn("failed to decode base64 audio from OpenAI Realtime", "error", err)
		return
	}
	s.playoutEstimate().Add(e.ItemID, e.ContentIndex, audio.PCM16Duration(len(rawBytes), s.outputSampleRate(), 1))

	s.emit(providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:       rawBytes,
			MIMEType:   mimeAudioPCM,
			SampleRate: s.outputSampleRate(),
			Channels:   1,
		},
		Metadata: map[string]interface{}{
			keyItemID:       e.ItemID,
			keyResponseID:   e.ResponseID,
			keyContentIndex: e.ContentIndex,
		},
	})
}

func (s *RealtimeSession) handleAudioDone(e *ResponseAudioDoneEvent) {
	if s.Dropping() {
		return
	}
	s.emit(providers.StreamChunk{
		Metadata: map[string]interface{}{
			keyItemID:       e.ItemID,
			keyResponseID:   e.ResponseID,
			keyContentIndex: e.ContentIndex,
			"audio_done":    true,
		},
	})
}

func (s *RealtimeSession) handleTranscriptDelta(e *ResponseAudioTranscriptDeltaEvent) {
	s.emit(providers.StreamChunk{
		Delta: e.Delta, // DuplexProviderStage accumulates this for output transcription
		Metadata: map[string]interface{}{
			keyType:         "output_transcription",
			keyItemID:       e.ItemID,
			keyResponseID:   e.ResponseID,
			keyContentIndex: e.ContentIndex,
		},
	})
}

func (s *RealtimeSession) handleTranscriptDone(e *ResponseAudioTranscriptDoneEvent) {
	s.emit(providers.StreamChunk{
		Metadata: map[string]interface{}{
			"transcript":      e.Transcript,
			keyItemID:         e.ItemID,
			keyResponseID:     e.ResponseID,
			keyContentIndex:   e.ContentIndex,
			"transcript_done": true,
		},
	})
}

func (s *RealtimeSession) handleFunctionCallDone(e *ResponseFunctionCallArgumentsDoneEvent) {
	if s.markToolCallEmitted(e.ItemID) {
		return
	}
	toolCall := types.MessageToolCall{
		ID:   e.CallID,
		Name: e.Name,
		Args: json.RawMessage(e.Arguments),
	}

	s.emit(providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{toolCall},
		Metadata: map[string]interface{}{
			keyItemID:     e.ItemID,
			keyResponseID: e.ResponseID,
		},
	})
}

// handleOutputItemDone extracts function calls carried on response.output_item.done.
// The GA Realtime API surfaces tool calls via this event (with item.type=function_call)
// and may not always emit response.function_call_arguments.done; we dedupe via item_id
// so we don't double-emit when both events arrive.
func (s *RealtimeSession) handleOutputItemDone(e *ResponseOutputItemDoneEvent) {
	if e.Item.Type != typeFunctionCall {
		return
	}
	if s.markToolCallEmitted(e.Item.ID) {
		return
	}

	callID := e.Item.CallID
	if callID == "" {
		callID = e.Item.ID
	}
	toolCall := types.MessageToolCall{
		ID:   callID,
		Name: e.Item.Name,
		Args: json.RawMessage(e.Item.Arguments),
	}

	s.emit(providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{toolCall},
		Metadata: map[string]interface{}{
			keyItemID:     e.Item.ID,
			keyResponseID: e.ResponseID,
		},
	})
}

func (s *RealtimeSession) handleResponseDone(e *ResponseDoneEvent) {
	// The response is over (completed or canceled by barge-in): stop dropping
	// audio. Speech while its delivered audio still plays is still barge-in
	// (see handleSpeechStarted).
	logger.Debug("OpenAI Realtime: response done", "response_id", e.Response.ID, "status", e.Response.Status)
	s.responseActive = false
	s.ClearDrop()

	finishReason := realtimeFinishReason(&e.Response)
	chunk := providers.StreamChunk{
		FinishReason: &finishReason,
		Metadata: map[string]interface{}{
			keyResponseID: e.Response.ID,
		},
	}

	if e.Response.Usage != nil {
		chunk.TokenCount = e.Response.Usage.TotalTokens
		chunk.CostInfo = s.calculateCost(e.Response.Usage)
	}

	s.emit(chunk)
	s.sendNextRequestLocked() // a reply asked for meanwhile can start now
}

func (s *RealtimeSession) handleInputTranscription(e *ConversationItemInputAudioTranscriptionCompletedEvent) {
	if _, ok := s.toolMediaItems.Load(e.ItemID); ok {
		// Tool audio the session sent, transcribed: not something the caller said.
		return
	}
	s.emit(providers.StreamChunk{
		Metadata: map[string]interface{}{
			keyType:         "input_transcription",
			"transcription": e.Transcript, // DuplexProviderStage expects this key
			keyItemID:       e.ItemID,
			keyContentIndex: e.ContentIndex,
			// This fires on conversation.item.input_audio_transcription.completed —
			// a discrete final event carrying the full user transcript. Mark it as
			// final so DuplexProviderStage materializes the user turn immediately
			// instead of waiting for the assistant's EndOfStream (latency fast path).
			"transcription_final": true,
		},
	})
}

func (s *RealtimeSession) handleRateLimits(e *RateLimitsUpdatedEvent) {
	for _, rl := range e.RateLimits {
		logger.Debug("OpenAI Realtime: rate limit updated",
			"name", rl.Name,
			"limit", rl.Limit,
			"remaining", rl.Remaining,
			"reset_seconds", rl.ResetSeconds)
	}
}

// calculateCost delegates to the pure realtimeCostInfo, passing the
// session's per-1K audio-rate overrides (0 ⇒ use the GA defaults). The money
// math lives in realtime_cost.go so it can be unit-tested in isolation.
func (s *RealtimeSession) calculateCost(usage *UsageInfo) *types.CostInfo {
	return realtimeCostInfo(usage, s.inputCostPer1K, s.outputCostPer1K)
}
