package stage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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

// capsStage is a passthrough stage that declares its formats.
type capsStage struct {
	*PassthroughStage
	in, out Capabilities
}

func (s *capsStage) InputCapabilities() Capabilities  { return s.in }
func (s *capsStage) OutputCapabilities() Capabilities { return s.out }

// Build warns, naming the edge, when connected stages cannot exchange data,
// and builds anyway: the warning is the only signal a host gets before
// runtime. A compatible chain logs no warning.
func TestPipelineBuilder_WarnsOnFormatMismatch(t *testing.T) {
	audio := func(f AudioFormat) Capabilities { return AudioCapabilities([]AudioFormat{f}, nil, nil) }
	build := func(from, to AudioFormat) string {
		var buf bytes.Buffer
		logger.SetOutput(&buf)
		defer logger.SetOutput(nil)
		_, err := NewPipelineBuilder().Chain(
			&capsStage{NewPassthroughStage("stage-a"), AnyCapabilities(), audio(from)},
			&capsStage{NewPassthroughStage("stage-b"), audio(to), AnyCapabilities()},
		).Build()
		require.NoError(t, err)
		return buf.String()
	}

	log := build(AudioFormatOpus, AudioFormatPCM16)
	assert.Equal(t, 1, strings.Count(log, "Pipeline format mismatch"), log)
	for _, want := range []string{"audio format incompatibility", "from_stage=stage-a", "to_stage=stage-b",
		"from_produces=[opus]", "to_accepts=[pcm16]"} {
		assert.Contains(t, log, want)
	}

	assert.NotContains(t, build(AudioFormatPCM16, AudioFormatPCM16), "Pipeline format mismatch")
}
