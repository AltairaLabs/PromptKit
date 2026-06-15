package stage

import (
	"encoding/json"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
)

// CompositionRecorder captures per-step composition execution data for Arena
// observability. Populated during a single engine.Execute (RecordStep may fire
// from parallel-branch goroutines, hence the mutex) and read afterward via
// CompositionMetadata. Reset() is called at the start of each turn.
type CompositionRecorder struct {
	mu       sync.Mutex
	steps    map[string]json.RawMessage
	branches map[string]string
	parallel map[string]string
}

var _ engine.Recorder = (*CompositionRecorder)(nil)

// parallelStatusComplete is the value written to the parallel-status map when a
// parallel step finishes. Named to satisfy the goconst linter.
const parallelStatusComplete = "complete"

// NewCompositionRecorder returns an empty recorder.
func NewCompositionRecorder() *CompositionRecorder {
	return &CompositionRecorder{
		steps:    map[string]json.RawMessage{},
		branches: map[string]string{},
		parallel: map[string]string{},
	}
}

// Reset clears all recorded data (called per turn).
func (r *CompositionRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = map[string]json.RawMessage{}
	r.branches = map[string]string{}
	r.parallel = map[string]string{}
}

// RecordStep implements engine.Recorder.
func (r *CompositionRecorder) RecordStep(id string, out any) {
	raw, _ := json.Marshal(out)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps[id] = raw
}

// RecordBranch implements engine.Recorder.
func (r *CompositionRecorder) RecordBranch(id, taken string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.branches[id] = taken
}

// RecordParallel implements engine.Recorder.
func (r *CompositionRecorder) RecordParallel(id string, _ []engine.NamedOutput) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parallel[id] = parallelStatusComplete
}

// CompositionMetadata returns a flat snapshot for the eval metadata bridge.
// The returned maps are copies safe to read after the next Reset().
func (r *CompositionRecorder) CompositionMetadata() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	steps := make(map[string]json.RawMessage, len(r.steps))
	for k, v := range r.steps {
		steps[k] = v
	}
	branches := make(map[string]string, len(r.branches))
	for k, v := range r.branches {
		branches[k] = v
	}
	parallel := make(map[string]string, len(r.parallel))
	for k, v := range r.parallel {
		parallel[k] = v
	}
	return map[string]any{
		"composition_step_outputs":    steps,
		"composition_branch_taken":    branches,
		"composition_parallel_status": parallel,
	}
}
