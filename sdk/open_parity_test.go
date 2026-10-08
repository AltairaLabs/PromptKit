package sdk

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// sdk.Open and PackTemplate.Open share their setup, so the same options wire
// the same things through either (#2211). Before, the template path was a
// hand-kept copy that skipped media storage, option executors, a host tool
// registry and shutdown registration.

const openParityPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "open-parity", "name": "open-parity", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"tools": {"lookup": {"name": "lookup", "description": "look up", "parameters": {"type": "object", "properties": {}}}},
	"prompts": {"chat": {"id": "chat", "name": "chat", "version": "1.0.0", "system_template": "chat", "tools": ["lookup"]}}
}`

// countingExecutor counts the calls it serves.
type countingExecutor struct{ calls atomic.Int32 }

func (e *countingExecutor) Name() string { return "counting" }

func (e *countingExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	e.calls.Add(1)
	return json.RawMessage(`{"ok":true}`), nil
}

func TestOpenParity_TemplateWiresWhatOpenWires(t *testing.T) {
	packPath := createTestPackFile(t, openParityPack)
	openers := map[string]func(opts ...Option) (*Conversation, error){
		"sdk.Open": func(opts ...Option) (*Conversation, error) { return Open(packPath, "chat", opts...) },
		"PackTemplate.Open": func(opts ...Option) (*Conversation, error) {
			tmpl, err := LoadTemplate(packPath)
			if err != nil {
				return nil, err
			}
			return tmpl.Open("chat", opts...)
		},
	}

	for name, open := range openers {
		t.Run(name, func(t *testing.T) {
			t.Run("media storage reaches the providers", func(t *testing.T) {
				spy := &storageSpyProvider{Provider: mock.NewProvider("spy", "mock-model", false)}
				conv, err := open(WithProvider(spy), WithMediaStorage(fakeMediaStore{}))
				require.NoError(t, err)
				defer conv.Close()
				assert.Equal(t, fakeMediaStore{}, spy.injectedStore())
			})
			t.Run("option executors are registered", func(t *testing.T) {
				exec := &countingExecutor{}
				conv, err := open(WithProvider(newRefProvider("agent")), WithToolExecutor("lookup", exec))
				require.NoError(t, err)
				defer conv.Close()
				_, err = conv.ToolRegistry().Execute(context.Background(), "lookup", json.RawMessage(`{}`))
				require.NoError(t, err)
				assert.Equal(t, int32(1), exec.calls.Load())
			})
			t.Run("a host tool registry is the parent", func(t *testing.T) {
				host := tools.NewRegistry()
				require.NoError(t, host.Register(&tools.ToolDescriptor{
					Name: "host_tool", Description: "from the host", InputSchema: json.RawMessage(`{"type":"object"}`),
				}))
				conv, err := open(WithProvider(newRefProvider("agent")), WithToolRegistry(host))
				require.NoError(t, err)
				defer conv.Close()
				td, err := conv.ToolRegistry().GetTool("host_tool")
				require.NoError(t, err)
				assert.Equal(t, "from the host", td.Description)
			})
			t.Run("the shutdown manager tracks it", func(t *testing.T) {
				mgr := NewShutdownManager()
				conv, err := open(WithProvider(newRefProvider("agent")), WithShutdownManager(mgr))
				require.NoError(t, err)
				defer conv.Close()
				assert.Equal(t, 1, mgr.Len())
			})
		})
	}
}
