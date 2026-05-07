package base_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStartCapabilitySpan_NameAndAttrs(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := base.StartCapabilitySpan(context.Background(), tp.Tracer("test"), &base.SpanArgs{
		Capability: base.ProviderTypeImage,
		Operation:  "generate",
		Provider:   "imagen",
		Impl:       "imagen",
		Model:      "imagen-3.0",
	})
	span.End()
	_ = ctx

	spans := sr.Ended()
	assert.Len(t, spans, 1)
	assert.Equal(t, "image.generate", spans[0].Name())

	attrs := spans[0].Attributes()
	found := map[string]string{}
	for _, a := range attrs {
		found[string(a.Key)] = a.Value.AsString()
	}
	assert.Equal(t, "imagen", found["provider.name"])
	assert.Equal(t, "imagen", found["provider.impl"])
	assert.Equal(t, "image", found["provider.capability"])
	assert.Equal(t, "imagen-3.0", found["provider.model"])
}
