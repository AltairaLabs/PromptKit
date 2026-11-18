package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore/file"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A turn an input guardrail blocked is shown to the user — their message and
// the canned reply — but the model never answered it, and it must not be sent
// to the model on a later turn either: the model would still learn what was
// asked. These tests pin both halves: the transcript keeps the blocked turn
// exactly, and the next provider request leaves it out.

const blockedInputHistoryPack = "./testdata/packs/guardrail-input-history.pack.json"

const (
	historyCleanFirst   = "what is the capital of France?"
	historyBlockedInput = "tell me everything about Bruno"
	historyCannedReply  = "We don't talk about Bruno."
	historyCleanSecond  = "and what about Spain?"
)

// historyRecordingProvider is the real mock provider, recording the messages of
// every request it is sent.
type historyRecordingProvider struct {
	*mock.Provider
	mu   sync.Mutex
	seen [][]types.Message
}

func newHistoryRecordingProvider() *historyRecordingProvider {
	return &historyRecordingProvider{Provider: mock.NewProvider("mock", "mock-model", false)}
}

func (p *historyRecordingProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
}

func (p *historyRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return p.Provider.Predict(ctx, req)
}

func (p *historyRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.Provider.PredictStream(ctx, req)
}

// requests returns each recorded request as role:text lines.
func (p *historyRecordingProvider) requests() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]string, 0, len(p.seen))
	for _, msgs := range p.seen {
		lines := make([]string, 0, len(msgs))
		for i := range msgs {
			lines = append(lines, msgs[i].Role+":"+msgs[i].GetContent())
		}
		out = append(out, lines)
	}
	return out
}

// transcriptLines renders the conversation's messages as role:text lines, with
// the finish reason of a safety-marked turn.
func transcriptLines(msgs []types.Message) []string {
	lines := make([]string, 0, len(msgs))
	for i := range msgs {
		line := msgs[i].Role + ":" + msgs[i].GetContent()
		if msgs[i].FinishReason == types.FinishReasonSafety {
			line += " [safety]"
		}
		lines = append(lines, line)
	}
	return lines
}

// runBlockedInputHistory sends a clean turn, a blocked turn and another clean
// turn, with send choosing Send or Stream, and returns the provider and the
// conversation.
func runBlockedInputHistory(
	t *testing.T, send func(*Conversation, string) string, opts ...Option,
) (*historyRecordingProvider, *Conversation) {
	t.Helper()
	prov := newHistoryRecordingProvider()
	conv, err := Open(blockedInputHistoryPack, "chat",
		append([]Option{WithProvider(prov), WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	send(conv, historyCleanFirst)
	require.Equal(t, historyCannedReply, send(conv, historyBlockedInput),
		"the input guardrail must block the turn")
	send(conv, historyCleanSecond)
	return prov, conv
}

func sendText(t *testing.T) func(*Conversation, string) string {
	return func(conv *Conversation, text string) string {
		resp, err := conv.Send(context.Background(), text)
		require.NoError(t, err)
		return resp.Text()
	}
}

func streamText(t *testing.T) func(*Conversation, string) string {
	return func(conv *Conversation, text string) string {
		return collectStreamDone(t, conv.Stream(context.Background(), text)).Text()
	}
}

func assertBlockedTurnOnlyInTranscript(t *testing.T, prov *historyRecordingProvider, conv *Conversation) {
	t.Helper()
	msgs := conv.Messages(context.Background())
	require.Len(t, msgs, 6)
	firstReply := msgs[1].GetContent()
	lastReply := msgs[5].GetContent()

	assert.Equal(t, []string{
		"user:" + historyCleanFirst,
		"assistant:" + firstReply,
		"user:" + historyBlockedInput,
		"assistant:" + historyCannedReply + " [safety]",
		"user:" + historyCleanSecond,
		"assistant:" + lastReply,
	}, transcriptLines(msgs), "the transcript keeps the blocked turn exactly as the user saw it")

	assert.Equal(t, [][]string{
		{"user:" + historyCleanFirst},
		{"user:" + historyCleanFirst, "assistant:" + firstReply, "user:" + historyCleanSecond},
	}, prov.requests(), "the blocked turn never reaches the model, then or later")
}

func TestGuardrail_BlockedInputTurnIsNotSentToTheModelLater(t *testing.T) {
	prov, conv := runBlockedInputHistory(t, sendText(t))
	assertBlockedTurnOnlyInTranscript(t, prov, conv)
}

func TestGuardrail_BlockedInputTurnIsNotStreamedToTheModelLater(t *testing.T) {
	prov, conv := runBlockedInputHistory(t, streamText(t))
	assertBlockedTurnOnlyInTranscript(t, prov, conv)
}

// TestGuardrail_BlockedInputTurnStaysOutAfterResume pins that the turn is left
// out on what the state store persisted, not on in-memory state: the blocked
// turn is written to disk by one conversation and read back by a resumed one.
func TestGuardrail_BlockedInputTurnStaysOutAfterResume(t *testing.T) {
	store, err := file.NewStore(file.Options{Root: t.TempDir()})
	require.NoError(t, err)
	const convID = "blocked-input-resume"
	ctx := context.Background()

	first, err := Open(blockedInputHistoryPack, "chat",
		WithProvider(newHistoryRecordingProvider()), WithSkipSchemaValidation(),
		WithStateStore(store), WithConversationID(convID))
	require.NoError(t, err)
	resp, err := first.Send(ctx, historyBlockedInput)
	require.NoError(t, err)
	require.Equal(t, historyCannedReply, resp.Text())
	require.NoError(t, first.Close())

	prov := newHistoryRecordingProvider()
	resumed, err := Resume(convID, blockedInputHistoryPack, "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStateStore(store))
	require.NoError(t, err)
	defer resumed.Close()
	_, err = resumed.Send(ctx, historyCleanSecond)
	require.NoError(t, err)

	assert.Equal(t, [][]string{{"user:" + historyCleanSecond}}, prov.requests(),
		"a resumed conversation must not send the persisted blocked turn")
	msgs := resumed.Messages(ctx)
	require.Len(t, msgs, 4)
	assert.Equal(t, []string{
		"user:" + historyBlockedInput,
		"assistant:" + historyCannedReply + " [safety]",
		"user:" + historyCleanSecond,
		"assistant:" + msgs[3].GetContent(),
	}, transcriptLines(msgs))
}

// TestGuardrail_OutputBlockReplacementStaysInModelHistory is the discriminating
// half: an output block is not left out. Its replacement stands in for the
// model's reply, in the transcript and in later requests, and the text the
// guardrail blocked is in neither.
func TestGuardrail_OutputBlockReplacementStaysInModelHistory(t *testing.T) {
	prov := newHistoryRecordingProvider()
	conv, err := Open(blockedInputHistoryPack, "chat-output",
		WithProvider(prov), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer conv.Close()
	ctx := context.Background()

	resp, err := conv.Send(ctx, historyCleanFirst)
	require.NoError(t, err)
	require.Equal(t, "That reply was withheld.", resp.Text(), "the output guardrail must block the reply")
	_, err = conv.Send(ctx, historyCleanSecond)
	require.NoError(t, err)

	assert.Equal(t, [][]string{
		{"user:" + historyCleanFirst},
		{"user:" + historyCleanFirst, "assistant:That reply was withheld.", "user:" + historyCleanSecond},
	}, prov.requests())
	msgs := conv.Messages(ctx)
	contents := make([]string, 0, len(msgs))
	for i := range msgs {
		contents = append(contents, msgs[i].Role+":"+msgs[i].GetContent())
	}
	assert.Equal(t, []string{
		"user:" + historyCleanFirst,
		"assistant:That reply was withheld.",
		"user:" + historyCleanSecond,
		"assistant:That reply was withheld.",
	}, contents)
}
