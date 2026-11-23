package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// TestProviderStage_NonFatalErrorElementStillCallsModel: an error element a
// stage emits as a non-fatal report (FramesToMessageStage, MediaComposeStage)
// is not a turn failure; the provider still answers the turn.
func TestProviderStage_NonFatalErrorElementStillCallsModel(t *testing.T) {
	ps := NewProviderStage(mock.NewProvider("mock", "m", false), nil, nil, nil)
	in := make(chan core.StreamElement, 2)
	in <- userElement("hi")
	in <- core.NewErrorElement(errors.New("frame 3 could not be decoded"))
	close(in)
	out := make(chan core.StreamElement, 8)

	require.NoError(t, ps.Process(context.Background(), in, out))
	var elems []core.StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	assert.Equal(t, []string{"Mock response from mock model m"}, replyContents(elems), "the model answers the turn")
}

// TestProviderStage_TurnFailureSignalSkipsModel is the other side: the
// pipeline's turn-failure signal stops the model call and is passed on.
func TestProviderStage_TurnFailureSignalSkipsModel(t *testing.T) {
	provider := &stageErrCountingProvider{Provider: mock.NewProvider("mock", "m", false)}
	ps := NewProviderStage(provider, nil, nil, nil)
	in := make(chan core.StreamElement, 2)
	in <- userElement("hi")
	in <- core.NewTurnFailureSignal(core.NewStageError("template", core.StageTypeTransform, core.ErrTemplateUnresolved))
	close(in)
	out := make(chan core.StreamElement, 8)

	require.NoError(t, ps.Process(context.Background(), in, out))
	assert.Zero(t, provider.callCount())
	errs := errorElements(out)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], core.ErrTemplateUnresolved)
}
