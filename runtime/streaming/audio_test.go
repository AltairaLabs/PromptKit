package streaming

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

func TestSendEndOfStream_Success(t *testing.T) {
	inputChan := make(chan stage.StreamElement, 1)

	if err := SendEndOfStream(context.Background(), inputChan); err != nil {
		t.Fatalf("SendEndOfStream = %v, want nil", err)
	}

	elem := <-inputChan
	if !elem.EndOfStream {
		t.Error("expected EndOfStream=true on sent element")
	}
}

func TestSendEndOfStream_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inputChan := make(chan stage.StreamElement) // unbuffered: send blocks

	err := SendEndOfStream(ctx, inputChan)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("SendEndOfStream = %v, want context.Canceled", err)
	}
}
