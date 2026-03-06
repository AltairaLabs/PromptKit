// Package lru provides a generic least-recently-used cache.
package lru

import "container/list"

// Cache is a generic LRU cache that is NOT goroutine-safe.
// Callers must provide their own synchronization.
type Cache[K comparable, V any] struct {
	maxSize int
	items   map[K]*list.Element
	order   *list.List // front = most recent, back = least recent
	onEvict func(key K, value V)
}

type entry[K comparable, V any] struct {
	key   K
	value V
}

// New creates a new LRU cache with the given maximum size.
// If onEvict is non-nil, it is called whenever an entry is evicted.
func New[K comparable, V any](maxSize int, onEvict func(K, V)) *Cache[K, V] {
	if maxSize <= 0 {
		maxSize = 256
	}
	return &Cache[K, V]{
		maxSize: maxSize,
		items:   make(map[K]*list.Element, maxSize),
		order:   list.New(),
		onEvict: onEvict,
	}
}

// Get returns the value for key and marks it as recently used.
// The second return value reports whether the key was found.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Put adds or updates a key-value pair, evicting the LRU entry if at capacity.
func (c *Cache[K, V]) Put(key K, value V) {
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		el.Value.(*entry[K, V]).value = value
		return
	}

	if c.order.Len() >= c.maxSize {
		c.evictOldest()
	}

	el := c.order.PushFront(&entry[K, V]{key: key, value: value})
	c.items[key] = el
}

// Remove removes a key from the cache. Returns true if the key was present.
func (c *Cache[K, V]) Remove(key K) bool {
	el, ok := c.items[key]
	if !ok {
		return false
	}
	c.removeElement(el)
	return true
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() int {
	return c.order.Len()
}

// MaxSize returns the configured maximum size.
func (c *Cache[K, V]) MaxSize() int {
	return c.maxSize
}

// Keys returns all keys in order from most recently used to least.
func (c *Cache[K, V]) Keys() []K {
	keys := make([]K, 0, c.order.Len())
	for el := c.order.Front(); el != nil; el = el.Next() {
		keys = append(keys, el.Value.(*entry[K, V]).key)
	}
	return keys
}

func (c *Cache[K, V]) evictOldest() {
	el := c.order.Back()
	if el == nil {
		return
	}
	c.removeElement(el)
}

func (c *Cache[K, V]) removeElement(el *list.Element) {
	e := el.Value.(*entry[K, V])
	c.order.Remove(el)
	delete(c.items, e.key)
	if c.onEvict != nil {
		c.onEvict(e.key, e.value)
	}
}
