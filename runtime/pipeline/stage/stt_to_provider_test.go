package stage

import (
	"context"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// TestProviderStage_ReceivesTranscriptText covers a transcript being silently
// dropped between STT and the model.
//
// STTStage's entire output for a completed turn is StreamElement{Text: &text}.
// The shipped VAD topology wires it straight into ProviderStage — see
// sdk/internal/pipeline/builder_vad.go, "Audio -> VAD -> STT -> LLM -> TTS",
// with no stage in between. But accumulateInput collects only elem.Message and
// discards everything else, so the model is asked to answer an empty
// conversation. Nothing errors; the turn is simply empty.
func TestProviderStage_ReceivesTranscriptText(t *testing.T) {
	const transcript = "I'd like a home insurance quote"

	// Chain the two stages exactly as the shipped VAD pipeline does, rather than
	// feeding ProviderStage by hand: the contract under test is that a
	// transcribed turn reaches the model, and either stage may be the one to
	// close the gap.
	sttStage := NewSTTStage(&transcriptSTT{text: transcript}, DefaultSTTStageConfig())

	provider := newCapturingProvider()
	turnState := NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)

	ctx := context.Background()

	sttIn := make(chan StreamElement, 1)
	sttIn <- StreamElement{Audio: &AudioData{
		Samples:    make([]byte, 32000), // 1s @ 16kHz PCM16, over MinAudioBytes
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
	}}
	close(sttIn)

	sttOut := make(chan StreamElement, 8)
	require.NoError(t, sttStage.Process(ctx, sttIn, sttOut))

	provOut := make(chan StreamElement, 32)
	require.NoError(t, providerStage.Process(ctx, sttOut, provOut))
	for range provOut { //nolint:revive // drain
	}

	assert.True(t, provider.sawText(transcript),
		"the model never received the transcript. STTStage emits it on elem.Text, "+
			"and accumulateInput collects only elem.Message, so a VAD-mode turn asks "+
			"the model to answer an empty conversation")
}

// transcriptSTT is a base.STTProvider returning a fixed transcript.
type transcriptSTT struct{ text string }

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

	turnState := NewTurnState()
	s := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)

	const content = "an ordinary message element"
	msg := types.Message{Role: "user", Content: content}
	input := make(chan StreamElement, 1)
	input <- NewMessageElement(&msg)
	close(input)

	output := make(chan StreamElement, 32)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // drain
	}

	assert.True(t, provider.sawText(content), "a Message element must still reach the model")
}
