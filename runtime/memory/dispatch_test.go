package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// plainContext implements Retriever and Extractor without the caller.
type plainContext struct{ scopes []map[string]string }

func (p *plainContext) RetrieveContext(_ context.Context, scope map[string]string, _ []types.Message) ([]*Memory, error) {
	p.scopes = append(p.scopes, scope)
	return []*Memory{{ID: "plain-retrieve"}}, nil
}

func (p *plainContext) Extract(_ context.Context, scope map[string]string, _ []types.Message) ([]*Memory, error) {
	p.scopes = append(p.scopes, scope)
	return []*Memory{{ID: "plain-extract"}}, nil
}

// requestContext implements the caller-taking forms as well.
type requestContext struct {
	plainContext
	callers []types.Caller
}

func (r *requestContext) RetrieveContextRequest(_ context.Context, req ContextRequest) ([]*Memory, error) {
	r.callers = append(r.callers, req.Caller)
	return []*Memory{{ID: "request-retrieve"}}, nil
}

func (r *requestContext) ExtractRequest(_ context.Context, req ContextRequest) ([]*Memory, error) {
	r.callers = append(r.callers, req.Caller)
	return []*Memory{{ID: "request-extract"}}, nil
}

func TestContextDispatch_PrefersTheCallerTakingForm(t *testing.T) {
	caller := types.Caller{ID: "member"}
	req := ContextRequest{Scope: map[string]string{"user_id": "u1"}, Caller: caller}
	r := &requestContext{}

	got, err := RetrieveContextFor(context.Background(), r, req)
	require.NoError(t, err)
	assert.Equal(t, "request-retrieve", got[0].ID)
	got, err = ExtractFor(context.Background(), r, req)
	require.NoError(t, err)
	assert.Equal(t, "request-extract", got[0].ID)

	assert.Equal(t, []types.Caller{caller, caller}, r.callers)
	assert.Empty(t, r.scopes)
}

func TestContextDispatch_FallsBackToThePlainForm(t *testing.T) {
	scope := map[string]string{"user_id": "u1"}
	req := ContextRequest{Scope: scope, Caller: types.Caller{ID: "member"}}
	p := &plainContext{}

	got, err := RetrieveContextFor(context.Background(), p, req)
	require.NoError(t, err)
	assert.Equal(t, "plain-retrieve", got[0].ID)
	got, err = ExtractFor(context.Background(), p, req)
	require.NoError(t, err)
	assert.Equal(t, "plain-extract", got[0].ID)

	assert.Equal(t, []map[string]string{scope, scope}, p.scopes)
}
