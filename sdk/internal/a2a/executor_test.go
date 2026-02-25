package a2a

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewExecutor(t *testing.T) {
	exec := NewExecutor()
	assert.NotNil(t, exec)
}
