package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithNamePrefix covers building the same stage more than once in one graph.
//
// PipelineBuilder.Build rejects duplicate stage names, so a fan-out that runs
// the same constructors per branch — one audio track per speaker on a two-party
// call, one per camera, one per tenant — needs each branch's stages renamed.
// The SDK has a helper for this in sdk/internal/pipeline, which Go's internal
// rule makes unreachable from anywhere outside sdk/: examples and downstream
// consumers re-derive it, embedding Stage and overriding Name() by hand.
func TestWithNamePrefix(t *testing.T) {
	inner := NewMapStage("audio_turn", func(e StreamElement) (StreamElement, error) {
		return e, nil
	})

	renamed := WithNamePrefix("caller", inner)

	assert.Equal(t, "caller_audio_turn", renamed.Name(),
		"the prefixed stage should carry a unique name")
	assert.Equal(t, "audio_turn", inner.Name(),
		"the original stage must not be mutated — the same instance may be wrapped twice")
}

// TestWithNamePrefixEmptyPrefixIsIdentity pins the single-track case: no prefix
// means the constructors' natural names, and no wrapper.
func TestWithNamePrefixEmptyPrefixIsIdentity(t *testing.T) {
	inner := NewMapStage("stt", func(e StreamElement) (StreamElement, error) { return e, nil })

	assert.Same(t, inner, WithNamePrefix("", inner),
		"an empty prefix should return the stage unchanged, not an extra wrapper")
}

// TestWithNamePrefixDelegatesProcessing pins that renaming is only a rename: the
// wrapped stage must still do its work, or a fan-out would silently drop data.
func TestWithNamePrefixDelegatesProcessing(t *testing.T) {
	const marker = "processed"
	inner := NewMapStage("mapper", func(e StreamElement) (StreamElement, error) {
		text := marker
		e.Text = &text
		return e, nil
	})

	renamed := WithNamePrefix("agent", inner)

	in := make(chan StreamElement, 1)
	in <- StreamElement{}
	close(in)
	out := make(chan StreamElement, 4)

	require.NoError(t, renamed.Process(context.Background(), in, out))

	var got string
	for e := range out {
		if e.Text != nil {
			got = *e.Text
		}
	}
	assert.Equal(t, marker, got, "the renamed stage must delegate to the wrapped stage")
}
