package provider

import (
	"context"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingProvider records the messages the model is actually asked to answer.
//
// The stock mock provider answers regardless of its input, so asserting that a
// turn was produced proves only that the stage ran — not that the transcript
// reached the model. Capturing the request is what distinguishes the two.
type capturingProvider struct {
	*mock.Provider

	mu   sync.Mutex
	seen [][]types.Message
}

func newCapturingProvider() *capturingProvider {
	return &capturingProvider{Provider: mock.NewProvider("capturing", "test-model", false)}
}

func (p *capturingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req.Messages)
	return p.Provider.Predict(ctx, req)
}

func (p *capturingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req.Messages)
	return p.Provider.PredictStream(ctx, req)
}

func (p *capturingProvider) record(msgs []types.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]types.Message, len(msgs))
	copy(cp, msgs)
	p.seen = append(p.seen, cp)
}

// sawText reports whether any captured request carried the given text.
func (p *capturingProvider) sawText(want string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.seen {
		for _, m := range req {
			if m.GetContent() == want {
				return true
			}
			for _, part := range m.Parts {
				if part.Text != nil && *part.Text == want {
					return true
				}
			}
		}
	}
	return false
}

func (s *transcriptSTT) Name() string                      { return "transcript" }
func (s *transcriptSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (s *transcriptSTT) Pricing() *base.PricingDescriptor  { return nil }
func (s *transcriptSTT) Validate() error                   { return nil }
func (s *transcriptSTT) Init(context.Context) error        { return nil }
func (s *transcriptSTT) HealthCheck(context.Context) error { return nil }
func (s *transcriptSTT) Close() error                      { return nil }
func (s *transcriptSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: s.text}, nil
}

// TestProviderStage_StillReceivesMessageElements guards the existing path: the
// fix must add Text handling without disturbing how Message elements arrive.
func TestProviderStage_StillReceivesMessageElements(t *testing.T) {
	provider := newCapturingProvider()

	turnState := core.NewTurnState()
	s := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)

	const content = "an ordinary message element"
	msg := types.Message{Role: "user", Content: content}
	input := make(chan core.StreamElement, 1)
	input <- core.NewMessageElement(&msg)
	close(input)

	output := make(chan core.StreamElement, 32)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // drain
	}

	assert.True(t, provider.sawText(content), "a Message element must still reach the model")
}
