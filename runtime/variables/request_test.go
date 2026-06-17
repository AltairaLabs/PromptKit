package variables

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequestVars_RoundTrip(t *testing.T) {
	ctx := WithRequestVars(context.Background(), map[string]string{"topic": "x"})
	assert.Equal(t, map[string]string{"topic": "x"}, RequestVars(ctx))
}

func TestRequestVars_EmptyLeavesContextUnchanged(t *testing.T) {
	base := context.Background()
	ctx := WithRequestVars(base, nil)
	assert.Equal(t, base, ctx)
	assert.Nil(t, RequestVars(ctx))
}

func TestRequestVars_AbsentReturnsNil(t *testing.T) {
	assert.Nil(t, RequestVars(context.Background()))
}
