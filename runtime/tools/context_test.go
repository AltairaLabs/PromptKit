package tools_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
)

func TestWithCallID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = tools.WithCallID(ctx, "call-abc-123")
	assert.Equal(t, "call-abc-123", tools.CallIDFromContext(ctx))
}

func TestCallIDFromContext_Missing(t *testing.T) {
	assert.Equal(t, "", tools.CallIDFromContext(context.Background()))
}
