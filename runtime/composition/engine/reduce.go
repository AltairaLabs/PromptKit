package engine

import "github.com/AltairaLabs/PromptKit/runtime/composition"

// NamedOutput pairs a parallel branch id with its decoded output, in branch order.
type NamedOutput struct {
	ID     string
	Output any
}

// reduce merges parallel branch outputs per the reducer strategy. The result is
// exposed downstream under the parallel step's output as reduce.Into.
//
//nolint:unused // wired to non-test callers in Task 7
func reduce(r *composition.Reducer, outs []NamedOutput) any {
	switch r.Strategy {
	case composition.ReduceReplace:
		if len(outs) == 0 {
			return nil
		}
		return outs[len(outs)-1].Output
	case composition.ReduceBarrier:
		m := make(map[string]any, len(outs))
		for _, o := range outs {
			m[o.ID] = o.Output
		}
		return m
	default:
		list := make([]any, 0, len(outs))
		for _, o := range outs {
			list = append(list, o.Output)
		}
		return list
	}
}
