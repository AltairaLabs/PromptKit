package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestRecordingProvider is the mock provider, recording the messages of
// every request it is sent.
type requestRecordingProvider struct {
	*mock.Provider
	mu       sync.Mutex
	requests [][]types.Message
}

func (p *requestRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]types.Message(nil), req.Messages...))
	p.mu.Unlock()
	return p.Provider.Predict(ctx, req)
}

func (p *requestRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]types.Message(nil), req.Messages...))
	p.mu.Unlock()
	return p.Provider.PredictStream(ctx, req)
}

func (p *requestRecordingProvider) recorded() [][]types.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]types.Message(nil), p.requests...)
}

// messageRecorder is a passthrough stage that records, in order, the role and
// content of every message that leaves the stage before it: the conversation
// history the downstream stages (state store, recording) see.
type messageRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *messageRecorder) stage() stage.Stage {
	return stage.NewStageFunc("message-recorder", stage.StageTypeTransform,
		func(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
			defer close(out)
			for e := range in {
				if e.Message != nil && e.Message.Content != "" {
					r.mu.Lock()
					r.seen = append(r.seen, e.Message.Role+":"+e.Message.Content)
					r.mu.Unlock()
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
}

func (r *messageRecorder) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// In ASM mode the typed text goes to the realtime session, the caller never
// gets its own text back as model output, and the history keeps the typed
// turns in the order they were sent, each before its reply.
func TestDuplexSession_ASMSendTextIsNotEchoedAndKeepsTurnOrder(t *testing.T) {
	ctx := context.Background()
	streamProvider := mock.NewStreamingProvider("test", "test-model", false).WithAutoRespond("the reply")
	recorder := &messageRecorder{}
	builder := func(_ context.Context, _ providers.Provider, sp providers.StreamInputSupport,
		cfg *providers.StreamingInputConfig, _ string, _ statestore.Store) (*stage.StreamPipeline, error) {
		pc := stage.DefaultPipelineConfig()
		pc.ExecutionTimeout = 0
		return stage.NewPipelineBuilderWithConfig(pc).
			Chain(stage.NewDuplexProviderStage(sp, cfg), recorder.stage()).Build()
	}
	sess, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        streamProvider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: builder,
	})
	require.NoError(t, err)
	defer sess.Close()
	responses := sess.Response()

	var outputs []string
	readReply := func() {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case c, ok := <-responses:
				require.True(t, ok, "response channel closed early")
				require.NoError(t, c.Error)
				if c.Content != "" {
					outputs = append(outputs, c.Content)
				}
				if c.Delta != "" {
					outputs = append(outputs, c.Delta)
				}
				if c.Content == "the reply" || c.Delta == "the reply" {
					return
				}
			case <-deadline:
				t.Fatalf("no reply; caller saw %q", outputs)
			}
		}
	}

	require.NoError(t, sess.SendText(ctx, "first"))
	readReply()
	require.NoError(t, sess.SendText(ctx, "second"))
	readReply()

	assert.NotContains(t, outputs, "first", "the caller must not get its own text back")
	assert.NotContains(t, outputs, "second", "the caller must not get its own text back")
	assert.Eventually(t, func() bool { return len(recorder.messages()) >= 4 }, 2*time.Second, time.Millisecond)
	assert.Equal(t, []string{"user:first", "assistant:the reply", "user:second", "assistant:the reply"},
		recorder.messages())
}
