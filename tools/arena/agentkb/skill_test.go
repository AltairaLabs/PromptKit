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
	// Every concept title appears as a section heading.
	cs, err := Concepts()
	require.NoError(t, err)
	for _, c := range cs {
		assert.Contains(t, s, "## "+c.Title)
	}
}

func TestAgentsBrief_HasMarker(t *testing.T) {
	assert.Contains(t, string(AgentsBrief()), "<!-- promptarena-authoring -->")
}
