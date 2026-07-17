package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/require"
)

// recordingTurnProvider records the messages of every Predict call and returns
// a numbered reply, so a test can assert one provider call per turn and that
// later turns carry earlier turns as history. It does NOT implement
// providers.StreamInputSupport — so it also exercises the WithIngestion gate
// relaxation, and proves the standard (streaming) text ProviderStage drives the
// agent regardless of provider streaming support.
type recordingTurnProvider struct {
	mu       sync.Mutex
	requests [][]types.Message
}

func (p *recordingTurnProvider) ID() string                        { return "rec" }
func (p *recordingTurnProvider) Name() string                      { return "rec" }
func (p *recordingTurnProvider) Type() base.ProviderType           { return base.ProviderTypeInference }
func (p *recordingTurnProvider) Pricing() *base.PricingDescriptor  { return nil }
func (p *recordingTurnProvider) Validate() error                   { return nil }
func (p *recordingTurnProvider) Init(context.Context) error        { return nil }
func (p *recordingTurnProvider) HealthCheck(context.Context) error { return nil }
func (p *recordingTurnProvider) Model() string                     { return "rec-model" }
func (p *recordingTurnProvider) SupportsStreaming() bool           { return false }
func (p *recordingTurnProvider) ShouldIncludeRawOutput() bool      { return false }
func (p *recordingTurnProvider) Close() error                      { return nil }
func (p *recordingTurnProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func (p *recordingTurnProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	p.requests = append(p.requests, msgs)
	return providers.PredictionResponse{Content: fmt.Sprintf("reply-%d", len(p.requests))}, nil
}

func (p *recordingTurnProvider) PredictStream(
	_ context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *recordingTurnProvider) turnCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// messagesInTurn returns how many messages the provider saw on the i-th turn.
func (p *recordingTurnProvider) messagesInTurn(i int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests[i])
}

// turnEmitterStage is a minimal ingestion sub-graph: for each text-bearing
// input element it emits a user Message followed by an EndOfTurn control
// element, so the downstream ProviderStage sees one complete turn per input.
// It stands in for the example's per-track resample/VAD/STT sub-chain without
// any audio. Control signals (EndOfStream etc.) pass through so Drain completes.
type turnEmitterStage struct {
	stage.BaseStage
}

func newTurnEmitterStage(name string) *turnEmitterStage {
	return &turnEmitterStage{BaseStage: stage.NewBaseStage(name, stage.StageTypeTransform)}
}

func (s *turnEmitterStage) Process(
	ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement,
) error {
	defer close(out)
	for elem := range in {
		if elem.EndOfStream || elem.EndOfTurn || elem.Interrupt {
			if err := sendPerTurnElem(ctx, out, elem); err != nil {
				return err
			}
			continue
		}
		if elem.Text == nil || *elem.Text == "" {
			continue
		}
		msg := &types.Message{Role: "user"}
		msg.AddTextPart(*elem.Text)
		if err := sendPerTurnElem(ctx, out, stage.StreamElement{Message: msg}); err != nil {
			return err
		}
		if err := sendPerTurnElem(ctx, out, stage.NewEndOfTurnElement()); err != nil {
			return err
		}
	}
	return nil
}

func sendPerTurnElem(ctx context.Context, out chan<- stage.StreamElement, e stage.StreamElement) error {
	select {
	case out <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestOpenDuplexWithIngestion_FiresAgentPerTurn is the SDK-level proof that a
// WithIngestion duplex is a real duplex-AGENT path: the path builds a streaming
// ProviderStage, so each EndOfTurn from the ingestion sub-graph fires the agent
// once, while the session is still open, and history threads across turns.
//
// This is the SDK-wiring counterpart to the runtime's own per-turn coverage
// (runtime/pipeline/stage: TestProviderStage_Streaming_OneReplyPerTurn /
// _ThreadsHistory). Before the streaming wiring, the standard ProviderStage
// drained the whole session and fired ONCE at close — this test would then see
// 0 provider calls until Close, and fail.
func TestOpenDuplexWithIngestion_FiresAgentPerTurn(t *testing.T) {
	packFile := writeIngestionTestPack(t)
	prov := &recordingTurnProvider{}

	ingest := IngestionFunc(func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(newTurnEmitterStage("turn_emitter"))
		return "turn_emitter", nil
	})

	conv, err := OpenDuplex(packFile, "main",
		WithProvider(prov),
		WithSkipSchemaValidation(),
		WithIngestion(ingest),
	)
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh { //nolint:revive // drain so the pipeline output never blocks
		}
	}()

	ctx := context.Background()
	require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{Content: "first turn", Source: "caller"}))
	require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{Content: "second turn", Source: "caller"}))

	// Decisive assertion: both turns fire while the session is still OPEN — no
	// Close() flush. The non-streaming (fire-once-at-close) path shows 0 here.
	require.Eventually(t, func() bool { return prov.turnCount() >= 2 }, 5*time.Second, 10*time.Millisecond,
		"streaming ProviderStage must fire the agent once per EndOfTurn while the session is open")
	require.Equal(t, 2, prov.turnCount(), "exactly one agent turn per EndOfTurn")

	// History threads across turns: turn 2 sees turn 1 (user + assistant reply)
	// plus its own user message, so it carries strictly more messages than turn 1.
	require.Greater(t, prov.messagesInTurn(1), prov.messagesInTurn(0),
		"later turns must carry earlier turns as history")
}
