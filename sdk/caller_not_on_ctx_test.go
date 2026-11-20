package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// hostCallerKey is a host's own context key for a caller.
type hostCallerKey struct{}

// Caller identity reaches tools only through WithCaller. A caller a host puts
// on the context of Send or of OpenDuplexContext is not the tool call's
// caller; the conversation's WithCaller caller is, whatever the context holds.
func TestCaller_NeverTakenFromTheContext(t *testing.T) {
	onCtx := types.Caller{ID: "from-context@example.com"}
	ctx := context.WithValue(context.Background(), hostCallerKey{}, onCtx)
	runs := map[string]func(t *testing.T, exec *callerExecutor, opts ...Option){
		"unary": func(t *testing.T, exec *callerExecutor, opts ...Option) {
			conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(newCallerToolProvider()), WithSkipSchemaValidation()}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec)
			_, err = conv.Send(ctx, "ada")
			require.NoError(t, err)
		},
		"duplex-asm": func(t *testing.T, exec *callerExecutor, opts ...Option) {
			prov := &callerStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}
			conv, err := OpenDuplexContext(ctx, createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(prov), WithSkipSchemaValidation(),
					WithStreamingConfig(convDuplexAudio)}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec)
			convDrain(t, conv)
			require.NoError(t, conv.SendText(ctx, "ada"))
			require.Eventually(t, func() bool { return len(exec.seen()) > 0 }, 5*time.Second, 5*time.Millisecond)
		},
	}
	member := types.Caller{ID: "member@example.com"}
	for name, run := range runs {
		t.Run(name+"/context-only", func(t *testing.T) {
			exec := &callerExecutor{}
			run(t, exec)
			calls := exec.seen()
			require.Len(t, calls, 1)
			assert.Equal(t, types.Caller{}, calls[0].Caller)
		})
		t.Run(name+"/WithCaller", func(t *testing.T) {
			exec := &callerExecutor{}
			run(t, exec, WithCaller(member))
			calls := exec.seen()
			require.Len(t, calls, 1)
			assert.Equal(t, member, calls[0].Caller)
		})
	}
}
