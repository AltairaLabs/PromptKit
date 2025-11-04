package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateResultRepository_JSON(t *testing.T) {
	params := &RunParameters{
		OutDir:        "/tmp/test",
		OutputFormats: []string{"json"},
	}

	repo, err := createResultRepository(params)
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestCreateResultRepository_Multiple(t *testing.T) {
	params := &RunParameters{
		OutDir:        "/tmp/test",
		OutputFormats: []string{"json", "junit", "html"},
		JUnitFile:     "/tmp/test/junit.xml",
		HTMLFile:      "/tmp/test/report.html",
	}

	repo, err := createResultRepository(params)
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestCreateResultRepository_UnsupportedFormat(t *testing.T) {
	params := &RunParameters{
		OutDir:        "/tmp/test",
		OutputFormats: []string{"invalid"},
	}

	repo, err := createResultRepository(params)
	assert.Error(t, err)
	assert.Nil(t, repo)
	assert.Contains(t, err.Error(), "unsupported output format: invalid")
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	assert.True(t, contains(slice, "a"))
	assert.True(t, contains(slice, "b"))
	assert.True(t, contains(slice, "c"))
	assert.False(t, contains(slice, "d"))
	assert.False(t, contains([]string{}, "a"))
}