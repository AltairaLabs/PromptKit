package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers" // register built-in eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// Reproduction for #2064: a topic_policy guardrail whose classifier times out
// blocks the turn (agent never called, err is nil) but Send returned an EMPTY
// response instead of the validator's configured message. The deny and
// error-immediately paths already carried the message; only the timeout path
// lost it.

const guardrailTimeoutPack = "./testdata/packs/guardrail-timeout.pack.json"

// guardrailTimeoutMessage is the validator "message" declared in
// guardrail-timeout.pack.json, copied verbatim from the topic-policy example
// (sdk/examples/topic-policy/support.pack.json) that #2064 was reported
// against.
const guardrailTimeoutMessage = "I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support."

// countingProvider records how many turns actually reached the agent, so a
// test can assert a guardrail-blocked turn never called it — mirrors
// sdk/examples/topic-policy/main.go's countingProvider.
type countingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *countingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record()
	return p.Provider.Predict(ctx, req)
}

func (p *countingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record()
	return p.Provider.PredictStream(ctx, req)
}

func (p *countingProvider) record() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
}

func (p *countingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func newCountingMockProvider() *countingProvider {
	return &countingProvider{
		Provider: mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Here's what I can tell you about that.")),
	}
}

// immediateDenyClassifier denies every message with no delay. This is the
// first control: it already passed before #2064 was fixed, and must stay
// green.
type immediateDenyClassifier struct{}

func (immediateDenyClassifier) ClassifyTopic(
	_ context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	return classify.TopicResult{Decision: classify.TopicDeny, Raw: "off-topic"}, nil
}

// immediateErrorClassifier fails every classification with no delay. Combined
// with topic_policy's on_error default (deny), this is the second control.
type immediateErrorClassifier struct{}

func (immediateErrorClassifier) ClassifyTopic(
	_ context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	return classify.TopicResult{}, assert.AnError
}

// slowClassifier blocks for d, honoring ctx.Done() so it does not leak a
// goroutine past the caller's deadline. Exactly the reproduction classifier
// from issue #2064.
type slowClassifier struct{ d time.Duration }

func (s slowClassifier) ClassifyTopic(
	ctx context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	select {
	case <-time.After(s.d):
		return classify.TopicResult{Decision: classify.TopicDeny}, nil
	case <-ctx.Done():
		return classify.TopicResult{}, ctx.Err()
	}
}

// TestGuardrailTimeout_DenyImmediately is the first control: an immediate
// TopicDeny must block the turn and return the validator's message. Must stay
// green through the fix.
func TestGuardrailTimeout_DenyImmediately(t *testing.T) {
	provider := newCountingMockProvider()
	conv, err := Open(guardrailTimeoutPack, "support",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithClassifier("topic-control", immediateDenyClassifier{}),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, sendErr := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, sendErr)
	assert.Equal(t, 0, provider.callCount(), "a denied turn must never reach the agent")
	assert.Equal(t, guardrailTimeoutMessage, resp.Text())
}

// TestGuardrailTimeout_ErrorsImmediately is the second control: a classifier
// erroring immediately obeys on_error (default deny) and returns the
// validator's message. Must stay green through the fix.
func TestGuardrailTimeout_ErrorsImmediately(t *testing.T) {
	provider := newCountingMockProvider()
	conv, err := Open(guardrailTimeoutPack, "support",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithClassifier("topic-control", immediateErrorClassifier{}),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, sendErr := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, sendErr)
	assert.Equal(t, 0, provider.callCount(), "an errored classification must never reach the agent")
	assert.Equal(t, guardrailTimeoutMessage, resp.Text())
}

// TestGuardrailTimeout_ClassifierTimesOut is the failing case from #2064: a
// classifier that blocks past the eval's timeout budget, honoring ctx.Done(),
// must still block the turn and carry the validator's message — not an empty
// response. This is a ~30s test (the eval runner's default timeout); it is
// intentionally the only timeout case in this file.
func TestGuardrailTimeout_ClassifierTimesOut(t *testing.T) {
	provider := newCountingMockProvider()
	conv, err := Open(guardrailTimeoutPack, "support",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithClassifier("topic-control", slowClassifier{d: 40 * time.Second}),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, sendErr := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, sendErr)
	assert.Equal(t, 0, provider.callCount(), "a timed-out classification must never reach the agent")
	assert.Equal(t, guardrailTimeoutMessage, resp.Text(),
		"a timed-out enforced guardrail must return the validator's message, not empty text (#2064)")
}
