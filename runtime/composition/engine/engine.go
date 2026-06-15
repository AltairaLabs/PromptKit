package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
)

// StepExecutor runs one leaf step (prompt|agent|tool) and returns its raw output.
// input is the step's resolved Input (prompt/agent) or resolved Args (tool),
// marshaled to JSON. Injected so the orchestrator core is testable without a
// pipeline; Plan 2b supplies the production implementation.
// For an arg-less tool (nil Args) or nil Input, input is the JSON literal `null`;
// production executors must treat `null` as "no input/args".
type StepExecutor func(ctx context.Context, step *composition.Step, input json.RawMessage) (json.RawMessage, error)

// Recorder observes step execution for observability (Arena testability). Methods
// are called during Execute; a nil Recorder disables recording. RecordStepStarted
// and RecordStepCompleted may be called concurrently from parallel branches —
// implementations must be safe.
type Recorder interface {
	RecordStepStarted(stepID, kind string, input json.RawMessage)
	RecordStepCompleted(stepID, kind string, input, output json.RawMessage, attempt int, err error)
	RecordBranch(stepID string, takenTarget string)
	RecordParallel(stepID string, branches []NamedOutput)
}

// Engine is the deterministic composition scheduler.
type Engine struct {
	exec     StepExecutor
	recorder Recorder
}

// New builds an Engine around a StepExecutor.
func New(exec StepExecutor) *Engine { return &Engine{exec: exec} }

// NewWithRecorder builds an Engine that records step observations via rec.
// A nil rec is equivalent to calling New(exec).
func NewWithRecorder(exec StepExecutor, rec Recorder) *Engine {
	return &Engine{exec: exec, recorder: rec}
}

// stepStatus tracks scheduling state across the array walk.
type stepStatus int

const (
	statusPending   stepStatus = iota // zero value; set on map miss
	statusCompleted stepStatus = iota
	statusSkipped   stepStatus = iota
)

// scopeOutputKey is the map key used to store a step's decoded output in scope.
const scopeOutputKey = "output"

// Execute walks the composition's step DAG to completion and returns the bound
// output (comp.Output, or the last output-producing step).
// The scope map is not safe for concurrent mutation; parallel branches (Task 7)
// read it during fan-out and their merged result is written back only at the join.
func (e *Engine) Execute(
	ctx context.Context, comp *composition.Composition, input json.RawMessage,
) (json.RawMessage, error) {
	if comp == nil {
		return nil, fmt.Errorf("nil composition")
	}

	var decodedInput any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decodedInput); err != nil {
			return nil, fmt.Errorf("decoding composition input: %w", err)
		}
	}
	scope := Scope{"input": decodedInput}
	status := make(map[string]stepStatus, len(comp.Steps))
	var lastOutputStep string

	for _, step := range comp.Steps {
		if status[step.ID] == statusSkipped {
			continue
		}
		if shouldSkip(step, status) {
			status[step.ID] = statusSkipped
			continue
		}
		last, err := e.runStep(ctx, step, scope, status)
		if err != nil {
			return nil, err
		}
		if last != "" {
			lastOutputStep = last
		}
	}

	return bindOutput(comp, scope, lastOutputStep)
}

// runStep dispatches a single step to its handler, updates scope and status, and
// returns the step ID if it produced an output (empty string if it did not).
func (e *Engine) runStep(
	ctx context.Context, step *composition.Step, scope Scope, status map[string]stepStatus,
) (string, error) {
	switch step.Kind {
	case composition.KindBranch:
		if err := e.runBranch(step, scope, status); err != nil {
			return "", err
		}
		return "", nil
	case composition.KindParallel:
		out, err := e.runParallel(ctx, step, scope)
		if err != nil {
			return "", fmt.Errorf("step %q: %w", step.ID, err)
		}
		scope[step.ID] = map[string]any{scopeOutputKey: out}
		status[step.ID] = statusCompleted
		return step.ID, nil
	case composition.KindPrompt, composition.KindAgent, composition.KindTool:
		out, err := e.runLeaf(ctx, step, scope)
		if err != nil {
			return "", fmt.Errorf("step %q: %w", step.ID, err)
		}
		scope[step.ID] = map[string]any{scopeOutputKey: out}
		status[step.ID] = statusCompleted
		return step.ID, nil
	default:
		return "", fmt.Errorf("step %q: unknown kind %q", step.ID, step.Kind)
	}
}

// shouldSkip reports whether a step is unreachable: it has depends_on and every
// dependency was skipped. A skipped dependency otherwise counts as satisfied.
//
// Soundness note: this reads status[dep] during an array-order walk and treats an
// unset (pending) status as "not skipped". That is only correct because validation
// guarantees every depends_on target precedes the step in array order (forward
// deps are rejected by the validator's acyclic check over the sequential backbone).
// If that invariant ever changes, a forward dep could read as pending and a skip
// would fail to propagate.
func shouldSkip(step *composition.Step, status map[string]stepStatus) bool {
	if len(step.DependsOn) == 0 {
		return false
	}
	for _, dep := range step.DependsOn {
		if status[dep] != statusSkipped {
			return false
		}
	}
	return true
}

// runLeaf resolves a leaf step's input/args, executes it, and decodes the output.
func (e *Engine) runLeaf(ctx context.Context, step *composition.Step, scope Scope) (any, error) {
	var raw any
	if step.Kind == composition.KindTool {
		raw = step.Args
	} else {
		raw = step.Input
	}
	resolved, err := resolveInput(raw, scope)
	if err != nil {
		return nil, err
	}
	resolvedJSON, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("marshaling resolved input: %w", err)
	}
	kind := string(step.Kind)
	if e.recorder != nil {
		e.recorder.RecordStepStarted(step.ID, kind, resolvedJSON)
	}
	out, attempt, execErr := e.execWithRetry(ctx, step, resolvedJSON)
	if e.recorder != nil {
		e.recorder.RecordStepCompleted(step.ID, kind, resolvedJSON, out, attempt, execErr)
	}
	if execErr != nil {
		return nil, execErr
	}
	return decodeOutput(out)
}

// execWithRetry invokes the executor, retrying per the step's retry modifier.
// max_attempts is the total attempt count (a missing/zero/one modifier means a
// single attempt). Returns the raw output, the number of attempts actually made,
// and any error. The last error propagates when the budget is exhausted. Used
// by runLeaf, so parallel branches inherit retry too.
func (e *Engine) execWithRetry(
	ctx context.Context, step *composition.Step, input json.RawMessage,
) (json.RawMessage, int, error) {
	attempts := 1
	if step.Modifiers != nil && step.Modifiers.Retry != nil && step.Modifiers.Retry.MaxAttempts > 1 {
		attempts = step.Modifiers.Retry.MaxAttempts
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, i + 1, err
		}
		out, err := e.exec(ctx, step, input)
		if err == nil {
			return out, i + 1, nil
		}
		lastErr = err
	}
	return nil, attempts, lastErr
}

// decodeOutput turns a step's raw JSON output into a scope value. Empty output and
// a literal JSON null both decode to nil; downstream ${step.output.x} resolution
// against a nil value fails closed (resolves to not-found), which is intended.
func decodeOutput(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decoding step output: %w", err)
	}
	return v, nil
}

// bindOutput resolves the composition's output value to JSON.
func bindOutput(comp *composition.Composition, scope Scope, lastOutputStep string) (json.RawMessage, error) {
	target := comp.Output
	if target == "" {
		target = lastOutputStep
	}
	if target == "" {
		return nil, fmt.Errorf("composition produced no output")
	}
	entry, ok := scope[target].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("output step %q produced no bindable output "+
			"(it did not complete, or is a branch/control step)", target)
	}
	return json.Marshal(entry[scopeOutputKey])
}

// runBranch evaluates the branch predicate and marks the not-taken target
// skipped. Predicate true -> active target is Then, not-taken is Else; predicate
// false -> active target is Else, not-taken is Then. The not-taken target is
// skipped only when it differs from the taken target (Then == Else converges, so
// the shared step must still run). The branch step itself produces no output.
func (e *Engine) runBranch(step *composition.Step, scope Scope, status map[string]stepStatus) error {
	taken, err := evalPredicate(step.Predicate, scope)
	if err != nil {
		return fmt.Errorf("step %q: %w", step.ID, err)
	}
	takenTarget, notTaken := step.Else, step.Then
	if taken {
		takenTarget, notTaken = step.Then, step.Else
	}
	if notTaken != "" && notTaken != takenTarget {
		status[notTaken] = statusSkipped
	}
	status[step.ID] = statusCompleted
	if e.recorder != nil {
		e.recorder.RecordBranch(step.ID, takenTarget)
	}
	return nil
}

// runParallel runs each branch concurrently, collects outputs in branch order,
// and reduces them. Branches must be leaf steps (prompt|agent|tool); nested
// branch/parallel inside a parallel is out of scope for v1 and is rejected.
//
// Semantics: a branch error does NOT cancel its siblings — all branches run to
// completion and every error is surfaced together via errors.Join. The goroutine
// count equals the (author-bounded) branch count; cancellation responsiveness
// depends on the injected executor honoring ctx (the engine does not poll it).
func (e *Engine) runParallel(ctx context.Context, step *composition.Step, scope Scope) (any, error) {
	if step.Reduce == nil {
		return nil, fmt.Errorf("parallel step %q: missing reducer", step.ID)
	}
	outs := make([]NamedOutput, len(step.Branches))
	errs := make([]error, len(step.Branches))

	var wg sync.WaitGroup
	for i, branch := range step.Branches {
		wg.Add(1)
		go func(i int, branch *composition.Step) {
			defer wg.Done()
			if branch.Kind == composition.KindBranch || branch.Kind == composition.KindParallel {
				errs[i] = fmt.Errorf("branch %q: nested %s not supported in a parallel step", branch.ID, branch.Kind)
				return
			}
			out, err := e.runLeaf(ctx, branch, scope)
			if err != nil {
				errs[i] = fmt.Errorf("branch %q: %w", branch.ID, err)
				return
			}
			outs[i] = NamedOutput{ID: branch.ID, Output: out}
		}(i, branch)
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if e.recorder != nil {
		e.recorder.RecordParallel(step.ID, outs)
	}
	merged := reduce(step.Reduce, outs)
	return map[string]any{step.Reduce.Into: merged}, nil
}
