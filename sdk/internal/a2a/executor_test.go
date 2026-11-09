package a2a

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The SDK registers this executor on the tool registry, which routes a tool
// to the executor whose Name matches the tool's mode. Pack A2A tools carry
// mode "a2a", so that is the name the executor must answer to.
func TestNewExecutor_ServesTheA2AToolMode(t *testing.T) {
	assert.Equal(t, "a2a", NewExecutor().Name())
}
