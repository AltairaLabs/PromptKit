package hooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type holdingStub struct {
	stubProviderHook
	mode StreamHoldMode
}

func (h *holdingStub) StreamHold() StreamHoldMode { return h.mode }
func (h *holdingStub) JudgePartialReply(context.Context, *ProviderRequest, PartialReply) Decision {
	return Allow
}

// The stream holders are the hooks that hold past a chunk's tail, in order.
func TestRegistry_StreamHolders(t *testing.T) {
	seg := &holdingStub{stubProviderHook{name: "seg"}, StreamHoldSegment}
	turn := &holdingStub{stubProviderHook{name: "turn"}, StreamHoldTurn}
	r := NewRegistry(
		WithProviderHook(&stubProviderHook{name: "plain"}),
		WithProviderHook(turn),
		WithProviderHook(&holdingStub{stubProviderHook{name: "tail"}, StreamHoldTail}),
		WithProviderHook(&holdingStub{stubProviderHook{name: "none"}, StreamHoldNone}),
		WithProviderHook(seg),
	)
	assert.Equal(t, []StreamHolder{turn, seg}, r.StreamHolders())

	var nilReg *Registry
	assert.Nil(t, nilReg.StreamHolders())
}
