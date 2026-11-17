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
	require.NoError(t, s.InterruptResponse(context.Background(), 3))
	require.NoError(t, s.InterruptResponse(context.Background(), 5))
	assert.Equal(t, 2, s.Interrupts())
	assert.Equal(t, []int64{3, 5}, s.InterruptedGenerations())
}
