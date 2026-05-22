package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStore_RequiresRoot(t *testing.T) {
	_, err := NewStore(Options{})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "root")
}

func TestNewStore_CreatesRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "store")

	s, err := NewStore(Options{Root: root})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileStore_Close_Idempotent(t *testing.T) {
	s, err := NewStore(Options{Root: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.NoError(t, s.Close())
}
