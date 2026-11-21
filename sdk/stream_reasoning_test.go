package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	streamPkg "github.com/AltairaLabs/PromptKit/sdk/v2/stream"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// reasoningTurnRepo answers every model call with turn.
type reasoningTurnRepo struct{ turn mock.Turn }

func (r *reasoningTurnRepo) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	t := r.turn
	return &t, nil
}

func (r *reasoningTurnRepo) GetResponse(context.Context, mock.ResponseParams) (string, error) {
	return r.turn.Content, nil
}

// openReasoningConv opens a conversation whose mock model reasons ("Think",
// "ing.") before it replies "Hi.".
func openReasoningConv(t *testing.T, opts ...Option) *Conversation {
	t.Helper()
	p := filepath.Join(t.TempDir(), "reasoning.pack.json")
	body := `{"id": "reasoning", "name": "reasoning", "version": "1.0.0",
	  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
	  "prompts": {"chat": {"id": "chat", "name": "chat", "version": "1.0.0", "system_template": "You help."}}}`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	turn := mock.Turn{Type: "text", ReasoningChunks: []string{"Think", "ing."}, StreamChunks: []string{"Hi."}}
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &reasoningTurnRepo{turn: turn})
	conv, err := Open(p, "chat", append([]Option{WithProvider(provider), WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// streamedChunk is what a test reads from one StreamChunk.
type streamedChunk struct {
	Type            ChunkType
	Text, Reasoning string
}

func collectStreamed(t *testing.T, chunks <-chan StreamChunk) ([]streamedChunk, *Response) {
	t.Helper()
	var got []streamedChunk
	var done *Response
	for c := range chunks {
		require.NoError(t, c.Error)
		got = append(got, streamedChunk{Type: c.Type, Text: c.Text, Reasoning: c.Reasoning})
		if c.Type == ChunkDone {
			done = c.Message
		}
	}
	return got, done
}

// With WithStreamReasoning, the model's reasoning streams as ChunkReasoning
// chunks as it arrives, before the reply's text; the response still carries
// the whole trace.
func TestStream_WithStreamReasoningStreamsTheReasoning(t *testing.T) {
	conv := openReasoningConv(t, WithStreamReasoning())

	got, done := collectStreamed(t, conv.Stream(context.Background(), "hi"))

	assert.Equal(t, []streamedChunk{
		{Type: ChunkReasoning, Reasoning: "Think"},
		{Type: ChunkReasoning, Reasoning: "ing."},
		{Type: ChunkText, Text: "Hi."},
		{Type: ChunkDone},
	}, got)
	require.NotNil(t, done)
	assert.Equal(t, &types.ReasoningTrace{Text: "Thinking."}, done.Message().Reasoning)
	assert.Equal(t, "reasoning", ChunkReasoning.String())
}

// Without it a stream carries no reasoning chunk, and the reasoning is on the
// response only.
func TestStream_ReasoningIsNotStreamedByDefault(t *testing.T) {
	conv := openReasoningConv(t)

	got, done := collectStreamed(t, conv.Stream(context.Background(), "hi"))

	assert.Equal(t, []streamedChunk{{Type: ChunkText, Text: "Hi."}, {Type: ChunkDone}}, got)
	require.NotNil(t, done)
	assert.Equal(t, &types.ReasoningTrace{Text: "Thinking."}, done.Message().Reasoning)
}

// StreamRaw and the A2A stream carry text, never reasoning, even when the
// conversation streams it.
func TestStream_ReasoningStaysOutOfRawAndA2AStreams(t *testing.T) {
	conv := openReasoningConv(t, WithStreamReasoning())
	raw, err := conv.StreamRaw(context.Background(), "hi")
	require.NoError(t, err)
	var text strings.Builder
	var kinds []streamPkg.ChunkType
	for c := range raw {
		require.NoError(t, c.Error)
		text.WriteString(c.Text)
		if !c.Done {
			kinds = append(kinds, c.Type)
		}
	}
	assert.Equal(t, "Hi.", text.String())
	assert.Equal(t, []streamPkg.ChunkType{streamPkg.ChunkText}, kinds)

	in := make(chan StreamChunk, 3)
	in <- StreamChunk{Type: ChunkReasoning, Reasoning: "Think"}
	in <- StreamChunk{Type: ChunkText, Text: "Hi."}
	in <- StreamChunk{Type: ChunkDone}
	close(in)
	out := make(chan a2aserver.StreamEvent, 3)
	forwardChunks(in, out)
	close(out)
	var events []a2aserver.StreamEvent
	for e := range out {
		events = append(events, e)
	}
	assert.Equal(t, []a2aserver.StreamEvent{
		{Kind: a2aserver.EventText, Text: "Hi."},
		{Kind: a2aserver.EventDone},
	}, events)
}
