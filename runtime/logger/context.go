// Package logger provides structured logging with automatic PII redaction.
package logger

import (
	"context"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

// Context keys for common logging fields.
// These keys are used to store values in context.Context that will be
// automatically extracted and added to log entries.
const (
	// ContextKeyTurnID identifies the current conversation turn.
	ContextKeyTurnID contextKey = "turn_id"

	// ContextKeyScenario identifies the scenario being executed.
	ContextKeyScenario contextKey = "scenario"

	// ContextKeyScenarioVersion identifies the version of the scenario.
	ContextKeyScenarioVersion contextKey = "scenario_version"

	// ContextKeyProvider identifies the LLM provider (e.g., "openai", "anthropic").
	ContextKeyProvider contextKey = "provider"

	// ContextKeyModel identifies the specific model being used.
	ContextKeyModel contextKey = "model"

	// ContextKeyStage identifies the pipeline stage (e.g., "init", "execution", "streaming").
	ContextKeyStage contextKey = "stage"

	// ContextKeySessionID identifies the user session.
	ContextKeySessionID contextKey = "session_id"

	// ContextKeyRequestID identifies the individual request.
	ContextKeyRequestID contextKey = "request_id"

	// ContextKeyCorrelationID is used for distributed tracing.
	ContextKeyCorrelationID contextKey = "correlation_id"

	// ContextKeyEnvironment identifies the deployment environment.
	ContextKeyEnvironment contextKey = "environment"
)

// allContextKeys lists all context keys that should be extracted for logging.
// This is used by the handler to iterate over all possible context values.
var allContextKeys = []contextKey{
	ContextKeyTurnID,
	ContextKeyScenario,
	ContextKeyScenarioVersion,
	ContextKeyProvider,
	ContextKeyModel,
	ContextKeyStage,
	ContextKeySessionID,
	ContextKeyRequestID,
	ContextKeyCorrelationID,
	ContextKeyEnvironment,
}

// WithTurnID returns a new context with the turn ID set.
func WithTurnID(ctx context.Context, turnID string) context.Context {
	return context.WithValue(ctx, ContextKeyTurnID, turnID)
}

// WithScenario returns a new context with the scenario name set.
func WithScenario(ctx context.Context, scenario string) context.Context {
	return context.WithValue(ctx, ContextKeyScenario, scenario)
}

// WithScenarioVersion returns a new context with the scenario version set.
func WithScenarioVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, ContextKeyScenarioVersion, version)
}

// WithProvider returns a new context with the provider name set.
func WithProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, ContextKeyProvider, provider)
}

// WithModel returns a new context with the model name set.
func WithModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ContextKeyModel, model)
}

// WithStage returns a new context with the pipeline stage set.
func WithStage(ctx context.Context, stage string) context.Context {
	return context.WithValue(ctx, ContextKeyStage, stage)
}

// WithSessionID returns a new context with the session ID set.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, ContextKeySessionID, sessionID)
}

// WithRequestID returns a new context with the request ID set.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ContextKeyRequestID, requestID)
}

// WithCorrelationID returns a new context with the correlation ID set.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, ContextKeyCorrelationID, correlationID)
}

// WithEnvironment returns a new context with the environment set.
func WithEnvironment(ctx context.Context, environment string) context.Context {
	return context.WithValue(ctx, ContextKeyEnvironment, environment)
}

// WithLoggingContext returns a new context with multiple logging fields set at once.
// This is a convenience function for setting multiple fields in one call.
// Only non-empty values are set.
func WithLoggingContext(ctx context.Context, fields *LoggingFields) context.Context {
	if fields == nil {
		return ctx
	}
	if fields.TurnID != "" {
		ctx = WithTurnID(ctx, fields.TurnID)
	}
	if fields.Scenario != "" {
		ctx = WithScenario(ctx, fields.Scenario)
	}
	if fields.ScenarioVersion != "" {
		ctx = WithScenarioVersion(ctx, fields.ScenarioVersion)
	}
	if fields.Provider != "" {
		ctx = WithProvider(ctx, fields.Provider)
	}
	if fields.Model != "" {
		ctx = WithModel(ctx, fields.Model)
	}
	if fields.Stage != "" {
		ctx = WithStage(ctx, fields.Stage)
	}
	if fields.SessionID != "" {
		ctx = WithSessionID(ctx, fields.SessionID)
	}
	if fields.RequestID != "" {
		ctx = WithRequestID(ctx, fields.RequestID)
	}
	if fields.CorrelationID != "" {
		ctx = WithCorrelationID(ctx, fields.CorrelationID)
	}
	if fields.Environment != "" {
		ctx = WithEnvironment(ctx, fields.Environment)
	}
	return ctx
}

// LoggingFields holds all standard logging context fields.
// This struct is used with WithLoggingContext for bulk field setting.
type LoggingFields struct {
	TurnID          string
	Scenario        string
	ScenarioVersion string
	Provider        string
	Model           string
	Stage           string
	SessionID       string
	RequestID       string
	CorrelationID   string
	Environment     string
}

// ExtractLoggingFields extracts all logging fields from a context.
// Returns a LoggingFields struct with all values found in the context.
func ExtractLoggingFields(ctx context.Context) LoggingFields {
	fields := LoggingFields{}
	if v := ctx.Value(ContextKeyTurnID); v != nil {
		fields.TurnID, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyScenario); v != nil {
		fields.Scenario, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyScenarioVersion); v != nil {
		fields.ScenarioVersion, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyProvider); v != nil {
		fields.Provider, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyModel); v != nil {
		fields.Model, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyStage); v != nil {
		fields.Stage, _ = v.(string)
	}
	if v := ctx.Value(ContextKeySessionID); v != nil {
		fields.SessionID, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyRequestID); v != nil {
		fields.RequestID, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyCorrelationID); v != nil {
		fields.CorrelationID, _ = v.(string)
	}
	if v := ctx.Value(ContextKeyEnvironment); v != nil {
		fields.Environment, _ = v.(string)
	}
	return fields
}
