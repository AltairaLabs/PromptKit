package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emitN is a root stage that emits n text elements tagged with a label, then closes.
func emitN(name, label string, n int) *StageFunc {
	return NewStageFunc(name, StageTypeGenerate,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for i := 0; i < n; i++ {
				e := StreamElement{Text: &label}
				select {
				case out <- e:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
}

func TestPipelineFanInMergesTwoSources(t *testing.T) {
	a := emitN("a", "from-a", 3)
	b := emitN("b", "from-b", 2)
	merge := NewMergeStage("merge", 2)

	p, err := NewPipelineBuilder().
		AddStage(a).AddStage(b).AddStage(merge).
		Merge("merge", "a", "b").
		Build()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := make(chan StreamElement)
	close(in) // roots ignore pipeline input; they generate
	out, err := p.Execute(ctx, in)
	require.NoError(t, err)

	var count, tagged int
	for e := range out {
		count++
		if e.Meta.MergeInputIndex != nil {
			tagged++
		}
	}
	assert.Equal(t, 5, count, "all elements from both sources arrive")
	assert.Equal(t, 5, tagged, "merge stamps MergeInputIndex on every element")
}
