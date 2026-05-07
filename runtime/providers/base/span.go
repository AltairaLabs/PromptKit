package base

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SpanArgs carries the standard attributes for a capability-scoped provider span.
type SpanArgs struct {
	Capability ProviderType
	Operation  string // "predict" | "synthesize" | "transcribe" | "embed" | "generate"
	Provider   string // base.Provider.Name()
	Impl       string // implementation discriminator (e.g. "openai", "imagen")
	Model      string
}

// StartCapabilitySpan begins a span named "<capability>.<operation>" with the
// standard provider.* attributes. Callers should defer span.End().
func StartCapabilitySpan(ctx context.Context, tracer trace.Tracer, args *SpanArgs) (context.Context, trace.Span) {
	name := string(args.Capability) + "." + args.Operation
	return tracer.Start(ctx, name,
		trace.WithAttributes(
			attribute.String("provider.name", args.Provider),
			attribute.String("provider.impl", args.Impl),
			attribute.String("provider.capability", string(args.Capability)),
			attribute.String("provider.model", args.Model),
		),
	)
}
