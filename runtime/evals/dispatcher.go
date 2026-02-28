package evals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// EvalDispatcher controls WHERE evals execute. Implementations decide
// whether evals run in-process, are published to an event bus for
// async processing, or are skipped entirely.
type EvalDispatcher interface {
	// DispatchTurnEvals dispatches turn-level evals.
	// Returns results synchronously (InProc) or nil (Event/NoOp).
	DispatchTurnEvals(
		ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
	) ([]EvalResult, error)

	// DispatchSessionEvals dispatches session-level evals.
	// Returns results synchronously (InProc) or nil (Event/NoOp).
	DispatchSessionEvals(
		ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
	) ([]EvalResult, error)

	// DispatchConversationEvals dispatches conversation-level evals.
	// Returns results synchronously (InProc) or nil (Event/NoOp).
	DispatchConversationEvals(
		ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
	) ([]EvalResult, error)
}

// EventPublisher publishes serialized eval payloads to an event bus.
// PromptKit ships this interface only — platforms provide concrete
// implementations backed by Redis Streams, NATS, Kafka, etc.
type EventPublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
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

// InProcDispatcher runs evals directly via EvalRunner and writes
// results via ResultWriter. Used by Arena (always) and SDK simple
// deployments. Results are returned synchronously.
type InProcDispatcher struct {
	runner       *EvalRunner
	resultWriter ResultWriter
}

// NewInProcDispatcher creates a dispatcher that runs evals in-process.
// The resultWriter may be nil if no result writing is needed.
func NewInProcDispatcher(
	runner *EvalRunner, resultWriter ResultWriter,
) *InProcDispatcher {
	return &InProcDispatcher{
		runner:       runner,
		resultWriter: resultWriter,
	}
}

// DispatchTurnEvals runs turn-level evals in-process.
func (d *InProcDispatcher) DispatchTurnEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	logger.Info("evals: dispatching turn evals", "count", len(defs), "session_id", evalCtx.SessionID)
	results := d.runner.RunTurnEvals(ctx, defs, evalCtx)
	logger.Debug("evals: turn evals completed", "results", len(results), "session_id", evalCtx.SessionID)
	if err := d.writeResults(ctx, results); err != nil {
		return results, err
	}
	return results, nil
}

// DispatchSessionEvals runs session-level evals in-process.
func (d *InProcDispatcher) DispatchSessionEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	logger.Info("evals: dispatching session evals", "count", len(defs), "session_id", evalCtx.SessionID)
	results := d.runner.RunSessionEvals(ctx, defs, evalCtx)
	logger.Debug("evals: session evals completed", "results", len(results), "session_id", evalCtx.SessionID)
	if err := d.writeResults(ctx, results); err != nil {
		return results, err
	}
	return results, nil
}

// DispatchConversationEvals runs conversation-level evals in-process.
func (d *InProcDispatcher) DispatchConversationEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	logger.Info("evals: dispatching conversation evals", "count", len(defs), "session_id", evalCtx.SessionID)
	results := d.runner.RunConversationEvals(ctx, defs, evalCtx)
	logger.Debug("evals: conversation evals completed", "results", len(results), "session_id", evalCtx.SessionID)
	if err := d.writeResults(ctx, results); err != nil {
		return results, err
	}
	return results, nil
}

func (d *InProcDispatcher) writeResults(
	ctx context.Context, results []EvalResult,
) error {
	if d.resultWriter == nil || len(results) == 0 {
		return nil
	}
	if err := d.resultWriter.WriteResults(ctx, results); err != nil {
		logger.Warn("evals: failed to write results", "error", err)
		return err
	}
	return nil
}

// evalEventPayload is the JSON payload published by EventDispatcher.
type evalEventPayload struct {
	Defs    []EvalDef    `json:"defs"`
	EvalCtx *EvalContext `json:"eval_ctx"`
}

// EventDispatcher publishes eval requests to an event bus for async
// processing by an EvalWorker (Pattern B). Returns nil results since
// evals run asynchronously in the worker.
type EventDispatcher struct {
	publisher EventPublisher
}

// NewEventDispatcher creates a dispatcher that publishes to an event bus.
func NewEventDispatcher(publisher EventPublisher) *EventDispatcher {
	return &EventDispatcher{publisher: publisher}
}

// DispatchTurnEvals publishes turn eval request to the event bus.
// Subject: eval.turn.{session_id}
func (d *EventDispatcher) DispatchTurnEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	return nil, d.publish(ctx, "eval.turn", defs, evalCtx)
}

// DispatchSessionEvals publishes session eval request to the event bus.
// Subject: eval.session.{session_id}
func (d *EventDispatcher) DispatchSessionEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	return nil, d.publish(ctx, "eval.session", defs, evalCtx)
}

// DispatchConversationEvals publishes conversation eval request to the event bus.
// Subject: eval.conversation.{session_id}
func (d *EventDispatcher) DispatchConversationEvals(
	ctx context.Context, defs []EvalDef, evalCtx *EvalContext,
) ([]EvalResult, error) {
	return nil, d.publish(ctx, "eval.conversation", defs, evalCtx)
}

func (d *EventDispatcher) publish(
	ctx context.Context,
	prefix string,
	defs []EvalDef,
	evalCtx *EvalContext,
) error {
	payload := evalEventPayload{Defs: defs, EvalCtx: evalCtx}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("evals: failed to marshal eval event", "prefix", prefix, "error", err)
		return fmt.Errorf("marshal eval event: %w", err)
	}
	subject := fmt.Sprintf("%s.%s", prefix, evalCtx.SessionID)
	logger.Info("evals: publishing eval event", "subject", subject, "count", len(defs))
	return d.publisher.Publish(ctx, subject, data)
}

// NoOpDispatcher is used when evals are disabled at the SDK dispatch
// level. Returns nil results with no error. Used when the platform
// handles evals externally (Pattern A) or via EventBusEvalListener
// (Pattern C).
type NoOpDispatcher struct{}

// DispatchTurnEvals is a no-op that returns nil results.
func (d *NoOpDispatcher) DispatchTurnEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	return nil, nil
}

// DispatchSessionEvals is a no-op that returns nil results.
func (d *NoOpDispatcher) DispatchSessionEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	return nil, nil
}

// DispatchConversationEvals is a no-op that returns nil results.
func (d *NoOpDispatcher) DispatchConversationEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	return nil, nil
}
