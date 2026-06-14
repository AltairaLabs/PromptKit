package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
)

func TestReduce(t *testing.T) {
	outs := []NamedOutput{
		{ID: "structure", Output: map[string]any{"sections": float64(3)}},
		{ID: "citations", Output: map[string]any{"count": float64(12)}},
	}

	t.Run("append", func(t *testing.T) {
		got := reduce(&composition.Reducer{Strategy: composition.ReduceAppend, Into: "metadata"}, outs)
		want := []any{
			map[string]any{"sections": float64(3)},
			map[string]any{"count": float64(12)},
		}
		if !reflectDeepEqual(got, want) {
			t.Errorf("append = %#v, want %#v", got, want)
		}
	})

	t.Run("replace", func(t *testing.T) {
		got := reduce(&composition.Reducer{Strategy: composition.ReduceReplace, Into: "metadata"}, outs)
		if !reflectDeepEqual(got, map[string]any{"count": float64(12)}) {
			t.Errorf("replace = %#v", got)
		}
	})

	t.Run("barrier", func(t *testing.T) {
		got := reduce(&composition.Reducer{Strategy: composition.ReduceBarrier, Into: "metadata"}, outs)
		want := map[string]any{
			"structure": map[string]any{"sections": float64(3)},
			"citations": map[string]any{"count": float64(12)},
		}
		if !reflectDeepEqual(got, want) {
			t.Errorf("barrier = %#v", got)
		}
	})

	t.Run("replace empty", func(t *testing.T) {
		if got := reduce(&composition.Reducer{Strategy: composition.ReduceReplace}, nil); got != nil {
			t.Errorf("replace of empty = %#v, want nil", got)
		}
	})

	t.Run("append empty", func(t *testing.T) {
		got := reduce(&composition.Reducer{Strategy: composition.ReduceAppend}, nil)
		if !reflectDeepEqual(got, []any{}) {
			t.Errorf("append of empty = %#v, want empty slice", got)
		}
	})

	t.Run("barrier empty", func(t *testing.T) {
		got := reduce(&composition.Reducer{Strategy: composition.ReduceBarrier}, nil)
		if !reflectDeepEqual(got, map[string]any{}) {
			t.Errorf("barrier of empty = %#v, want empty map", got)
		}
	})
}
