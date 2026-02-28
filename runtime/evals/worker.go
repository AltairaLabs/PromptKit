package evals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// EvalWorker is a reusable worker loop for Pattern B event-driven
// eval execution. It subscribes to eval events via EventSubscriber,
// deserializes payloads, calls EvalRunner, and writes results via
// ResultWriter. Platforms wire this with their own EventSubscriber
// and ResultWriter implementations.
type EvalWorker struct {
	runner       *EvalRunner
	subscriber   EventSubscriber
	resultWriter ResultWriter
	logger       Logger
}

// Logger is a minimal logging interface for EvalWorker.
type Logger interface {
	Printf(format string, v ...any)
}

// defaultLogger wraps the standard log package.
type defaultLogger struct{}

// Printf logs using the structured logger at warn level.
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
		runner:       runner,
		subscriber:   subscriber,
		resultWriter: resultWriter,
		logger:       defaultLogger{},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start subscribes to turn and session eval events and processes them.
// It blocks until the context is canceled or a subscription error occurs.
func (w *EvalWorker) Start(ctx context.Context) error {
	logger.Info("evals: worker starting", "subscriptions", []string{"eval.turn.*", "eval.session.*"})
	turnErr := make(chan error, 1)
	sessErr := make(chan error, 1)

	go func() {
		turnErr <- w.subscriber.Subscribe(
			ctx, "eval.turn.*", w.handleTurnEvent,
		)
	}()

	go func() {
		sessErr <- w.subscriber.Subscribe(
			ctx, "eval.session.*", w.handleSessionEvent,
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
	results := w.runner.RunTurnEvals(
		context.Background(), payload.Defs, payload.EvalCtx,
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
	results := w.runner.RunSessionEvals(
		context.Background(), payload.Defs, payload.EvalCtx,
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
	if err := w.resultWriter.WriteResults(
		context.Background(), results,
	); err != nil {
		w.logger.Printf("failed to write eval results: %v", err)
		return err
	}
	return nil
}
