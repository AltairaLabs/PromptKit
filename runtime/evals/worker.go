package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// ResultWriter controls WHERE eval results go. Implementations may
// write to Prometheus metrics, message metadata, telemetry spans,
// databases, or external APIs. Platform-specific writers are
// implemented outside PromptKit.
type ResultWriter interface {
	WriteResults(ctx context.Context, results []EvalResult) error
}

// EventSubscriber subscribes to eval events from an event bus.
// PromptKit ships only the interface; a platform supplies an implementation
// backed by a message system such as Redis Streams, NATS or Kafka.
type EventSubscriber interface {
	Subscribe(
		ctx context.Context,
		subject string,
		handler func(event []byte) error,
	) error
}

// evalEventPayload is the JSON payload processed by EvalWorker, on every
// subject it subscribes to.
type evalEventPayload struct {
	Defs    []EvalDef    `json:"defs"`
	EvalCtx *EvalContext `json:"eval_ctx"`
}

// Default timeout for eval handler operations.
const defaultHandlerTimeout = 30 * time.Second

// EvalWorker is a reusable worker loop for event-driven eval execution.
// It subscribes to eval events via EventSubscriber, deserializes payloads,
// calls EvalRunner, and writes results via ResultWriter. Platforms wire
// this with their own EventSubscriber and ResultWriter implementations.
type EvalWorker struct {
	runner         *EvalRunner
	subscriber     EventSubscriber
	resultWriter   ResultWriter
	logger         Logger
	handlerTimeout time.Duration
	ctx            context.Context //nolint:containedctx // stored for handler context derivation
}

// Logger is a minimal logging interface for EvalWorker.
type Logger interface {
	Printf(format string, v ...any)
}

// defaultLogger wraps the standard log package.
type defaultLogger struct{}

// Printf logs using the structured logger at warn level.
// Uses Sprintf to bridge the Printf-based Logger interface to structured logging.
func (defaultLogger) Printf(format string, v ...any) {
	logger.Warn(fmt.Sprintf(format, v...))
}

// WorkerOption configures an EvalWorker.
type WorkerOption func(*EvalWorker)

// WithLogger sets a custom logger for the worker. The default logs at warn level
// through the runtime logger.
func WithLogger(l Logger) WorkerOption {
	return func(w *EvalWorker) { w.logger = l }
}

// NewEvalWorker creates a worker that processes eval events.
func NewEvalWorker(
	runner *EvalRunner,
	subscriber EventSubscriber,
	resultWriter ResultWriter,
	opts ...WorkerOption,
) *EvalWorker {
	w := &EvalWorker{
		runner:         runner,
		subscriber:     subscriber,
		resultWriter:   resultWriter,
		logger:         defaultLogger{},
		handlerTimeout: defaultHandlerTimeout,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Subjects an EvalWorker subscribes to. Each event payload is JSON with the
// keys "defs" and "eval_ctx".
const (
	// SubjectTurnEvals carries turn events; the worker runs RunTurnEvals.
	SubjectTurnEvals = "eval.turn.*"
	// SubjectSessionEvals carries session events, published when a session
	// ends (a conversation closed, possibly to be resumed); the worker runs
	// RunSessionEvals.
	SubjectSessionEvals = "eval.session.*"
	// SubjectConversationEvals carries conversation events, published once
	// when the conversation is finished; the worker runs RunConversationEvals.
	SubjectConversationEvals = "eval.conversation.*"
)

// Start subscribes to SubjectTurnEvals, SubjectSessionEvals and
// SubjectConversationEvals and processes the events: turn events run
// RunTurnEvals, session events RunSessionEvals and conversation events
// RunConversationEvals. Start blocks until the context is canceled or a
// subscription returns an error, and it cancels the other subscriptions in
// either case.
func (w *EvalWorker) Start(ctx context.Context) error {
	subs := []struct {
		subject string
		name    string
		handler func([]byte) error
	}{
		{SubjectTurnEvals, "turn", w.handleTurnEvent},
		{SubjectSessionEvals, "session", w.handleSessionEvent},
		{SubjectConversationEvals, "conversation", w.handleConversationEvent},
	}
	subjects := make([]string, len(subs))
	for i, s := range subs {
		subjects[i] = s.subject
	}
	logger.Info("evals: worker starting", "subscriptions", subjects)

	// Derive a child context so we can cancel every subscription if one fails.
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Store context for handler methods to derive timeouts from.
	w.ctx = subCtx

	errs := make(chan error, len(subs))
	for _, s := range subs {
		go func() {
			errs <- fmt.Errorf("%s subscription: %w", s.name, w.subscriber.Subscribe(subCtx, s.subject, s.handler))
		}()
	}

	select {
	case err := <-errs:
		if ctx.Err() != nil {
			// A subscription that ended because the worker was stopped.
			return ctx.Err()
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *EvalWorker) handleTurnEvent(event []byte) error {
	return w.handleEvent("turn", event, w.runner.RunTurnEvals)
}

func (w *EvalWorker) handleSessionEvent(event []byte) error {
	return w.handleEvent("session", event, w.runner.RunSessionEvals)
}

func (w *EvalWorker) handleConversationEvent(event []byte) error {
	return w.handleEvent("conversation", event, w.runner.RunConversationEvals)
}

// handleEvent decodes one event, runs its evals with run and writes the results.
func (w *EvalWorker) handleEvent(
	kind string, event []byte, run func(context.Context, []EvalDef, *EvalContext) []EvalResult,
) error {
	logger.Debug("evals: worker received " + kind + " event")
	payload, err := w.decode(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(w.ctx, w.handlerTimeout)
	defer cancel()
	results := run(ctx, payload.Defs, payload.EvalCtx)
	logger.Debug("evals: worker "+kind+" event processed", "results", len(results))
	return w.writeResults(results)
}

func (w *EvalWorker) decode(event []byte) (*evalEventPayload, error) {
	var payload evalEventPayload
	if err := json.Unmarshal(event, &payload); err != nil {
		w.logger.Printf("failed to decode eval event: %v", err)
		return nil, fmt.Errorf("decode eval event: %w", err)
	}
	return &payload, nil
}

func (w *EvalWorker) writeResults(results []EvalResult) error {
	if w.resultWriter == nil || len(results) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(w.ctx, w.handlerTimeout)
	defer cancel()
	if err := w.resultWriter.WriteResults(
		ctx, results,
	); err != nil {
		w.logger.Printf("failed to write eval results: %v", err)
		return err
	}
	return nil
}
