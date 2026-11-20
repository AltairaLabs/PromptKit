package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// hostValueKey stands in for a host value on the context, such as a logger or
// a database handle, that PromptKit does not read.
type hostValueKey struct{}

// openDuplexFunc opens a duplex conversation on ctx from packPath.
type openDuplexFunc func(ctx context.Context, t *testing.T, packPath string, opts []sdk.Option) (*sdk.Conversation, error)

func openWithOpenDuplexContext(
	ctx context.Context, _ *testing.T, packPath string, opts []sdk.Option,
) (*sdk.Conversation, error) {
	return sdk.OpenDuplexContext(ctx, packPath, "chat", opts...)
}

func openWithTemplate(ctx context.Context, t *testing.T, packPath string, opts []sdk.Option) (*sdk.Conversation, error) {
	tmpl, err := sdk.LoadTemplate(packPath, sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	return tmpl.OpenDuplexContext(ctx, "chat", opts...)
}

var duplexContextOpeners = map[string]openDuplexFunc{
	"OpenDuplexContext":              openWithOpenDuplexContext,
	"PackTemplate.OpenDuplexContext": openWithTemplate,
}

// A duplex session opened with OpenDuplexContext runs its tool calls on a
// context carrying the opening context's values, as Send(ctx) does for a text
// turn (#2690). Canceling the opening context does not end the session: it
// owns its own cancellation.
func TestDuplexContext_ToolCallSeesOpeningContextValues(t *testing.T) {
	for name, open := range duplexContextOpeners {
		t.Run(name, func(t *testing.T) {
			provider := newToolAwareStreamingProvider([]types.MessageToolCall{{
				ID: "call_1", Name: "get_location", Args: json.RawMessage(`{"accuracy":"fine"}`),
			}}, "done")
			packPath := writePackFile(t, duplexToolsPackJSON)

			hostCtx := context.WithValue(context.Background(), hostValueKey{}, "tenant-db")
			openCtx, cancelOpen := context.WithCancel(hostCtx)
			conv, err := open(openCtx, t, packPath, []sdk.Option{
				sdk.WithProvider(provider),
				sdk.WithSkipSchemaValidation(),
				sdk.WithStreamingConfig(&providers.StreamingInputConfig{
					Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
				}),
			})
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			cancelOpen() // the session must outlive the context it was opened with

			patchToolMode(t, conv, "get_location", "client")
			seen := make(chan any, 1)
			conv.OnClientTool("get_location", func(ctx context.Context, _ sdk.ClientToolRequest) (any, error) {
				seen <- ctx.Value(hostValueKey{})
				return map[string]any{"lat": 1}, nil
			})

			responseCh, err := conv.Response()
			require.NoError(t, err)
			require.NoError(t, conv.SendText(context.Background(), "Where am I?"))

			select {
			case got := <-seen:
				assert.Equal(t, "tenant-db", got)
			case <-time.After(5 * time.Second):
				t.Fatal("tool handler never ran")
			}
			chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
			for _, c := range chunks {
				assert.NoError(t, c.Error)
			}
		})
	}
}

// A fork of a session opened with OpenDuplexContext keeps the parent's
// context values: Fork takes no context, and the fork's tool calls run with
// the same host values as the parent's.
func TestDuplexContext_ForkKeepsTheParentsContextValues(t *testing.T) {
	provider := newToolAwareStreamingProvider([]types.MessageToolCall{{
		ID: "call_1", Name: "get_location", Args: json.RawMessage(`{"accuracy":"fine"}`),
	}}, "done")
	packPath := writePackFile(t, duplexToolsPackJSON)

	hostCtx := context.WithValue(context.Background(), hostValueKey{}, "tenant-db")
	parent, err := sdk.OpenDuplexContext(hostCtx, packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = parent.Close() })

	patchToolMode(t, parent, "get_location", "client")
	seen := make(chan any, 1)
	parent.OnClientTool("get_location", func(ctx context.Context, _ sdk.ClientToolRequest) (any, error) {
		seen <- ctx.Value(hostValueKey{})
		return map[string]any{"lat": 1}, nil
	})

	fork, err := parent.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })
	responseCh, err := fork.Response()
	require.NoError(t, err)
	require.NoError(t, fork.SendText(context.Background(), "Where am I?"))

	select {
	case got := <-seen:
		assert.Equal(t, "tenant-db", got)
	case <-time.After(5 * time.Second):
		t.Fatal("the fork's tool handler never ran")
	}
	for _, c := range collectDuplexResponse(t, responseCh, 5*time.Second) {
		assert.NoError(t, c.Error)
	}
}
