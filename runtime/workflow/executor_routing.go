package workflow

import "context"

type (
	transitionExecutorKey struct{}
	artifactExecutorKey   struct{}
)

// WithTransitionExecutor attaches the caller's own [TransitionExecutor] to the
// context. Whichever instance the tools.Registry holds under
// [TransitionExecutorMode] forwards workflow__transition to this one.
//
// A Registry keys executors by name and keeps exactly one per name, but a
// TransitionExecutor is bound to one conversation's state machine and holds
// that conversation's pending transition. Two workflow conversations sharing a
// registry therefore had the second one's executor receive the first's
// transition: the first's CommitPending found nothing pending and dropped it
// silently, while the second committed a transition its own model never asked
// for. Attaching the caller's executor per call removes the collision without
// the conversations fighting over the registry — the same shape as
// tools.WithMCPRegistry. See AltairaLabs/PromptKit#2011.
//
// A nil executor is ignored rather than redirecting the call into nothing.
func WithTransitionExecutor(ctx context.Context, e *TransitionExecutor) context.Context {
	if e == nil {
		return ctx
	}
	return context.WithValue(ctx, transitionExecutorKey{}, e)
}

// transitionExecutorFromCtx returns the executor attached by
// [WithTransitionExecutor], or nil when the context carries none.
func transitionExecutorFromCtx(ctx context.Context) *TransitionExecutor {
	if ctx == nil {
		return nil
	}
	e, _ := ctx.Value(transitionExecutorKey{}).(*TransitionExecutor)
	return e
}

// WithArtifactExecutor attaches the caller's own [ArtifactExecutor] to the
// context, for the same reason as [WithTransitionExecutor]: an ArtifactExecutor
// writes into one conversation's state machine, and the registry holds one per
// name. Without this, workflow__set_artifact wrote another conversation's
// artifacts — which the prompt then renders as template variables.
//
// A nil executor is ignored.
func WithArtifactExecutor(ctx context.Context, e *ArtifactExecutor) context.Context {
	if e == nil {
		return ctx
	}
	return context.WithValue(ctx, artifactExecutorKey{}, e)
}

// artifactExecutorFromCtx returns the executor attached by
// [WithArtifactExecutor], or nil when the context carries none.
func artifactExecutorFromCtx(ctx context.Context) *ArtifactExecutor {
	if ctx == nil {
		return nil
	}
	e, _ := ctx.Value(artifactExecutorKey{}).(*ArtifactExecutor)
	return e
}
