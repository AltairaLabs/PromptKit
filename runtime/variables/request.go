package variables

import "context"

// requestVarsKey is the context key under which per-request template variables
// are carried. Using an unexported type avoids collisions with other packages.
type requestVarsKey struct{}

// WithRequestVars returns a context carrying per-request template variables.
// These are resolved into the variable set for a single request — both when
// validating required variables and when rendering — and take precedence over
// static and provider-supplied variables. Returns ctx unchanged when vars is
// empty.
//
// This is the vehicle for per-send variables (e.g. an SDK structured input):
// because the values ride on the context, they are available at the very start
// of request processing, before any pipeline stage runs.
func WithRequestVars(ctx context.Context, vars map[string]string) context.Context {
	if len(vars) == 0 {
		return ctx
	}
	return context.WithValue(ctx, requestVarsKey{}, vars)
}

// RequestVars returns the per-request variables carried on ctx, or nil if none.
func RequestVars(ctx context.Context) map[string]string {
	if vars, ok := ctx.Value(requestVarsKey{}).(map[string]string); ok {
		return vars
	}
	return nil
}
