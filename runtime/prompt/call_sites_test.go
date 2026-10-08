package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callSitesPack has a prompt naming a key, one using the default, a workflow
// state, and a composition whose steps name, inherit and nest keys.
const callSitesPack = `{
	"id": "p", "name": "p", "version": "1.0.0",
	"prompts": {
		"drafter": {"id": "drafter", "name": "d", "version": "1", "system_template": "d", "provider": "writer"},
		"tooler":  {"id": "tooler", "name": "t", "version": "1", "system_template": "t", "provider": "writer",
			"tools": ["lookup"]},
		"plain":   {"id": "plain", "name": "p", "version": "1", "system_template": "p"},
		"stateful": {"id": "stateful", "name": "s", "version": "1", "system_template": "s", "provider": "router"}
	},
	"workflow": {"version": 1, "entry": "s", "states": {"s": {"prompt_task": "stateful"}}},
	"compositions": {"c": {"version": 1, "steps": [
		{"id": "own", "kind": "prompt", "prompt_task": "plain", "provider": "judge"},
		{"id": "inherits", "kind": "prompt", "prompt_task": "drafter"},
		{"id": "agent", "kind": "agent", "prompt_task": "drafter"},
		{"id": "tool", "kind": "tool", "tool": "lookup", "provider": "ignored"},
		{"id": "fan", "kind": "parallel", "branches": [
			{"id": "deep", "kind": "prompt", "prompt_task": "plain", "provider": "deep-key"}
		]}
	]}}
}`

func loadCallSitesPack(t *testing.T) *Pack {
	t.Helper()
	var p Pack
	require.NoError(t, json.Unmarshal([]byte(callSitesPack), &p))
	return &p
}

// The step's key wins, then the prompt's, then the default (RFC 0017).
func TestCallProviderKey_Precedence(t *testing.T) {
	p := loadCallSitesPack(t)
	assert.Equal(t, "judge", CallProviderKey(p, "drafter", "judge"), "the step's own key")
	assert.Equal(t, "writer", CallProviderKey(p, "drafter", ""), "the prompt's key")
	assert.Equal(t, RequirementKeyDefault, CallProviderKey(p, "plain", ""), "neither names one")
	assert.Equal(t, RequirementKeyDefault, CallProviderKey(p, "missing", ""))
	assert.Equal(t, RequirementKeyDefault, CallProviderKey(nil, "drafter", ""))
	assert.False(t, IsNamedProviderKey(RequirementKeyDefault))
	assert.False(t, IsNamedProviderKey(""))
	assert.True(t, IsNamedProviderKey("writer"))
}

// Every call site naming a key is listed once, in order, with whether it
// needs tools; a prompt step that inherits its prompt's key is the prompt's
// site, and a tool step names no provider.
func TestCallSites_ListsEveryNamedKey(t *testing.T) {
	assert.Equal(t, []CallSite{
		{Site: `prompt "drafter"`, Key: "writer"},
		{Site: `prompt "stateful"`, Key: "router", NeedsTools: true},
		{Site: `prompt "tooler"`, Key: "writer", NeedsTools: true},
		{Site: `composition "c" step "own"`, Key: "judge"},
		{Site: `composition "c" step "agent"`, Key: "writer", NeedsTools: true},
		{Site: `composition "c" step "deep"`, Key: "deep-key"},
	}, CallSites(loadCallSitesPack(t)))
	assert.Nil(t, CallSites(nil))
}

// A pack needs a default provider while any call can run on it.
func TestNeedsDefaultProvider(t *testing.T) {
	assert.True(t, NeedsDefaultProvider(loadCallSitesPack(t)), "prompt plain names no key")
	assert.True(t, NeedsDefaultProvider(nil))

	allNamed := &Pack{}
	require.NoError(t, json.Unmarshal([]byte(`{"id":"p","name":"p","version":"1","prompts":{
		"a":{"id":"a","name":"a","version":"1","system_template":"a","provider":"k"}},
		"compositions":{"c":{"version":1,"steps":[{"id":"s","kind":"prompt","prompt_task":"a"}]}}}`), allNamed))
	assert.False(t, NeedsDefaultProvider(allNamed))

	unnamedStep := &Pack{}
	require.NoError(t, json.Unmarshal([]byte(`{"id":"p","name":"p","version":"1","prompts":{
		"a":{"id":"a","name":"a","version":"1","system_template":"a","provider":"k"}},
		"compositions":{"c":{"version":1,"steps":[{"id":"x","kind":"parallel","branches":[
			{"id":"s","kind":"prompt","prompt_task":"gone"}]}]}}}`), unnamedStep))
	assert.True(t, NeedsDefaultProvider(unnamedStep), "a nested step whose prompt is unknown runs on the default")
}
