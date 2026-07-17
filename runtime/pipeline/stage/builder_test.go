package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeBuilderConnectsAllUpstreams(t *testing.T) {
	a := NewPassthroughStage("a")
	b := NewPassthroughStage("b")
	merge := NewMergeStage("merge", 2)
	p, err := NewPipelineBuilder().
		AddStage(a).AddStage(b).AddStage(merge).
		Merge("merge", "a", "b").
		Build()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, p.findUpstreamStages("merge"))
}

func TestBuildRejectsFanInIntoNonMultiInputStage(t *testing.T) {
	a := NewPassthroughStage("a")
	b := NewPassthroughStage("b")
	sink := NewPassthroughStage("sink") // NOT a MultiInputStage
	_, err := NewPipelineBuilder().
		AddStage(a).AddStage(b).AddStage(sink).
		Merge("sink", "a", "b").
		Build()
	require.ErrorIs(t, err, ErrFanInNotSupported)
}

func TestBuildRejectsDuplicateFanInEdge(t *testing.T) {
	a := NewPassthroughStage("a")
	merge := NewMergeStage("merge", 2)
	_, err := NewPipelineBuilder().
		AddStage(a).AddStage(merge).
		Merge("merge", "a", "a").
		Build()
	require.ErrorIs(t, err, ErrDuplicateFanInEdge)
}
