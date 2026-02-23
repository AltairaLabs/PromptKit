package generate

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAdapter is a test double for SessionSourceAdapter.
type mockAdapter struct {
	name string
}

func (m *mockAdapter) Name() string { return m.name }

func (m *mockAdapter) List(_ context.Context, _ ListOptions) ([]SessionSummary, error) {
	return nil, nil
}

func (m *mockAdapter) Get(_ context.Context, _ string) (*SessionDetail, error) {
	return nil, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	adapter := &mockAdapter{name: "test"}
	r.Register(adapter)

	got, err := r.Get("test")
	require.NoError(t, err)
	assert.Equal(t, "test", got.Name())
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown session source adapter")
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{name: "beta"})
	r.Register(&mockAdapter{name: "alpha"})

	names := r.Names()
	sort.Strings(names)
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestRegistry_RegisterOverwrites(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{name: "dup"})
	r.Register(&mockAdapter{name: "dup"})

	names := r.Names()
	assert.Len(t, names, 1)
}
