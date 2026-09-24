package inference

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// inferenceCapability is the CapabilityCallData.Capability value stamped on
// every event Instrument emits, identifying the call as a generic inference
// call (as opposed to tts/stt/embedding/image_gen).
const inferenceCapability = "inference"

// instrumentedProvider wraps a Provider so every Infer call emits telemetry
// through the events.Emitter attached to ctx (see WithEmitter).
type instrumentedProvider struct {
	inner        Provider
	id           string
	providerType string
}

// Instrument wraps p so every Infer call times the request and, when ctx
// carries an events.Emitter (see WithEmitter), publishes an
// inference.call.completed or inference.call.failed event carrying the
// provider id, provider type, model, duration and cost. id identifies the
// configured provider instance (e.g. "hf", "openai-moderation");
// providerType is the vendor type backing it (e.g. "huggingface", "openai").
//
// The wrapped Infer's result is returned unchanged; instrumentation never
// alters the response or error. With no emitter on ctx, Instrument is a
// transparent passthrough. Instrument(nil, ...) returns nil.
func Instrument(p Provider, id, providerType string) Provider {
	if p == nil {
		return nil
	}
	return &instrumentedProvider{inner: p, id: id, providerType: providerType}
}

// Infer implements Provider.
func (p *instrumentedProvider) Infer(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	resp, err := p.inner.Infer(ctx, req)
	duration := time.Since(start)

	model := resp.Model
	if model == "" {
		model = req.Model
	}

	emitter := emitterFromContext(ctx)
	if err != nil {
		emitter.InferenceCallFailedCtx(ctx, &events.InferenceCallFailedData{
			CapabilityCallData: events.CapabilityCallData{
				Provider:   p.id,
				Model:      model,
				Capability: inferenceCapability,
				Source:     p.providerType,
				Duration:   duration,
			},
			Error: err.Error(),
		})
		return resp, err
	}

	emitter.InferenceCallCompletedCtx(ctx, &events.InferenceCallCompletedData{
		CapabilityCallData: events.CapabilityCallData{
			Provider:   p.id,
			Model:      model,
			Capability: inferenceCapability,
			Source:     p.providerType,
			Duration:   duration,
			Cost:       resp.Usage.Cost,
		},
		InputTokens: resp.Usage.InputTokens,
	})

	return resp, nil
}
