package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCaller_IsZero(t *testing.T) {
	assert.True(t, Caller{}.IsZero())
	assert.False(t, Caller{ID: "a"}.IsZero())
	assert.False(t, Caller{Claims: map[string]any{"k": 1}}.IsZero())
}
