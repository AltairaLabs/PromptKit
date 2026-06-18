package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteExplain_List(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExplain(&buf, true, nil))
	out := buf.String()
	// Lists id + summary for each concept.
	assert.Contains(t, out, "mock-providers")
	assert.Contains(t, out, "assertions-vs-evals")
}

func TestWriteExplain_Concept(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExplain(&buf, false, []string{"mock-providers"}))
	out := buf.String()
	assert.Contains(t, out, "Mock providers simulate the LLM")
	assert.Contains(t, out, "type: mock")
}

func TestWriteExplain_UnknownAndMissing(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, writeExplain(&buf, false, []string{"nope"}), "unknown id errors")
	require.Error(t, writeExplain(&buf, false, nil), "missing id errors")
}
