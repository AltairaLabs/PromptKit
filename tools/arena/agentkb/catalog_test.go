package agentkb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalog_NonEmpty(t *testing.T) {
	cat, err := LoadCatalog()
	require.NoError(t, err)
	require.NotEmpty(t, cat.Entries)
	for _, e := range cat.Entries {
		assert.NotEmpty(t, e.Name)
		assert.NotEmpty(t, e.Description)
		assert.NotEmpty(t, e.Source)
	}
}

func TestParseCatalog_ValidAndInvalid(t *testing.T) {
	cat, err := parseCatalog([]byte("entries:\n  - name: x\n    source: builtin:x\n"))
	require.NoError(t, err)
	require.Len(t, cat.Entries, 1)
	assert.Equal(t, "x", cat.Entries[0].Name)

	_, err = parseCatalog([]byte("entries: [unterminated"))
	require.Error(t, err)
}

// agentkb-lint: every catalog concept-ref resolves to a real concept, and every
// source uses a known scheme.
func TestCatalog_LintRefsAndSources(t *testing.T) {
	cat, err := LoadCatalog()
	require.NoError(t, err)
	cs, err := Concepts()
	require.NoError(t, err)

	known := map[string]bool{}
	for _, c := range cs {
		known[c.ID] = true
	}
	for _, e := range cat.Entries {
		for _, ref := range e.Concepts {
			assert.True(t, known[ref], "catalog entry %q references unknown concept %q", e.Name, ref)
		}
		assert.True(t,
			strings.HasPrefix(e.Source, "builtin:") || strings.HasPrefix(e.Source, "remote:"),
			"catalog entry %q has invalid source %q", e.Name, e.Source)
	}
}
