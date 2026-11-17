package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerStore is an in-memory store that records the caller each operation
// is given through its options.
type callerStore struct {
	*InMemoryStore
	saved, retrieved, listed, deleted []types.Caller
}

func (s *callerStore) SaveWithOptions(ctx context.Context, m *Memory, opts SaveOptions) error {
	s.saved = append(s.saved, opts.Caller)
	return s.Save(ctx, m)
}

func (s *callerStore) Retrieve(
	ctx context.Context, scope map[string]string, query string, opts RetrieveOptions,
) ([]*Memory, error) {
	s.retrieved = append(s.retrieved, opts.Caller)
	return s.InMemoryStore.Retrieve(ctx, scope, query, opts)
}

func (s *callerStore) List(ctx context.Context, scope map[string]string, opts ListOptions) ([]*Memory, error) {
	s.listed = append(s.listed, opts.Caller)
	return s.InMemoryStore.List(ctx, scope, opts)
}

func (s *callerStore) DeleteWithOptions(
	ctx context.Context, scope map[string]string, id string, opts DeleteOptions,
) error {
	s.deleted = append(s.deleted, opts.Caller)
	return s.Delete(ctx, scope, id)
}

func TestMemoryTools_HandTheCallsCallerToTheStore(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}
	store := &callerStore{InMemoryStore: NewInMemoryStore()}
	registry := tools.NewRegistry()
	RegisterMemoryTools(registry)
	registry.RegisterExecutor(NewExecutor(store, map[string]string{"user_id": "u1"}))

	run := func(name, args string) *tools.ToolResult {
		t.Helper()
		res, err := registry.ExecuteCall(context.Background(), tools.ToolCall{
			Name: name, ID: "c-" + name, Args: json.RawMessage(args), Caller: member,
		})
		require.NoError(t, err)
		require.Empty(t, res.Error)
		return res
	}

	remembered := run(RememberToolName, `{"content":"likes tea"}`)
	var saved struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(remembered.Result, &saved))
	run(RecallToolName, `{"query":"tea"}`)
	run(ListToolName, `{}`)
	run(ForgetToolName, `{"memory_id":"`+saved.ID+`"}`)

	assert.Equal(t, []types.Caller{member}, store.saved)
	assert.Equal(t, []types.Caller{member}, store.retrieved)
	assert.Equal(t, []types.Caller{member}, store.listed)
	assert.Equal(t, []types.Caller{member}, store.deleted)
}

// A store that does not implement OptionsSaver keeps its plain Save.
func TestMemoryTools_PlainStoreStillSaves(t *testing.T) {
	store := NewInMemoryStore()
	registry := tools.NewRegistry()
	RegisterMemoryTools(registry)
	registry.RegisterExecutor(NewExecutor(store, map[string]string{"user_id": "u1"}))

	res, err := registry.ExecuteCall(context.Background(), tools.ToolCall{
		Name: RememberToolName, Args: json.RawMessage(`{"content":"likes tea"}`),
		Caller: types.Caller{ID: "member"},
	})
	require.NoError(t, err)
	require.Empty(t, res.Error)

	mems, err := store.List(context.Background(), map[string]string{"user_id": "u1"}, ListOptions{})
	require.NoError(t, err)
	require.Len(t, mems, 1)
	assert.Equal(t, "likes tea", mems[0].Content)
}
