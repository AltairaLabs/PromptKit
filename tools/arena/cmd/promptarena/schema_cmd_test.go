package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSchema_List(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeSchema(&buf, true, nil))
	out := buf.String()
	assert.Contains(t, out, "scenario")
	assert.Contains(t, out, "provider")
}

func TestWriteSchema_Type(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeSchema(&buf, false, []string{"scenario"}))
	assert.Contains(t, buf.String(), "$schema")
}

func TestWriteSchema_UnknownAndMissing(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, writeSchema(&buf, false, []string{"nope"}), "unknown type errors")
	require.Error(t, writeSchema(&buf, false, nil), "missing type errors")
}
