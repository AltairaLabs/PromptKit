package sdk

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

// sessionVarProvider exposes the conversation's live variable map — what
// SetVar, SetVars and SetVarsFromEnv write — to the pipeline's variable stage.
//
// It has to be a provider rather than a map handed to the builder because the
// pipeline is constructed once, at Open(), while these setters are called
// afterwards (see sdk/CLAUDE.md, "Pipeline Built Once"). A map copied at build
// time is the bug in #1959: the setter and the renderer held different maps, so
// every variable set after Open rendered as an unresolved placeholder. This is
// the same live-accessor treatment localExecutor uses to see handlers
// registered after Open.
//
// Resolution happens per Send, so the value read is always the latest.
type sessionVarProvider struct {
	conv *Conversation
}

// Name implements variables.Provider.
func (p sessionVarProvider) Name() string { return "session" }

// Provide implements variables.Provider. Returns nothing before a session
// exists — the provider is constructed during Open, ahead of the session it
// reads, and is only ever consulted afterwards.
func (p sessionVarProvider) Provide(_ context.Context) (map[string]string, error) {
	if p.conv == nil {
		return nil, nil
	}
	sess := p.conv.getBaseSession()
	if sess == nil {
		return nil, nil
	}
	return sess.Variables(), nil
}

// Compile-time assertion that sessionVarProvider implements variables.Provider.
var _ variables.Provider = sessionVarProvider{}

// withSessionVars returns a provider list carrying the conversation's live
// variables after the host's own providers, so an explicit SetVar wins over a
// value a provider computed. The per-send provider is appended after this one
// (see appendSendScopedProvider), keeping per-send bindings the most specific
// declaration of all. The input slice is not mutated.
func (c *Conversation) withSessionVars(existing []variables.Provider) []variables.Provider {
	out := make([]variables.Provider, 0, len(existing)+1)
	out = append(out, existing...)
	out = append(out, sessionVarProvider{conv: c})
	return out
}

// setVarNames records which variables the application set on a conversation
// with SetVar, SetVars or SetVarsFromEnv. The session's map also holds the
// variables the conversation was opened with (WithVariables, prompt defaults,
// a workflow's carried context and artifacts), and only the set ones belong
// to the application to carry into another conversation.
type setVarNames struct {
	mu    sync.Mutex
	names map[string]struct{}
}

// noteSetVar records that the application set name.
func (c *Conversation) noteSetVar(name string) {
	c.setVars.mu.Lock()
	defer c.setVars.mu.Unlock()
	if c.setVars.names == nil {
		c.setVars.names = make(map[string]struct{})
	}
	c.setVars.names[name] = struct{}{}
}

// appSetVars returns the current value of every variable the application set
// on this conversation with SetVar, SetVars or SetVarsFromEnv.
func (c *Conversation) appSetVars() map[string]string {
	c.setVars.mu.Lock()
	names := make([]string, 0, len(c.setVars.names))
	for name := range c.setVars.names {
		names = append(names, name)
	}
	c.setVars.mu.Unlock()
	if len(names) == 0 {
		return nil
	}
	sess := c.getBaseSession()
	if sess == nil {
		return nil
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		if v, ok := sess.GetVar(name); ok {
			out[name] = v
		}
	}
	return out
}
