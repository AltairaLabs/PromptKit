// Package telemetry provides OpenTelemetry integration for PromptKit,
// including TracerProvider management and an event-to-span listener.
package telemetry

import (
	"context"

	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// InstrumentationName is the OTel instrumentation scope name.
	InstrumentationName = "github.com/AltairaLabs/PromptKit"

	// InstrumentationVersion is the OTel instrumentation scope version.
	InstrumentationVersion = "1.0.0"
)

// Tracer returns a named tracer from the given TracerProvider.
// If tp is nil the global noop provider is used.
func Tracer(tp trace.TracerProvider) trace.Tracer {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return tp.Tracer(InstrumentationName, trace.WithInstrumentationVersion(InstrumentationVersion))
}

// NewTracerProvider creates a TracerProvider that exports spans via OTLP/HTTP.
// The caller is responsible for calling Shutdown on the returned provider.
func NewTracerProvider(ctx context.Context, endpoint, serviceName string) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			attribute.String("service.name", serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	return tp, nil
}

// SetupPropagation configures the global OTel text-map propagator to handle
// W3C TraceContext, W3C Baggage, and AWS X-Ray trace headers.
func SetupPropagation() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		xray.Propagator{},
	))
}
