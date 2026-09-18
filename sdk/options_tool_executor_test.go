package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

type stubExecutor struct{ name string }

func (e *stubExecutor) Name() string { return e.name }

func (e *stubExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestWithToolExecutor(t *testing.T) {
	t.Run("records the executor by tool name", func(t *testing.T) {
		exec := &stubExecutor{name: "governed"}
		cfg := &config{}
		require.NoError(t, WithToolExecutor("search", exec)(cfg))
		assert.Same(t, exec, cfg.toolExecutors["search"])
	})

	t.Run("accumulates across options", func(t *testing.T) {
		first := &stubExecutor{name: "a"}
		second := &stubExecutor{name: "b"}
		cfg := &config{}
		require.NoError(t, WithToolExecutor("search", first)(cfg))
		require.NoError(t, WithToolExecutor("fetch", second)(cfg))
		assert.Len(t, cfg.toolExecutors, 2)
		assert.Same(t, first, cfg.toolExecutors["search"])
		assert.Same(t, second, cfg.toolExecutors["fetch"])
	})

	t.Run("last registration for a name wins", func(t *testing.T) {
		first := &stubExecutor{name: "a"}
		second := &stubExecutor{name: "b"}
		cfg := &config{}
		require.NoError(t, WithToolExecutor("search", first)(cfg))
		require.NoError(t, WithToolExecutor("search", second)(cfg))
		assert.Same(t, second, cfg.toolExecutors["search"])
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		cfg := &config{}
		require.Error(t, WithToolExecutor("", &stubExecutor{})(cfg))
		assert.Empty(t, cfg.toolExecutors)
	})

	t.Run("rejects a nil executor", func(t *testing.T) {
		cfg := &config{}
		require.Error(t, WithToolExecutor("search", nil)(cfg))
		assert.Empty(t, cfg.toolExecutors)
	})
}
