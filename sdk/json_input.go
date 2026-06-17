package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// WithJSONInput binds a structured value to the prompt's template variables for
// a single Send or Stream call. It is the input half of the "function-style"
// pattern: pass a JSON object in, run one turn, and (paired with
// WithResponseFormat) get JSON back.
//
// v may be any JSON-marshalable value — a map[string]any, a struct, or a
// json.RawMessage. The marshaled value is bound to template variables as
// follows:
//
//   - {{input}} is always bound to the whole value as compact JSON.
//   - If the value is a JSON object, each top-level field is also bound to
//     {{field}}: JSON strings pass through verbatim; numbers, booleans, objects
//     and arrays are bound as their compact JSON encoding. A real top-level
//     field named "input" shadows the synthetic whole-object variable.
//
// Bound variables override open-time WithVariables defaults (the live request
// wins). If the caller passes an empty message, the input JSON is also used as
// the user-turn message; a non-empty message is left untouched and only the
// variables are added.
//
//	resp, _ := conv.Send(ctx, "", sdk.WithJSONInput(map[string]any{"topic": "x"}))
func WithJSONInput(v any) SendOption {
	return func(c *sendConfig) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("WithJSONInput: marshal input: %w", err)
		}
		c.jsonInputRaw = raw
		c.jsonInputVars = bindJSONInputVars(raw)
		return nil
	}
}

// bindJSONInputVars maps a marshaled JSON value to flat string template
// variables per the WithJSONInput contract.
func bindJSONInputVars(raw json.RawMessage) map[string]string {
	vars := make(map[string]string)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		for k, v := range obj {
			vars[k] = jsonValueToString(v)
		}
	}

	// The whole object is exposed as {{input}} unless a real top-level field
	// named "input" already claimed that name.
	if _, exists := vars["input"]; !exists {
		vars["input"] = string(raw)
	}

	return vars
}

// jsonValueToString renders a single JSON value as a template-variable string:
// JSON strings are unquoted; everything else keeps its compact JSON encoding.
func jsonValueToString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// sendVarsCtxKey is the context key under which per-send template variables are
// carried from Send/Stream to the send-scoped variable provider.
type sendVarsCtxKey struct{}

// withSendVars returns a context carrying the given per-send variables. It
// returns ctx unchanged when there are no variables to add.
func withSendVars(ctx context.Context, vars map[string]string) context.Context {
	if len(vars) == 0 {
		return ctx
	}
	return context.WithValue(ctx, sendVarsCtxKey{}, vars)
}

// sendScopedVarProvider resolves the per-send variables carried on the request
// context (see WithJSONInput). It is registered once at pipeline-build time and
// runs fresh on every turn, so each Send observes only its own variables. It is
// ordered last among providers so request-scoped values win.
type sendScopedVarProvider struct{}

// Name identifies the provider in logs and merge ordering.
func (sendScopedVarProvider) Name() string { return "send-scoped" }

// Provide returns the per-send variables carried on ctx, or nil when none.
func (sendScopedVarProvider) Provide(ctx context.Context) (map[string]string, error) {
	if vars, ok := ctx.Value(sendVarsCtxKey{}).(map[string]string); ok {
		return vars, nil
	}
	return nil, nil
}

// Compile-time assertion that sendScopedVarProvider implements variables.Provider.
var _ variables.Provider = sendScopedVarProvider{}

// appendSendScopedProvider returns a provider list with the send-scoped provider
// appended last, so per-send variables win over user-configured providers and
// open-time WithVariables. The input slice is not mutated.
func appendSendScopedProvider(existing []variables.Provider) []variables.Provider {
	out := make([]variables.Provider, 0, len(existing)+1)
	out = append(out, existing...)
	out = append(out, sendScopedVarProvider{})
	return out
}
