package agentkb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcepts_ParseAllWithFrontmatter(t *testing.T) {
	cs, err := Concepts()
	require.NoError(t, err)
	require.NotEmpty(t, cs)

	for _, c := range cs {
		assert.NotEmpty(t, c.ID, "id required")
		assert.NotEmpty(t, c.Title, "title required")
		assert.NotEmpty(t, c.Summary, "summary required")
		assert.NotEmpty(t, c.Body, "body required for %s", c.ID)
	}

	// Sorted by ID.
	for i := 1; i < len(cs); i++ {
		assert.Less(t, cs[i-1].ID, cs[i].ID)
	}
}

func TestConcept_ToolBoundaryExists(t *testing.T) {
	c, err := ConceptByID("tools-definitions-not-implementations")
	require.NoError(t, err)
	assert.Contains(t, c.Body, "definition")
	assert.Contains(t, c.Body, "mode: mock")
}

func TestParseConcept_Errors(t *testing.T) {
	cases := map[string]string{
		"missing frontmatter":     "no fence here\nbody",
		"unterminated":            "---\nid: x\ntitle: y\nsummary: z\nbody with no closing fence",
		"invalid yaml":            "---\nid: [unclosed\n---\nbody",
		"missing required fields": "---\nid: x\n---\nbody",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseConcept(name+".md", []byte(raw))
			require.Error(t, err)
		})
	}
}

func TestParseConcept_Valid(t *testing.T) {
	raw := "---\nid: demo\ntitle: Demo\nsummary: A demo concept\ntags: [a, b]\n---\n\nThe body.\n"
	c, err := parseConcept("demo.md", []byte(raw))
	require.NoError(t, err)
	assert.Equal(t, "demo", c.ID)
	assert.Equal(t, "Demo", c.Title)
	assert.Equal(t, []string{"a", "b"}, c.Tags)
	assert.Equal(t, "The body.\n", c.Body)
}

func TestConcept_LookupAndUnknown(t *testing.T) {
	c, err := ConceptByID("mock-providers")
	require.NoError(t, err)
	assert.Equal(t, "mock-providers", c.ID)
	assert.Contains(t, c.Body, "type: mock")

	_, err = ConceptByID("does-not-exist")
	require.Error(t, err)
}
