package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A mock session counts the replies it was asked to stop, so a test can
// assert the pipeline stopped a blocked reply.
func TestMockStreamSession_InterruptResponseIsCounted(t *testing.T) {
	s := NewMockStreamSession()
	assert.Equal(t, 0, s.Interrupts())
	require.NoError(t, s.InterruptResponse(context.Background()))
	require.NoError(t, s.InterruptResponse(context.Background()))
	assert.Equal(t, 2, s.Interrupts())
}
