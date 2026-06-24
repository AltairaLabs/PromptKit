package agentkb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateConfigFieldsReference(t *testing.T) {
	out, err := GenerateConfigFieldsReference()
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "# Config Fields")
	// One section per schema type.
	assert.Contains(t, s, "## scenario")
	assert.Contains(t, s, "## provider")
	// Required fields are marked.
	assert.Contains(t, strings.ToLower(s), "required")
	// spec fields were resolved through $defs (scenario requires description).
	assert.Contains(t, s, "`description`")
}
