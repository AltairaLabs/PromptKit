package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// ResultWriter controls WHERE eval results go. Implementations may
// write to Prometheus metrics, message metadata, telemetry spans,
// databases, or external APIs. Platform-specific writers are
// implemented outside PromptKit.
type ResultWriter interface {
	WriteResults(ctx context.Context, results []EvalResult) error
}

// EventSubscriber subscribes to eval events from an event bus.
// PromptKit ships this interface only — platforms provide concrete
// implementations backed by Redis Streams, NATS, Kafka, etc.
type EventSubscriber interface {
	Subscribe(
		ctx context.Context,
		subject string,
		handler func(event []byte) error,
	) error
}

// evalEventPayload is the JSON payload processed by EvalWorker.
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

// WithLogger sets a custom logger for the worker.
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

// Start subscribes to turn and session eval events and processes them.
// It blocks until the context is canceled or a subscription error occurs.
// If either subscription fails, the other is canceled to avoid goroutine leaks.
func (w *EvalWorker) Start(ctx context.Context) error {
	logger.Info("evals: worker starting", "subscriptions", []string{"eval.turn.*", "eval.session.*"})

	// Derive a child context so we can cancel both subscriptions if either fails.
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Store context for handler methods to derive timeouts from.
	w.ctx = subCtx

	turnErr := make(chan error, 1)
	sessErr := make(chan error, 1)

	go func() {
		turnErr <- w.subscriber.Subscribe(
			subCtx, "eval.turn.*", w.handleTurnEvent,
		)
	}()

	go func() {
		sessErr <- w.subscriber.Subscribe(
			subCtx, "eval.session.*", w.handleSessionEvent,
		)
	}()

	select {
	case err := <-turnErr:
		return fmt.Errorf("turn subscription: %w", err)
	case err := <-sessErr:
		return fmt.Errorf("session subscription: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *EvalWorker) handleTurnEvent(event []byte) error {
	logger.Debug("evals: worker received turn event")
	payload, err := w.decode(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(w.ctx, w.handlerTimeout)
	defer cancel()
	results := w.runner.RunTurnEvals(
		ctx, payload.Defs, payload.EvalCtx,
	)
	logger.Debug("evals: worker turn event processed", "results", len(results))
	return w.writeResults(results)
}

func (w *EvalWorker) handleSessionEvent(event []byte) error {
	logger.Debug("evals: worker received session event")
	payload, err := w.decode(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(w.ctx, w.handlerTimeout)
	defer cancel()
	results := w.runner.RunSessionEvals(
		ctx, payload.Defs, payload.EvalCtx,
	)
	logger.Debug("evals: worker session event processed", "results", len(results))
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
