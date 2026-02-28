package evals

import (
	"context"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Default TTL for session state in the accumulator.
const defaultSessionTTL = 30 * time.Minute

// Default cleanup interval for expired sessions.
const defaultCleanupInterval = 5 * time.Minute

// roleAssistant is the role string for assistant messages.
const roleAssistant = "assistant"

// PackEvalLoader resolves eval definitions for a prompt.
// Implementations are provided by SDK/platform.
type PackEvalLoader interface {
	LoadEvals(promptID string) ([]EvalDef, error)
}

// sessionState holds the accumulated state for a single session.
type sessionState struct {
	mu       sync.Mutex
	messages []types.Message
	promptID string
	lastSeen time.Time
}

// SessionAccumulator accumulates messages per session for eval context building.
type SessionAccumulator struct {
	mu       sync.RWMutex
	sessions map[string]*sessionState
}

// NewSessionAccumulator creates a new SessionAccumulator.
func NewSessionAccumulator() *SessionAccumulator {
	return &SessionAccumulator{
		sessions: make(map[string]*sessionState),
	}
}

// AddMessage adds a message to a session's accumulator.
func (sa *SessionAccumulator) AddMessage(sessionID, promptID, role, content string) {
	sa.mu.Lock()
	state, ok := sa.sessions[sessionID]
	if !ok {
		state = &sessionState{
			promptID: promptID,
		}
		sa.sessions[sessionID] = state
	}
	sa.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	msg := types.Message{Role: role}
	msg.AddTextPart(content)
	state.messages = append(state.messages, msg)
	state.lastSeen = time.Now()
	if promptID != "" {
		state.promptID = promptID
	}
}

// BuildEvalContext builds an EvalContext from the accumulated session state.
func (sa *SessionAccumulator) BuildEvalContext(sessionID string) *EvalContext {
	sa.mu.RLock()
	state, ok := sa.sessions[sessionID]
	sa.mu.RUnlock()

	if !ok {
		return &EvalContext{SessionID: sessionID}
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	// Copy messages
	msgs := make([]types.Message, len(state.messages))
	copy(msgs, state.messages)

	// Determine current output from last assistant message
	var currentOutput string
	turnIndex := 0
	for i := range msgs {
		if msgs[i].Role == roleAssistant {
			turnIndex++
			currentOutput = msgs[i].GetContent()
		}
	}

	return &EvalContext{
		Messages:      msgs,
		TurnIndex:     turnIndex,
		CurrentOutput: currentOutput,
		SessionID:     sessionID,
		PromptID:      state.promptID,
	}
}

// Remove removes a session from the accumulator.
func (sa *SessionAccumulator) Remove(sessionID string) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	delete(sa.sessions, sessionID)
}

// CleanupBefore removes sessions with lastSeen before the cutoff.
// Returns the number of sessions removed.
func (sa *SessionAccumulator) CleanupBefore(cutoff time.Time) int {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	removed := 0
	for id, state := range sa.sessions {
		state.mu.Lock()
		if state.lastSeen.Before(cutoff) {
			delete(sa.sessions, id)
			removed++
		}
		state.mu.Unlock()
	}
	return removed
}

// PromptID returns the prompt ID for a session, or empty string if not found.
func (sa *SessionAccumulator) PromptID(sessionID string) string {
	sa.mu.RLock()
	state, ok := sa.sessions[sessionID]
	sa.mu.RUnlock()
	if !ok {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.promptID
}

// EventBusEvalListenerOption configures an EventBusEvalListener.
type EventBusEvalListenerOption func(*EventBusEvalListener)

// WithTTL sets the session TTL for the listener.
func WithTTL(ttl time.Duration) EventBusEvalListenerOption {
	return func(l *EventBusEvalListener) { l.ttl = ttl }
}

// EventBusEvalListener subscribes to EventBus message events and triggers
// evals automatically (Pattern C). It accumulates messages per session
// and dispatches turn evals on assistant messages, session evals on close.
type EventBusEvalListener struct {
	accumulator  *SessionAccumulator
	dispatcher   EvalDispatcher
	evalLoader   PackEvalLoader
	resultWriter ResultWriter
	ttl          time.Duration
	ctx          context.Context    // lifecycle context for background dispatches
	cancel       context.CancelFunc // cancels ctx
}

// NewEventBusEvalListener creates a listener that subscribes to the bus
// for EventMessageCreated events and runs evals automatically.
// The provided context controls the lifetime of background goroutines;
// pass a long-lived context (e.g. the server's root context) or use Close() to stop.
func NewEventBusEvalListener(
	bus *events.EventBus,
	dispatcher EvalDispatcher,
	evalLoader PackEvalLoader,
	resultWriter ResultWriter,
	opts ...EventBusEvalListenerOption,
) *EventBusEvalListener {
	l := &EventBusEvalListener{
		accumulator:  NewSessionAccumulator(),
		dispatcher:   dispatcher,
		evalLoader:   evalLoader,
		resultWriter: resultWriter,
		ttl:          defaultSessionTTL,
	}
	for _, opt := range opts {
		opt(l)
	}

	// Subscribe to message created events
	bus.Subscribe(events.EventMessageCreated, l.Handle)

	// Start TTL cleanup goroutine with a cancellable context derived from background.
	// The cleanup loop is an internal concern; its lifetime is managed by Close().
	ctx, cancel := context.WithCancel(context.Background())
	l.ctx = ctx
	l.cancel = cancel
	go l.cleanupLoop(ctx)

	return l
}

// Handle is the events.Listener callback for EventMessageCreated events.
func (l *EventBusEvalListener) Handle(event *events.Event) {
	data, ok := event.Data.(*events.MessageCreatedData)
	if !ok {
		return
	}

	sessionID := event.SessionID
	if sessionID == "" {
		return
	}

	logger.Debug("evals: message event received", "session_id", sessionID, "role", data.Role)

	// Accumulate message
	l.accumulator.AddMessage(sessionID, "", data.Role, data.Content)

	// Only trigger turn evals for assistant messages
	if data.Role != roleAssistant {
		return
	}

	// Run turn evals async
	go l.dispatchTurnEvals(sessionID)
}

// CloseSession runs session-complete evals and removes the session.
// The provided context is propagated to eval dispatch and result writing.
func (l *EventBusEvalListener) CloseSession(ctx context.Context, sessionID string) {
	logger.Info("evals: closing session", "session_id", sessionID)
	promptID := l.accumulator.PromptID(sessionID)
	if promptID == "" {
		l.accumulator.Remove(sessionID)
		return
	}

	defs, err := l.evalLoader.LoadEvals(promptID)
	if err != nil {
		logger.Warn("evals: failed to load evals for prompt", "prompt_id", promptID, "error", err)
		l.accumulator.Remove(sessionID)
		return
	}

	evalCtx := l.accumulator.BuildEvalContext(sessionID)

	results, err := l.dispatcher.DispatchSessionEvals(ctx, defs, evalCtx)
	if err != nil {
		logger.Warn("evals: session eval dispatch error", "session_id", sessionID, "error", err)
	}

	if l.resultWriter != nil && len(results) > 0 {
		if err := l.resultWriter.WriteResults(ctx, results); err != nil {
			logger.Warn("evals: result write error", "session_id", sessionID, "error", err)
		}
	}

	l.accumulator.Remove(sessionID)
}

// Accumulator returns the session accumulator for external seeding.
// Use this to set prompt IDs on sessions before messages arrive.
func (l *EventBusEvalListener) Accumulator() *SessionAccumulator {
	return l.accumulator
}

// Close stops the cleanup goroutine.
func (l *EventBusEvalListener) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	return nil
}

// dispatchTurnEvals loads evals and dispatches turn-level evals for a session.
// It uses the listener's lifecycle context so dispatches are canceled on Close().
func (l *EventBusEvalListener) dispatchTurnEvals(sessionID string) {
	promptID := l.accumulator.PromptID(sessionID)
	if promptID == "" {
		return
	}

	logger.Debug("evals: dispatching turn evals from listener", "session_id", sessionID, "prompt_id", promptID)

	defs, err := l.evalLoader.LoadEvals(promptID)
	if err != nil {
		logger.Warn("evals: failed to load evals for prompt", "prompt_id", promptID, "error", err)
		return
	}

	evalCtx := l.accumulator.BuildEvalContext(sessionID)

	results, err := l.dispatcher.DispatchTurnEvals(l.ctx, defs, evalCtx)
	if err != nil {
		logger.Warn("evals: turn eval dispatch error", "session_id", sessionID, "error", err)
	}

	if l.resultWriter != nil && len(results) > 0 {
		if err := l.resultWriter.WriteResults(l.ctx, results); err != nil {
			logger.Warn("evals: result write error", "session_id", sessionID, "error", err)
		}
	}
}

// cleanupLoop periodically removes expired sessions.
func (l *EventBusEvalListener) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-l.ttl)
			removed := l.accumulator.CleanupBefore(cutoff)
			if removed > 0 {
				logger.Debug("evals: cleanup removed expired sessions", "removed", removed)
			}
		}
	}
}
