package stage

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/require"
)

// TestProviderStageCompletesOnEndOfStreamWithoutChannelClose proves the
// non-streaming ProviderStage treats an EndOfStream control element as
// end-of-input, rather than waiting for the input channel to close.
//
// This is what makes graceful duplex shutdown possible. duplexSession.Drain
// signals end-of-input by sending EndOfStream and then waits for the pipeline
// to finish before closing the channel — so a stage that only reacts to channel
// close can never complete during a drain, and every duplex Close burns the
// full 30s DefaultDrainTimeout before hard-closing. EndOfStream is already the
// established session-over signal honored by AudioTurnStage, ResponseVAD and
// the streaming ProviderStage; this stage was the sole holdout. See #1638.
func TestProviderStageCompletesOnEndOfStreamWithoutChannelClose(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	st := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	// Deliberately left open for the lifetime of the test, exactly as Drain
	// leaves it: the stage must finish on the control element alone.
	input := make(chan StreamElement, 4)
	output := make(chan StreamElement, 16)

	msg := types.Message{Role: "user", Content: "hello"}
	input <- StreamElement{Message: &msg}
	input <- StreamElement{EndOfStream: true}

	done := make(chan error, 1)
	go func() { done <- st.Process(context.Background(), input, output) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ProviderStage did not complete on EndOfStream while the input channel stayed open")
	}
}
