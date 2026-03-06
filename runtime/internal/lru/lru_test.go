package lru

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_BasicOperations(t *testing.T) {
	c := New[string, int](3, nil)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	v, ok = c.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)

	assert.Equal(t, 3, c.Len())
}

func TestCache_Eviction(t *testing.T) {
	var evictedKeys []string
	c := New[string, int](2, func(k string, _ int) {
		evictedKeys = append(evictedKeys, k)
	})

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // Should evict "a"

	assert.Equal(t, []string{"a"}, evictedKeys)
	assert.Equal(t, 2, c.Len())

	_, ok := c.Get("a")
	assert.False(t, ok)

	v, ok := c.Get("c")
	require.True(t, ok)
	assert.Equal(t, 3, v)
}

func TestCache_GetPromotesToFront(t *testing.T) {
	var evictedKeys []string
	c := New[string, int](2, func(k string, _ int) {
		evictedKeys = append(evictedKeys, k)
	})

	c.Put("a", 1)
	c.Put("b", 2)

	// Access "a" to make it most recently used
	c.Get("a")

	// Add "c" — should evict "b" (now the LRU)
	c.Put("c", 3)

	assert.Equal(t, []string{"b"}, evictedKeys)
	_, ok := c.Get("a")
	assert.True(t, ok)
}

func TestCache_UpdateExisting(t *testing.T) {
	c := New[string, int](2, nil)

	c.Put("a", 1)
	c.Put("a", 10) // Update value

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 10, v)
	assert.Equal(t, 1, c.Len())
}

func TestCache_Remove(t *testing.T) {
	c := New[string, int](3, nil)

	c.Put("a", 1)
	c.Put("b", 2)

	ok := c.Remove("a")
	assert.True(t, ok)
	assert.Equal(t, 1, c.Len())

	ok = c.Remove("nonexistent")
	assert.False(t, ok)
}

func TestCache_GetMiss(t *testing.T) {
	c := New[string, int](3, nil)

	v, ok := c.Get("missing")
	assert.False(t, ok)
	assert.Equal(t, 0, v)
}

func TestCache_DefaultMaxSize(t *testing.T) {
	c := New[string, int](0, nil)
	assert.Equal(t, 256, c.MaxSize())
}

func TestCache_Keys(t *testing.T) {
	c := New[string, int](5, nil)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	// Most recently used first
	keys := c.Keys()
	assert.Equal(t, []string{"c", "b", "a"}, keys)
}
