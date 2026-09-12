package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// These cover #1952. IsAgentTool documented that both the bare member key
// ("summarizer") and the qualified name ("a2a__summarizer") name an agent,
// but ResolveAgentTools — the function that builds the descriptors — matched
// bare keys only, so a prompt whose tools list used the very name the
// resolver itself registers got no agent tool, silently.

func twoAgentPack() *prompt.Pack {
	return &prompt.Pack{Pack: packspec.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {Name: "Summarizer", Description: "Summarizes text"},
			"translator": {Name: "Translator", Description: "Translates text"},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
				"translator": {Description: "Translates text"},
			},
		},
	}}
}

func resolvedNames(descs []*tools.ToolDescriptor) []string {
	names := make([]string, 0, len(descs))
	for _, d := range descs {
		names = append(names, d.Name)
	}
	return names
}

func TestAgentToolResolver_ResolvesQualifiedName(t *testing.T) {
	r := NewAgentToolResolver(twoAgentPack())
	require.NotNil(t, r)
	r.SetEndpointResolver(&MapEndpointResolver{Endpoints: map[string]string{"summarizer": "http://sum:9000"}})

	descriptors := r.ResolveAgentTools([]string{"a2a__summarizer"})
	require.Len(t, descriptors, 1, "the qualified spelling must resolve to the same descriptor as the bare key")
	assert.Equal(t, "a2a__summarizer", descriptors[0].Name)
	assert.Equal(t, "http://sum:9000", descriptors[0].A2AConfig.AgentURL,
		"the endpoint is resolved by member key, not by the qualified name")
}

func TestAgentToolResolver_BothSpellingsYieldOneDescriptor(t *testing.T) {
	r := NewAgentToolResolver(twoAgentPack())
	require.NotNil(t, r)

	descriptors := r.ResolveAgentTools([]string{"summarizer", "a2a__summarizer", "translator"})
	assert.ElementsMatch(t, []string{"a2a__summarizer", "a2a__translator"}, resolvedNames(descriptors),
		"listing a member under both spellings must not register it twice")
}

// The two accepted spellings are the same rule: whatever IsAgentTool says is
// an agent tool, ResolveAgentTools resolves, and vice versa.
func TestAgentToolResolver_IsAgentToolAndResolveAgree(t *testing.T) {
	r := NewAgentToolResolver(twoAgentPack())
	require.NotNil(t, r)

	for _, name := range []string{"summarizer", "a2a__summarizer", "a2a__nobody", "get_weather", "a2a__agent__skill"} {
		resolved := len(r.ResolveAgentTools([]string{name})) > 0
		assert.Equal(t, r.IsAgentTool(name), resolved, "IsAgentTool and ResolveAgentTools disagree on %q", name)
	}
}

// A name that carries the a2a namespace and matches no member is
// unambiguously a mistake and is reported. Ordinary tool names are not agent
// references and stay quiet, and so does the bridge path's three-segment
// a2a__{agent}__{skill} shape, which is registered elsewhere.
func TestAgentToolResolver_WarnsOnlyForUnknownQualifiedAgent(t *testing.T) {
	logs := captureLogs(t)
	r := NewAgentToolResolver(twoAgentPack())
	require.NotNil(t, r)

	descriptors := r.ResolveAgentTools([]string{"a2a__sumarizer", "get_weather", "a2a__research__search"})
	assert.Empty(t, descriptors)

	out := logs.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "a2a__sumarizer", "the misspelled agent reference must be named")
	assert.Contains(t, out, "summarizer", "the warning must list the members that exist")
	assert.NotContains(t, out, "get_weather", "an ordinary tool name is not an agent reference")
	assert.NotContains(t, out, "a2a__research__search", "a bridge-shaped name is not a pack agent reference")
}
