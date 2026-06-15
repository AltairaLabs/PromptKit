package stage

import (
	"encoding/json"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// CompositionRecorder captures per-step composition execution data for Arena
// observability. Populated during a single engine.Execute (RecordStepStarted and
// RecordStepCompleted may fire from parallel-branch goroutines, hence the mutex)
// and read afterward via CompositionMetadata. Reset() is called at the start of
// each turn. An optional Emitter (set via SetEmitter) receives composition.*
// events; all map mutations occur under the mutex but the emitter is called
// outside the lock to avoid holding it across emit from concurrent branches.
type CompositionRecorder struct {
	mu       sync.Mutex
	steps    map[string]json.RawMessage
	branches map[string]string
	parallel map[string]string
	emitter  *events.Emitter
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

// SetEmitter wires an Emitter so the recorder publishes composition.* events.
// Safe to call concurrently; replaces any previously set emitter.
func (r *CompositionRecorder) SetEmitter(em *events.Emitter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emitter = em
}

// Reset clears all recorded data (called per turn). Does NOT clear the emitter.
func (r *CompositionRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = map[string]json.RawMessage{}
	r.branches = map[string]string{}
	r.parallel = map[string]string{}
}

// RecordStepStarted implements engine.Recorder.
func (r *CompositionRecorder) RecordStepStarted(id, kind string, input json.RawMessage) {
	r.mu.Lock()
	em := r.emitter
	r.mu.Unlock()
	if em != nil {
		em.CompositionStepStarted(id, kind, input)
	}
}

// RecordStepCompleted implements engine.Recorder.
func (r *CompositionRecorder) RecordStepCompleted(id, kind string, input, output json.RawMessage, attempt int, err error) { //nolint:lll
	r.mu.Lock()
	r.steps[id] = output
	em := r.emitter
	r.mu.Unlock()
	if em != nil {
		em.CompositionStepCompleted(id, kind, input, output, attempt, err)
	}
}

// RecordBranch implements engine.Recorder.
func (r *CompositionRecorder) RecordBranch(id, taken string) {
	r.mu.Lock()
	r.branches[id] = taken
	em := r.emitter
	r.mu.Unlock()
	if em != nil {
		em.CompositionBranchEvaluated(id, taken)
	}
}

// RecordParallel implements engine.Recorder.
func (r *CompositionRecorder) RecordParallel(id string, branches []engine.NamedOutput) {
	r.mu.Lock()
	r.parallel[id] = parallelStatusComplete
	em := r.emitter
	r.mu.Unlock()
	if em != nil {
		evBranches := make([]events.CompositionParallelBranch, len(branches))
		for i, o := range branches {
			evBranches[i] = events.CompositionParallelBranch{ID: o.ID, Status: parallelStatusComplete}
		}
		em.CompositionParallelCompleted(id, evBranches)
	}
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
