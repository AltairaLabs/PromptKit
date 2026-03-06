package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessTemplate_CachesCompiledTemplates(t *testing.T) {
	executor := &MockScriptedExecutor{}
	tmpl := `{"greeting": "hello {{.name}}"}`
	args := map[string]any{"name": "world"}

	// First call: compiles and caches.
	result1, err := executor.processTemplate(tmpl, args)
	require.NoError(t, err)
	assert.Contains(t, result1, "hello world")

	// Second call: should hit cache; result identical.
	result2, err := executor.processTemplate(tmpl, args)
	require.NoError(t, err)
	assert.Equal(t, result1, result2)

	// Verify the template is actually in the cache.
	cached, ok := templateCache.Load(tmpl)
	assert.True(t, ok, "template should be in cache")
	assert.NotNil(t, cached)
}

func TestProcessTemplate_InvalidTemplate(t *testing.T) {
	executor := &MockScriptedExecutor{}
	_, err := executor.processTemplate(`{{.invalid`, nil)
	assert.Error(t, err)
}

func TestProcessTemplate_DifferentTemplatesCached(t *testing.T) {
	executor := &MockScriptedExecutor{}
	tmplA := `{"a": "{{.x}}"}`
	tmplB := `{"b": "{{.y}}"}`

	_, err := executor.processTemplate(tmplA, map[string]any{"x": "1"})
	require.NoError(t, err)
	_, err = executor.processTemplate(tmplB, map[string]any{"y": "2"})
	require.NoError(t, err)

	_, okA := templateCache.Load(tmplA)
	_, okB := templateCache.Load(tmplB)
	assert.True(t, okA)
	assert.True(t, okB)
}

func TestMockScriptedExecutor_Execute(t *testing.T) {
	executor := NewMockScriptedExecutor()

	t.Run("renders template from descriptor", func(t *testing.T) {
		descriptor := &ToolDescriptor{
			Name:         "test-scripted",
			Mode:         modeMock,
			MockTemplate: `{"result": "{{.input}}"}`,
		}
		args := json.RawMessage(`{"input": "hello"}`)
		result, err := executor.Execute(context.Background(), descriptor, args)
		require.NoError(t, err)
		assert.Contains(t, string(result), "hello")
	})

	t.Run("rejects non-mock mode", func(t *testing.T) {
		descriptor := &ToolDescriptor{
			Name:         "test-live",
			Mode:         modeLive,
			MockTemplate: `{"result": "nope"}`,
		}
		_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
		assert.ErrorIs(t, err, ErrMockExecutorOnly)
	})

	t.Run("error when no template", func(t *testing.T) {
		descriptor := &ToolDescriptor{
			Name: "test-empty",
			Mode: modeMock,
		}
		_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no mock template")
	})
}
