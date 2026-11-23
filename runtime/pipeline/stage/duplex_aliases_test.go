package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The facade's wrappers of internal/duplex's functions do what they did
// before the move (#2876), for a stage.X caller.

// facadeInstructionProvider records the system instruction of the session it
// is asked to create.
type facadeInstructionProvider struct {
	*providersmock.StreamingProvider
	mu          sync.Mutex
	instruction string
}

func (p *facadeInstructionProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	p.mu.Lock()
	p.instruction = req.SystemInstruction
	p.mu.Unlock()
	return p.StreamingProvider.CreateStreamSession(ctx, req)
}

func (p *facadeInstructionProvider) recorded() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.instruction
}

// runFacadeDuplexTurn runs s on one typed turn until done reports true, then
// cancels it and waits for Process to return.
func runFacadeDuplexTurn(t *testing.T, s *DuplexProviderStage, done func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	text := "hi"
	in := make(chan StreamElement, 1)
	in <- StreamElement{Text: &text, EndOfStream: true}
	out := make(chan StreamElement, 64)
	returned := make(chan error, 1)
	go func() { returned <- s.Process(ctx, in, out) }()
	require.Eventually(t, done, 3*time.Second, time.Millisecond)
	cancel()
	close(in)
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("Process did not return after cancel")
	}
}

func TestFacade_NewDuplexProviderStageWithTurnStateUsesItsSystemPrompt(t *testing.T) {
	p := &facadeInstructionProvider{StreamingProvider: providersmock.NewStreamingProvider("t", "m", false).
		WithAutoRespond("ok")}
	ts := core.NewTurnState()
	ts.SystemPrompt = "You are terse."
	s := NewDuplexProviderStageWithTurnState(p, &providers.StreamingInputConfig{}, nil, ts)
	runFacadeDuplexTurn(t, s, func() bool { return p.recorded() != "" })
	assert.Equal(t, "You are terse.", p.recorded())
}

func TestFacade_NewDuplexProviderStageWithEmitterEmitsThroughIt(t *testing.T) {
	bus := events.NewEventBus()
	started := make(chan *events.Event, 8)
	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) { started <- e })
	s := NewDuplexProviderStageWithEmitter(providersmock.NewStreamingProvider("t", "m", false).WithAutoRespond("ok"),
		&providers.StreamingInputConfig{}, events.NewEmitter(bus, "run", "sess", "conv"))
	var got *events.Event
	runFacadeDuplexTurn(t, s, func() bool {
		select {
		case got = <-started:
			return true
		default:
			return false
		}
	})
	assert.Equal(t, "conv", got.ConversationID)
}

func TestFacade_NewTranscriptReorderStageWithTimeoutWritesThePlaceholder(t *testing.T) {
	s := NewTranscriptReorderStageWithTimeout("[inaudible]", 10*time.Millisecond)
	in := make(chan StreamElement, 1)
	in <- StreamElement{EndOfStream: true, Message: &types.Message{Role: core.RoleAssistant, Content: "Sure."}}
	out := make(chan StreamElement, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Process(ctx, in, out) }()

	var got []string
	for len(got) < 2 {
		select {
		case e := <-out:
			if e.Message != nil {
				got = append(got, e.Message.Role+":"+e.Message.Content)
			}
		case <-ctx.Done():
			t.Fatalf("got only %v", got)
		}
	}
	assert.Equal(t, []string{"user:[inaudible]", "assistant:Sure."}, got)
	close(in)
	require.NoError(t, <-done)
}
