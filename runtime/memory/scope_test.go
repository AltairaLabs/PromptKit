package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

func callMemoryTool(
	t *testing.T, e *Executor, ctx context.Context, name string, args map[string]any,
) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := e.Execute(ctx, &tools.ToolDescriptor{Name: name}, raw)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	return decoded, nil
}

func listContents(t *testing.T, e *Executor, ctx context.Context) []string {
	t.Helper()
	res, err := callMemoryTool(t, e, ctx, ListToolName, map[string]any{})
	require.NoError(t, err)
	raw, _ := res["memories"].([]any)
	var contents []string
	for _, m := range raw {
		contents = append(contents, m.(map[string]any)["content"].(string))
	}
	return contents
}

// One memory Executor must write each conversation's memories into that
// conversation's own scope.
//
// The scope used to be captured at construction, and the Executor is registered
// by name into a tools.Registry that keeps exactly one executor per name. A
// host running concurrent conversations over one registry therefore had the
// last-registered scope win for all of them: one conversation's remember landed
// in another's scope and was invisible in its own. Reproduced in PromptArena,
// which is what #2011 was filed from.
func TestExecutor_RemembersIntoTheContextScope(t *testing.T) {
	store := NewInMemoryStore()
	// Constructed with A's scope, exactly as a host would before it knew better.
	exec := NewExecutor(store, map[string]string{"user_id": "alice"})

	ctxBob := WithScope(context.Background(), map[string]string{"user_id": "bob"})
	_, err := callMemoryTool(t, exec, ctxBob, RememberToolName, map[string]any{
		"content": "bob likes tea",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"bob likes tea"}, listContents(t, exec, ctxBob))
	assert.Empty(t, listContents(t, exec, context.Background()),
		"alice's scope must not see what bob remembered")
}

// Recall reads the context scope too, so a conversation cannot retrieve another
// conversation's memories.
func TestExecutor_RecallsFromTheContextScope(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, map[string]string{"user_id": "alice"})

	ctxAlice := WithScope(context.Background(), map[string]string{"user_id": "alice"})
	ctxBob := WithScope(context.Background(), map[string]string{"user_id": "bob"})

	_, err := callMemoryTool(t, exec, ctxAlice, RememberToolName, map[string]any{
		"content": "alice likes coffee",
	})
	require.NoError(t, err)

	res, err := callMemoryTool(t, exec, ctxBob, RecallToolName, map[string]any{"query": "likes"})
	require.NoError(t, err)
	assert.Equal(t, float64(0), res["count"], "bob must not recall alice's memory")
}

// Forget is scoped too: deleting in one conversation must not reach another's.
func TestExecutor_ForgetsWithinTheContextScope(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, map[string]string{"user_id": "alice"})

	ctxAlice := WithScope(context.Background(), map[string]string{"user_id": "alice"})
	ctxBob := WithScope(context.Background(), map[string]string{"user_id": "bob"})

	saved, err := callMemoryTool(t, exec, ctxAlice, RememberToolName, map[string]any{
		"content": "alice likes coffee",
	})
	require.NoError(t, err)
	id := saved["id"].(string)

	// The in-memory store treats "not found in this scope" as a no-op rather
	// than an error, so the assertion that matters is that alice still has it.
	_, err = callMemoryTool(t, exec, ctxBob, ForgetToolName, map[string]any{"memory_id": id})
	require.NoError(t, err)
	assert.Equal(t, []string{"alice likes coffee"}, listContents(t, exec, ctxAlice),
		"bob's forget must not reach into alice's scope")

	_, err = callMemoryTool(t, exec, ctxAlice, ForgetToolName, map[string]any{"memory_id": id})
	require.NoError(t, err)
	assert.Empty(t, listContents(t, exec, ctxAlice), "alice can forget her own")
}

// With no scope on the context the configured scope is used, so hosts that have
// not adopted WithScope keep working exactly as before.
func TestExecutor_FallsBackToTheConfiguredScope(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "alice"}
	exec := NewExecutor(store, scope)

	_, err := callMemoryTool(t, exec, context.Background(), RememberToolName, map[string]any{
		"content": "alice likes coffee",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"alice likes coffee"}, listContents(t, exec, context.Background()))
}

// WithScope ignores an empty scope rather than installing one that matches
// nothing, and a context carrying none reports none.
func TestScopeContext_EmptyIsANoOp(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, ScopeFromContext(ctx))
	assert.Equal(t, ctx, WithScope(ctx, nil))
	assert.Equal(t, ctx, WithScope(ctx, map[string]string{}))

	scope := map[string]string{"user_id": "alice"}
	assert.Equal(t, scope, ScopeFromContext(WithScope(ctx, scope)))
}
