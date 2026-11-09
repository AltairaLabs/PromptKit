package pipeline

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordedReply is what the recording provider's underlying mock answers.
const recordedReply = "Mock response from rec model rec-model"

// recordingProvider is the real mock tool provider with every request it
// receives kept, so a test can assert what the built pipeline actually sent:
// the rendered system prompt, the sampling parameters, the history and the
// tools offered.
type recordingProvider struct {
	*mock.ToolProvider

	mu       sync.Mutex
	requests []providers.PredictionRequest
	tools    [][]string
	choices  []string
	sessions []*providers.StreamingInputConfig

	// block makes every call wait for its context to end, for timeout tests.
	block bool
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{ToolProvider: mock.NewToolProvider("rec", "rec-model", false, nil)}
}

func (r *recordingProvider) record(ctx context.Context, req providers.PredictionRequest) error {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	if r.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (r *recordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	if err := r.record(ctx, req); err != nil {
		return providers.PredictionResponse{}, err
	}
	return r.ToolProvider.Predict(ctx, req)
}

func (r *recordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	if err := r.record(ctx, req); err != nil {
		return nil, err
	}
	return r.ToolProvider.PredictStream(ctx, req)
}

func (r *recordingProvider) BuildTooling(descs []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	names := make([]string, 0, len(descs))
	for _, d := range descs {
		names = append(names, d.Name)
	}
	r.mu.Lock()
	r.tools = append(r.tools, names)
	r.mu.Unlock()
	return r.ToolProvider.BuildTooling(descs)
}

func (r *recordingProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	r.mu.Lock()
	r.choices = append(r.choices, choice)
	r.mu.Unlock()
	if err := r.record(ctx, req); err != nil {
		return providers.PredictionResponse{}, nil, err
	}
	return r.ToolProvider.PredictWithTools(ctx, req, tools, choice)
}

func (r *recordingProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (<-chan providers.StreamChunk, error) {
	r.mu.Lock()
	r.choices = append(r.choices, choice)
	r.mu.Unlock()
	if err := r.record(ctx, req); err != nil {
		return nil, err
	}
	return r.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
}

func (r *recordingProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	r.mu.Lock()
	cp := *req
	r.sessions = append(r.sessions, &cp)
	r.mu.Unlock()
	return r.ToolProvider.CreateStreamSession(ctx, req)
}

// onlyRequest returns the single request the provider received.
func (r *recordingProvider) onlyRequest(t *testing.T) providers.PredictionRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.requests, 1, "expected exactly one provider call")
	return r.requests[0]
}

// lastRequest returns the most recent request the provider received.
func (r *recordingProvider) lastRequest(t *testing.T) providers.PredictionRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.requests, "the provider was never called")
	return r.requests[len(r.requests)-1]
}
