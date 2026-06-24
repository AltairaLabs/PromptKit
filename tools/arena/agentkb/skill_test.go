package agentkb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkill_AssembledFromConcepts(t *testing.T) {
	b, err := Skill()
	require.NoError(t, err)
	s := string(b)

	assert.True(t, strings.HasPrefix(s, "---\n"), "skill must start with frontmatter")
	assert.Contains(t, s, "name: promptarena-authoring")
	// Every concept title appears as an idiom sub-heading.
	cs, err := Concepts()
	require.NoError(t, err)
	for _, c := range cs {
		assert.Contains(t, s, "### "+c.Title)
	}
}

func TestSkill_IncludesSpineAndPointers(t *testing.T) {
	out, err := Skill()
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "Building a PromptArena Kit")
	assert.Contains(t, s, "Define success first")
	assert.Contains(t, s, "reference/evals-and-assertions.md")
	assert.Contains(t, s, "Do not gold-plate")
	// Idioms still present (concepts still appended).
	assert.Contains(t, s, "Mock providers simulate the LLM")
	// Frontmatter intact.
	assert.True(t, strings.HasPrefix(s, "---\nname: promptarena-authoring"))
	// Skeletons carry schema modelines.
	assert.Contains(t, s, "# yaml-language-server: $schema=")
}

func TestAgentsBrief_HasMarker(t *testing.T) {
	assert.Contains(t, string(AgentsBrief()), "<!-- promptarena-authoring -->")
}

func TestAgentsBrief_MentionsReferenceAndTraps(t *testing.T) {
	s := string(AgentsBrief())
	assert.True(t, strings.HasPrefix(s, "<!-- promptarena-authoring -->"), "marker must stay first")
	assert.Contains(t, s, "reference/")
	assert.Contains(t, s, "metadata.name")
	assert.Contains(t, s, "definitions") // tool boundary
	assert.Contains(t, s, "Do not gold-plate")
}
