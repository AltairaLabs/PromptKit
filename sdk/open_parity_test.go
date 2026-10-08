package sdk

import (
	"context"
	"encoding/json"
	"runtime"
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

// An Open refused after the conversation exists, or a duplex Open refused for
// a provider that cannot stream, leaves nothing running, through either path.
func TestOpenParity_RefusedOpenLeaksNothing(t *testing.T) {
	packPath := createTestPackFile(t, openParityPack)
	fromTemplate := func(open func(*PackTemplate) error) func() error {
		return func() error {
			tmpl, err := LoadTemplate(packPath)
			if err != nil {
				return err
			}
			return open(tmpl)
		}
	}

	cases := []struct {
		name    string
		refuse  func() error
		wantMsg string
	}{
		{"template capability init", fromTemplate(func(tmpl *PackTemplate) error {
			_, err := tmpl.Open("chat", WithProvider(newRefProvider("agent")), WithCapability(&failingCapability{}))
			return err
		}), `capability "failing" init failed`},
		{"sdk.Open capability init", func() error {
			_, err := Open(packPath, "chat", WithProvider(newRefProvider("agent")), WithCapability(&failingCapability{}))
			return err
		}, `capability "failing" init failed`},
		{"template duplex without streaming", fromTemplate(func(tmpl *PackTemplate) error {
			_, err := tmpl.OpenDuplex("chat", WithProvider(newRefProvider("agent")))
			return err
		}), "does not support duplex streaming"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := runtime.NumGoroutine()
			const attempts = 40
			for range attempts {
				require.ErrorContains(t, tc.refuse(), tc.wantMsg)
			}
			assert.Less(t, runtime.NumGoroutine()-before, attempts, "a goroutine per refused Open leaked")
		})
	}
}

// A capability that failed is not closed; the ones initialized before it are.
func TestOpenParity_RefusedOpenClosesInitializedCapabilities(t *testing.T) {
	packPath := createTestPackFile(t, openParityPack)
	ok := &closeTrackingCapability{}
	_, err := Open(packPath, "chat", WithProvider(newRefProvider("agent")),
		WithCapability(ok), WithCapability(&failingCapability{}))
	require.ErrorContains(t, err, `capability "failing" init failed`)
	assert.True(t, ok.closed.Load(), "a capability initialized before the failure is closed")
}

type closeTrackingCapability struct{ closed atomic.Bool }

func (c *closeTrackingCapability) Name() string                  { return "tracking" }
func (c *closeTrackingCapability) Init(CapabilityContext) error  { return nil }
func (c *closeTrackingCapability) RegisterTools(*tools.Registry) {}
func (c *closeTrackingCapability) Close() error                  { c.closed.Store(true); return nil }

// The duplex gate is one function for every path: VAD and ingestion modes
// drive a text provider, so only ASM mode needs one that streams input.
func TestCheckDuplexProvider(t *testing.T) {
	text := newRefProvider("text")
	require.ErrorContains(t, checkDuplexProvider(text, &config{}), "does not support duplex streaming")
	assert.NoError(t, checkDuplexProvider(text, &config{vadModeConfig: &VADModeConfig{}}))
	assert.NoError(t, checkDuplexProvider(mock.NewStreamingProvider("s", "m", false), &config{}))
}
