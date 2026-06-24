package agentkb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceNamesAndRead(t *testing.T) {
	names, err := ReferenceNames()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cli.md", "config-fields.md", "evals-and-assertions.md"}, names)

	b, err := Reference("evals-and-assertions.md")
	require.NoError(t, err)
	assert.Contains(t, string(b), "# Evals & Assertions")

	_, err = Reference("nope.md")
	assert.Error(t, err)
}
