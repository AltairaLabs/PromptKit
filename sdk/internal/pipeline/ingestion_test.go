package pipeline

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildWithIngestionConnectsToAgentChain verifies that a custom
// Config.Ingestion callback can author an upstream sub-graph whose output
// feeds the head of the standard agent chain.
//
// *stage.StreamPipeline has no exported root-stage accessor (only Execute,
// ExecuteSync, and Shutdown are exported — see runtime/pipeline/stage/pipeline.go),
// and the mock provider ignores request content (it always returns a fixed
// response), so a marker stamped on the message can't be observed by asserting
// on result.Response.Content. Instead, the "ingest" stage is a stage.MapStage
// that flips an atomic flag as a side effect of its Process() actually running.
//
// This is deterministic, unlike asserting only on result.Response != nil: if
// Ingestion's output were NOT wired ahead of the agent chain (e.g. the
// builder.Connect(outputNode, ...) call were dropped), both "ingest" and the
// agent head become independent root stages sharing one pipelineInput channel,
// and the single fed element is delivered to whichever root reads it first:
//   - if "ingest" wins the race, ingestInvoked flips true, but the raw
//     (role=user) element becomes the terminal output with no assistant
//     message, so result.Response stays nil — assertion fails.
//   - if the agent head wins the race, it bypasses "ingest" entirely and
//     still produces a normal assistant Response — the old assertion would
//     pass here roughly half the time despite the broken wiring, but
//     ingestInvoked stays false, so the strengthened assertion fails.
//
// Only correct wiring (ingest is the sole root, feeding the agent head)
// guarantees both ingestInvoked == true and result.Response != nil every time.
func TestBuildWithIngestionConnectsToAgentChain(t *testing.T) {
	registry := createTestRegistry("chat")
	mockProvider := mock.NewProvider("test-mock", "test-model", false)

	cfg := &Config{
		PromptRegistry: registry,
		TaskType:       "chat",
		Provider:       mockProvider,
		MaxTokens:      100,
		Temperature:    0.5,
	}
	var ingestInvoked atomic.Bool
	cfg.Ingestion = func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(stage.NewMapStage("ingest", func(elem stage.StreamElement) (stage.StreamElement, error) {
			ingestInvoked.Store(true)
			return elem, nil
		}))
		return "ingest", nil
	}

	p, err := Build(cfg)
	require.NoError(t, err)
	require.NotNil(t, p)

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart("Hello!")
	elem := stage.StreamElement{Message: &userMsg}

	result, err := p.ExecuteSync(context.Background(), elem)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Response)
	assert.True(t, ingestInvoked.Load(), "ingest stage's Process must run before the agent head does")
}

// TestBuildWithIngestionPropagatesCallbackError verifies that an error returned
// by the Ingestion callback aborts the build and is wrapped, not swallowed.
func TestBuildWithIngestionPropagatesCallbackError(t *testing.T) {
	registry := createTestRegistry("chat")
	mockProvider := mock.NewProvider("test-mock", "test-model", false)

	cfg := &Config{
		PromptRegistry: registry,
		TaskType:       "chat",
		Provider:       mockProvider,
	}
	wantErr := errors.New("boom")
	cfg.Ingestion = func(_ *stage.PipelineBuilder) (string, error) {
		return "", wantErr
	}

	p, err := Build(cfg)
	require.Error(t, err)
	assert.Nil(t, p)
	assert.ErrorIs(t, err, wantErr)
}

// TestBuildWithoutIngestionUnaffected pins the non-ingestion path: when
// Config.Ingestion is nil, the agent chain is still rooted at its own head
// (existing behavior, unchanged).
func TestBuildWithoutIngestionUnaffected(t *testing.T) {
	registry := createTestRegistry("chat")
	mockProvider := mock.NewProvider("test-mock", "test-model", false)

	cfg := &Config{
		PromptRegistry: registry,
		TaskType:       "chat",
		Provider:       mockProvider,
	}

	p, err := Build(cfg)
	require.NoError(t, err)
	require.NotNil(t, p)

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart("Hello!")
	elem := stage.StreamElement{Message: &userMsg}

	result, err := p.ExecuteSync(context.Background(), elem)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Response)
}
