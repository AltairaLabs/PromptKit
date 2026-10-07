package sdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// slowFirstByteProvider streams like the mock provider, but produces nothing
// until its first byte is due, so an idle timeout shorter than that cancels
// the call before any output.
type slowFirstByteProvider struct {
	*mock.Provider
	firstByte time.Duration
}

func (p *slowFirstByteProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	out := make(chan providers.StreamChunk, 1)
	go func() {
		defer close(out)
		select {
		case <-time.After(p.firstByte):
			out <- providers.StreamChunk{Content: "late", Delta: "late"}
		case <-ctx.Done():
			out <- providers.StreamChunk{Error: ctx.Err()}
		}
	}()
	return out, nil
}

// A turn the idle timeout cancels before the first byte must end in an error
// chunk, never in a ChunkDone that looks like an empty successful turn (#2071).
// It was a race that took the ChunkDone path about half the time, so repeat.
func TestStream_IdleTimeoutBeforeFirstByteEndsInError(t *testing.T) {
	const runs = 40
	packPath := filepath.Join(t.TempDir(), "idle.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(inertProbePackJSON), 0o600))
	for i := 0; i < runs; i++ {
		provider := &slowFirstByteProvider{
			Provider:  mock.NewProvider("slow", "slow-model", false),
			firstByte: 2 * time.Second,
		}
		conv, err := Open(packPath, "chat", WithProvider(provider), WithIdleTimeout(20*time.Millisecond))
		require.NoError(t, err)

		var gotErr error
		var gotDone bool
		for chunk := range conv.Stream(context.Background(), "hi") {
			if chunk.Error != nil {
				gotErr = chunk.Error
			}
			if chunk.Type == ChunkDone {
				gotDone = true
			}
		}
		require.NoError(t, conv.Close())

		require.Falsef(t, gotDone, "run %d: an idle-timed-out turn ended in ChunkDone", i)
		require.Errorf(t, gotErr, "run %d: an idle-timed-out turn ended without an error chunk", i)
	}
}
