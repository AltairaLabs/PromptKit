package mcpsource

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct{ opened int }

func (f *fakeSource) Open(_ context.Context, _ map[string]any) (MCPConn, io.Closer, error) {
	f.opened++
	return MCPConn{URL: "http://x"}, io.NopCloser(nil), nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	resetRegistryForTest()
	f := &fakeSource{}

	RegisterMCPSource("fake", f)

	got, ok := LookupMCPSource("fake")
	require.True(t, ok)
	assert.Same(t, f, got)
}

func TestRegistry_LookupUnknown(t *testing.T) {
	resetRegistryForTest()
	_, ok := LookupMCPSource("nope")
	assert.False(t, ok)
}

func TestRegistry_DuplicateRegistrationPanics(t *testing.T) {
	resetRegistryForTest()
	RegisterMCPSource("x", &fakeSource{})
	assert.Panics(t, func() { RegisterMCPSource("x", &fakeSource{}) })
}

func TestRegistry_ListNames(t *testing.T) {
	resetRegistryForTest()
	RegisterMCPSource("a", &fakeSource{})
	RegisterMCPSource("b", &fakeSource{})
	names := ListMCPSources()
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}
