package stage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The facade's wrappers of internal/provider's functions do what they did
// before the move (#2876), for a stage.X caller.

func TestFacade_NewFileSchemaResolverReadsUnderTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s.json"), []byte(`{"type":"object"}`), 0o600))
	resolve := NewFileSchemaResolver(dir)
	got, err := resolve("s.json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"object"}`, string(got))
	none, err := resolve("")
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestFacade_ParseStructuredOutputMode(t *testing.T) {
	assert.Equal(t, StructuredOutputFinalTurn, ParseStructuredOutputMode("final_turn"))
	assert.Equal(t, StructuredOutputEveryRound, ParseStructuredOutputMode("every_round"))
	assert.Equal(t, StructuredOutputMode(""), ParseStructuredOutputMode("bogus"))
}

func TestFacade_NewProviderStageWithEmitterEmitsThroughIt(t *testing.T) {
	bus := events.NewEventBus()
	started := make(chan *events.Event, 1)
	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) { started <- e })
	s := NewProviderStageWithEmitter(mock.NewProvider("p", "m", false), nil, nil, nil,
		events.NewEmitter(bus, "run", "sess", "conv"))
	assert.Equal(t, "provider", s.Name())

	in := make(chan StreamElement, 1)
	in <- NewMessageElement(&types.Message{Role: "user", Content: "hi"})
	close(in)
	out := make(chan StreamElement, 8)
	require.NoError(t, s.Process(context.Background(), in, out))
	select {
	case e := <-started:
		assert.Equal(t, "conv", e.ConversationID)
	case <-time.After(3 * time.Second):
		t.Fatal("no provider.call.started event")
	}
}
