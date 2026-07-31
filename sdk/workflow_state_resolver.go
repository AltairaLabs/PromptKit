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

// ResolvePendingHandoff implements stage.WorkflowStateResolver.
func (h *workflowResolverHolder) ResolvePendingHandoff(ctx context.Context) (stage.Handoff, error) {
	inner := h.get()
	if inner == nil {
		return stage.Handoff{}, nil
	}
	return inner.ResolvePendingHandoff(ctx)
}

// CurrentStateMeta implements stage.WorkflowStateResolver.
func (h *workflowResolverHolder) CurrentStateMeta() map[string]any {
	inner := h.get()
	if inner == nil {
		return nil
	}
	return inner.CurrentStateMeta()
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

// ResolvePendingHandoff implements stage.WorkflowStateResolver.
func (r *workflowStateResolver) ResolvePendingHandoff(_ context.Context) (stage.Handoff, error) {
	if r.transExec == nil {
		return stage.Handoff{}, nil
	}
	pending := r.transExec.Pending()
	if pending == nil {
		return stage.Handoff{}, nil
	}
	// Capture before committing — CommitPending clears the pending record, and
	// the LLM's `context` argument is the brief for the destination state.
	contextSummary := pending.ContextSummary

	result, err := r.transExec.CommitPending()
	if err != nil {
		return stage.Handoff{}, err
	}
	if result == nil {
		return stage.Handoff{}, nil
	}

	dest := r.spec.States[result.To]
	if dest == nil {
		return stage.Handoff{}, fmt.Errorf("destination state %q not found in spec", result.To)
	}

	// States the turn must not continue into. The transition is committed
	// either way — only the in-turn continuation is suppressed.
	//
	//   external    — RFC 0009: the runtime pauses for an injected event.
	//   composition — CompositionStage runs the state itself.
	//   no prompt   — nothing to render.
	switch {
	case dest.Orchestration == workflow.OrchestrationExternal,
		dest.Orchestration == workflow.OrchestrationComposition,
		dest.PromptTask == "":
		return stage.Handoff{}, nil
	}

	systemPrompt, allowedTools, err := r.renderState(dest, contextSummary)
	if err != nil {
		return stage.Handoff{}, err
	}
	return stage.Handoff{
		Changed:      true,
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
