package sdk

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/template"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// workflowResolverHolder is a stable stage.WorkflowStateResolver that the
// pipeline can be built against before the real resolver exists.
//
// Open() builds the pipeline — and therefore the ProviderStage — before
// WorkflowConversation has created the TransitionExecutor the resolver needs.
// Every Conversation is built against a holder; a workflow populates it in
// registerWorkflowTools, and a plain conversation leaves it empty, in which
// case it reports no handoff and no state metadata.
type workflowResolverHolder struct {
	mu    sync.RWMutex
	inner stage.WorkflowStateResolver
}

func (h *workflowResolverHolder) set(inner stage.WorkflowStateResolver) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inner = inner
}

// get is nil-receiver safe. A nil *workflowResolverHolder is NOT nil once
// boxed into the stage.WorkflowStateResolver interface, so the pipeline
// builder installs it and the tool loop calls straight through — any
// Conversation built without a holder (tests, and any construction path that
// predates this field) would otherwise panic on the first round.
func (h *workflowResolverHolder) get() stage.WorkflowStateResolver {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.inner
}

// ResolveCurrentState implements stage.WorkflowStateResolver.
func (h *workflowResolverHolder) ResolveCurrentState(ctx context.Context) (stage.Handoff, error) {
	inner := h.get()
	if inner == nil {
		return stage.Handoff{}, nil
	}
	return inner.ResolveCurrentState(ctx)
}

// CurrentStateMeta implements stage.WorkflowStateResolver.
func (h *workflowResolverHolder) CurrentStateMeta() map[string]any {
	inner := h.get()
	if inner == nil {
		return nil
	}
	return inner.CurrentStateMeta()
}

// RecordToolCalls implements stage.ToolCallRecorder.
//
// The holder is what the pipeline installs, so the stage type-asserts against
// it, not the inner resolver — without this hop the count stops here and never
// reaches a workflow. Inner resolvers that do not record are skipped.
func (h *workflowResolverHolder) RecordToolCalls(n int) {
	inner := h.get()
	if inner == nil {
		return
	}
	if rec, ok := inner.(stage.ToolCallRecorder); ok {
		rec.RecordToolCalls(n)
	}
}

// workflowStateResolver implements stage.WorkflowStateResolver for the SDK.
//
// It commits the transition the workflow tool left pending and renders the
// destination state's system prompt, so the provider tool loop's next round
// runs as that state. Without it a transition advances the state machine and
// the destination state never speaks — see
// docs/local-backlog/WORKFLOW_IN_TURN_STATE_HANDOFF_DESIGN.md.
type workflowStateResolver struct {
	machine   *workflow.StateMachine
	spec      *workflow.Spec
	transExec *workflow.TransitionExecutor
	registry  *prompt.Registry
	renderer  *template.Renderer

	// contextSummary is the brief the outgoing state wrote for the incoming
	// one (the transition tool's `context` argument). Retained across calls
	// because ResolveCurrentState re-renders on every execution, including
	// resumes long after the transition committed and cleared the record.
	contextSummary string
}

func newWorkflowStateResolver(
	machine *workflow.StateMachine,
	spec *workflow.Spec,
	transExec *workflow.TransitionExecutor,
	registry *prompt.Registry,
) *workflowStateResolver {
	return &workflowStateResolver{
		machine:   machine,
		spec:      spec,
		transExec: transExec,
		registry:  registry,
		renderer:  template.NewRenderer(),
	}
}

// RecordToolCalls implements stage.ToolCallRecorder, feeding RFC 0009's
// engine.budget.max_tool_calls. The runtime tool loop counts what executed;
// this forwards it to the workflow context, where checkBudgetLocked reads it on
// the next ProcessEvent. Before this existed the counter had no production
// caller at all, so the limit could never fire (#1785).
func (r *workflowStateResolver) RecordToolCalls(n int) {
	if r == nil || r.machine == nil || n <= 0 {
		return
	}
	r.machine.IncrementToolCalls(n)
}

// ResolveCurrentState implements stage.WorkflowStateResolver.
//
// It reports what the turn *should* be running rather than whether something
// just changed, so a re-executed pipeline (HITL resume, deferred client tool)
// — which re-runs PromptAssemblyStage and resets the prompt to the pipeline's
// build-time state — is corrected on the next call. Safe to call repeatedly.
func (r *workflowStateResolver) ResolveCurrentState(_ context.Context) (stage.Handoff, error) {
	// Commit any transition the tool left pending. Capture the brief first:
	// CommitPending clears the record, and the LLM's `context` argument is
	// what the outgoing state wrote for the incoming one.
	justTransitioned := false
	if r.transExec != nil {
		if pending := r.transExec.Pending(); pending != nil {
			r.contextSummary = pending.ContextSummary
			if _, err := r.transExec.CommitPending(); err != nil {
				return stage.Handoff{}, err
			}
			justTransitioned = true
		}
	}

	if r.machine == nil {
		return stage.Handoff{}, nil
	}
	name := r.machine.CurrentState()
	current := r.spec.States[name]
	if current == nil {
		return stage.Handoff{}, fmt.Errorf("current state %q not found in spec", name)
	}

	// States the turn must not auto-advance INTO. This applies only when we
	// arrived here by committing a transition during this turn — an
	// externally orchestrated state still handles ordinary user turns
	// normally. RFC 0009 says the runtime pauses there for an injected event,
	// which constrains where transitions come from, not whether the state may
	// converse. Treating it as "never run" ends a plain Send with no rounds
	// and an empty response.
	//
	//   external    — RFC 0009: wait for the injected event, don't run on.
	//   composition — CompositionStage runs the state itself.
	if justTransitioned {
		switch current.Orchestration {
		case workflow.OrchestrationExternal, workflow.OrchestrationComposition:
			return stage.Handoff{Stop: true}, nil
		case workflow.OrchestrationInternal, workflow.OrchestrationHybrid:
			// Both are runtime-driven: continue the turn as this state.
			// (The zero value "" also lands here — it means internal.)
		}
	}

	// Nothing to render: leave the turn on whatever prompt it already has
	// rather than blanking it.
	if current.PromptTask == "" {
		return stage.Handoff{}, nil
	}

	systemPrompt, allowedTools, err := r.renderState(current, r.contextSummary)
	if err != nil {
		return stage.Handoff{}, err
	}
	return stage.Handoff{
		Valid:        true,
		SystemPrompt: systemPrompt,
		AllowedTools: allowedTools,
	}, nil
}

// renderState loads and renders the destination state's prompt with the
// carry-forward context and current artifact values bound, mirroring what
// openConvForCurrentState injects when it opens a conversation for a state.
func (r *workflowStateResolver) renderState(
	dest *workflow.State, contextSummary string,
) (systemPrompt string, allowedTools []string, err error) {
	if r.registry == nil {
		return "", nil, fmt.Errorf("no prompt registry configured")
	}

	vars := map[string]string{}
	for name, value := range r.machine.Artifacts() {
		vars["artifacts."+name] = value
	}
	if contextSummary != "" {
		vars[workflowContextVar] = contextSummary
	}

	tmpl, err := r.registry.LoadTemplate(dest.PromptTask, vars, "")
	if err != nil {
		return "", nil, fmt.Errorf("load prompt %q for state: %w", dest.PromptTask, err)
	}

	// Same precedence as TemplateStage: template defaults, then fragments,
	// then the values we bind for this handoff.
	merged := make(map[string]string, len(tmpl.DefaultVars)+len(tmpl.FragmentVars)+len(vars))
	maps.Copy(merged, tmpl.DefaultVars)
	maps.Copy(merged, tmpl.FragmentVars)
	maps.Copy(merged, vars)

	rendered, err := r.renderer.RenderDetailed(tmpl.RawTemplate, merged)
	if err != nil {
		// Match TemplateStage's degrade-to-raw behavior rather than failing the
		// turn: a template that renders badly should still hand off.
		return tmpl.RawTemplate, tmpl.AllowedTools, nil //nolint:nilerr // deliberate degrade
	}
	return rendered.Text, tmpl.AllowedTools, nil
}

// CurrentStateMeta implements stage.WorkflowStateResolver. The returned map is
// stamped onto each assistant message so output can be attributed to the state
// that produced it, which per-turn attribution cannot do once a turn spans
// states.
func (r *workflowStateResolver) CurrentStateMeta() map[string]any {
	if r.machine == nil {
		return nil
	}
	name := r.machine.CurrentState()
	meta := map[string]any{"current_state": name}
	if state := r.spec.States[name]; state != nil {
		if state.Description != "" {
			meta["description"] = state.Description
		}
		meta["terminal"] = state.Terminal || len(state.OnEvent) == 0
	}
	return meta
}
