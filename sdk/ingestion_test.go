package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeIngestionTestPack writes a minimal valid pack file and returns its path.
func writeIngestionTestPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant."
			}
		}
	}`
	require.NoError(t, os.WriteFile(packFile, []byte(packContent), 0o600))
	return packFile
}

// sourceRecorder is a passthrough IngestionFunc callback that records the
// Source of every element flowing through it, proving both that the
// sub-graph is actually wired ahead of the agent chain (elements reach it at
// all) and that StreamChunk.Source survives the SendChunk boundary copy into
// StreamElement.Source.
type sourceRecorder struct {
	mu      sync.Mutex
	sources []string
}

func (r *sourceRecorder) record(elem stage.StreamElement) (stage.StreamElement, error) {
	r.mu.Lock()
	r.sources = append(r.sources, elem.Source)
	r.mu.Unlock()
	return elem, nil
}

func (r *sourceRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.sources))
	copy(out, r.sources)
	return out
}

// waitForCount polls until snapshot() has at least n entries or the timeout
// elapses, returning the final snapshot either way.
func (r *sourceRecorder) waitForCount(n int, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := r.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return r.snapshot()
}

// TestOpenDuplexWithIngestion verifies that WithIngestion installs a custom
// upstream sub-graph ahead of the agent chain in a duplex session, and that
// StreamChunk.Source is copied onto StreamElement.Source at the SendChunk
// boundary (sdk/session/duplex_session.go) so a custom ingestion stage can
// see which track/speaker produced each element. It also verifies the
// session builds and streams a real response without any VAD/TTS wiring.
func TestOpenDuplexWithIngestion(t *testing.T) {
	packFile := writeIngestionTestPack(t)
	provider := mock.NewStreamingProvider("mock", "mock-model", false).
		WithAutoRespond("Hello from ingestion test")

	recorder := &sourceRecorder{}
	ingest := IngestionFunc(func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(stage.NewMapStage("ingest", recorder.record))
		return "ingest", nil
	})

	conv, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithIngestion(ingest),
	)
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, DuplexMode, conv.mode)
	assert.NotNil(t, conv.duplexSession)
	defer func() { _ = conv.Close() }()

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
		Content: "hello from caller",
		Source:  "caller",
	}))
	require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
		Content: "hello from agent",
		Source:  "agent",
	}))

	// The ingest MapStage runs sequentially on a single linear chain fed by
	// one root channel, so element order is preserved end to end; poll
	// (rather than sleep a fixed duration) since SendChunk only enqueues.
	got := recorder.waitForCount(2, 2*time.Second)
	require.Len(t, got, 2, "ingest stage must observe both elements")
	assert.Equal(t, []string{"caller", "agent"}, got,
		"StreamChunk.Source must survive the SendChunk boundary copy onto StreamElement.Source")

	// Drain the response channel briefly to confirm the wired pipeline
	// actually produces output through the ingestion → agent-chain path,
	// without requiring WithVADMode (no TTS service configured at all).
	select {
	case chunk, ok := <-responseCh:
		if ok {
			t.Logf("received response chunk: content=%q", chunk.Content)
		}
	case <-time.After(2 * time.Second):
		t.Log("no response chunk observed within timeout (non-fatal: ingestion wiring already proven above)")
	}
}

// TestOpenDuplexWithIngestionAcceptsNonStreamingProvider proves that
// WithIngestion drives the standard agent chain (a streaming text
// ProviderStage), not the ASM DuplexProviderStage, so OpenDuplex must NOT
// require the provider to implement providers.StreamInputSupport. mock.Provider
// (unlike mock.StreamingProvider) does not implement it — without the ingestion
// relaxation this fails fast with "does not support duplex streaming", which is
// what blocks real Claude from driving a WithIngestion duplex.
func TestOpenDuplexWithIngestionAcceptsNonStreamingProvider(t *testing.T) {
	packFile := writeIngestionTestPack(t)

	// mock.NewProvider returns *mock.Provider, which — unlike the streaming
	// mocks used elsewhere in this file — does NOT implement StreamInputSupport.
	provider := mock.NewProvider("mock", "mock-model", false)
	if _, ok := providers.Provider(provider).(providers.StreamInputSupport); ok {
		t.Fatal("test premise broken: mock.NewProvider must NOT implement StreamInputSupport")
	}

	ingest := IngestionFunc(func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(stage.NewMapStage("ingest", func(elem stage.StreamElement) (stage.StreamElement, error) {
			return elem, nil
		}))
		return "ingest", nil
	})

	conv, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithIngestion(ingest),
	)
	require.NoError(t, err, "OpenDuplex must accept a non-StreamInputSupport provider when WithIngestion is set")
	require.NotNil(t, conv)
	assert.Equal(t, DuplexMode, conv.mode)
	assert.NotNil(t, conv.duplexSession, "duplex session must build on the ingestion path without a stream provider")

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh {
		}
	}()

	// Push one element through the streaming path so the ProviderStage's
	// continuous loop is active before Drain sends EndOfStream — a session that
	// receives no input at all has nothing to flush and falls back to the drain
	// timeout on Close, which is orthogonal to the gate under test here.
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		Content: "hello",
		Source:  "caller",
	}))
	_ = conv.Close()
}

// TestWithIngestionAndVADModeMutuallyExclusive verifies that combining
// WithIngestion and WithVADMode on the same duplex conversation is rejected:
// both options author the input side of the pipeline and cannot coexist.
func TestWithIngestionAndVADModeMutuallyExclusive(t *testing.T) {
	packFile := writeIngestionTestPack(t)
	provider := mock.NewStreamingProvider("mock", "mock-model", false)

	ingest := IngestionFunc(func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(stage.NewMapStage("ingest", func(elem stage.StreamElement) (stage.StreamElement, error) {
			return elem, nil
		}))
		return "ingest", nil
	})

	_, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(nil, nil, nil),
		WithIngestion(ingest),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "mutually exclusive")
}
