package probes

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// probedProvider wraps a providers.Provider and counts Predict / PredictStream
// calls. It is the simplest path to count auto-summarize traffic, because the
// LLMSummarizer calls Provider.Predict directly without going through the
// pipeline ProviderStage (so no provider.call.* events fire for summary work).
//
// The wrapper implements only the base Provider interface — it does NOT
// satisfy ToolSupport. That is intentional: summarization does not use
// tools, and keeping the wrapper minimal avoids accidentally being passed
// where the agent tool-loop needs ToolSupport.
type probedProvider struct {
	inner providers.Provider

	mu      sync.Mutex
	predict int
	stream  int
}

func newProbedProvider(inner providers.Provider) *probedProvider {
	return &probedProvider{inner: inner}
}

// ID delegates to the inner provider.
func (p *probedProvider) ID() string { return p.inner.ID() }

// Model delegates to the inner provider.
func (p *probedProvider) Model() string { return p.inner.Model() }

// SupportsStreaming delegates to the inner provider.
func (p *probedProvider) SupportsStreaming() bool { return p.inner.SupportsStreaming() }

// ShouldIncludeRawOutput delegates to the inner provider.
func (p *probedProvider) ShouldIncludeRawOutput() bool { return p.inner.ShouldIncludeRawOutput() }

// Close delegates to the inner provider.
func (p *probedProvider) Close() error { return p.inner.Close() }

// CalculateCost delegates to the inner provider.
func (p *probedProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return p.inner.CalculateCost(inputTokens, outputTokens, cachedTokens)
}

// Predict counts the call and forwards to the inner provider.
//
// Signature must match providers.Provider; PredictionRequest is passed by
// value there and we cannot deviate.
//
//nolint:gocritic // hugeParam — locked by interface contract.
func (p *probedProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	p.predict++
	p.mu.Unlock()
	return p.inner.Predict(ctx, req)
}

// PredictStream counts the call and forwards to the inner provider.
//
//nolint:gocritic // hugeParam — locked by interface contract.
func (p *probedProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.stream++
	p.mu.Unlock()
	return p.inner.PredictStream(ctx, req)
}

func (p *probedProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.predict, p.stream = 0, 0
}

func (p *probedProvider) predicts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.predict
}

// Compile-time assertion that probedProvider implements providers.Provider.
var _ providers.Provider = (*probedProvider)(nil)
