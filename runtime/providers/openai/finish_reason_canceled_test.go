package openai

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A canceled chat stream reports the finish reason every other provider
// uses for cancellation ("canceled"), so callers match one value.
func TestChatStream_CanceledFinishReasonMatchesOtherProviders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("canceled before the next read", func(t *testing.T) {
		out := make(chan providers.StreamChunk, 1)
		if !sendIfCanceled(ctx, "partial", nil, out) {
			t.Fatal("sendIfCanceled must report a canceled context")
		}
		assertCanceledChunk(t, <-out)
	})

	t.Run("read ended by the cancellation", func(t *testing.T) {
		p := &Provider{}
		out := make(chan providers.StreamChunk, 1)
		p.endIncompleteStream(ctx, nil, "partial", nil, 0, nil, out)
		assertCanceledChunk(t, <-out)
	})
}

func assertCanceledChunk(t *testing.T, chunk providers.StreamChunk) {
	t.Helper()
	if chunk.FinishReason == nil || *chunk.FinishReason != "canceled" {
		t.Fatalf("FinishReason = %v, want \"canceled\"", chunk.FinishReason)
	}
	if chunk.Error == nil {
		t.Fatal("a canceled stream must end on an error chunk")
	}
}
