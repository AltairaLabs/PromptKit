package duplex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// An input guardrail on a realtime session judges the provider's transcript
// of the caller's speech. The mock provider sends the transcript its response
// file scripts for a spoken turn, so the guardrail can be tested without a
// live realtime provider: these run the real stage on the real mock provider,
// reading a real mock response file, with a real banned_words guardrail.

// mockCallTurn is one scripted turn: what the caller says and the reply.
type mockCallTurn struct{ said, reply string }

// mockSpokenCall runs spoken caller turns, one at a time, through a duplex
// stage on a mock provider whose response file scripts each turn, with an
// input guardrail banning "wire". It returns what the stage emitted for each
// turn and the mock session.
func mockSpokenCall(t *testing.T, turns ...mockCallTurn) ([][]string, *providersmock.MockStreamSession) {
	t.Helper()
	var yaml strings.Builder
	yaml.WriteString("scenarios:\n  call:\n    turns:\n")
	for i, turn := range turns {
		fmt.Fprintf(&yaml, "      %d:\n        content: %q\n        input_transcript: %q\n", i+1, turn.reply, turn.said)
	}
	path := filepath.Join(t.TempDir(), "mock-responses.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml.String()), 0o600))
	repo, err := providersmock.NewFileMockRepository(path)
	require.NoError(t, err)
	provider := providersmock.NewStreamingProvider("mock", "m", false).WithAutoRespond("unscripted").
		WithMockResponses(repo, "call", repo.BaseDir())

	s := NewDuplexProviderStage(provider, baseConfig())
	s.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	input := make(chan core.StreamElement, 4)
	output := make(chan core.StreamElement, 64)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	got := make([][]string, 0, len(turns))
	for range turns {
		speech := core.StreamElement{Audio: &core.AudioData{Samples: make([]byte, 640), SampleRate: 16000, Channels: 1}}
		speech.EndOfStream = true
		input <- speech
		got = append(got, trace(untilTurnEnds(t, output)))
	}
	drain := core.StreamElement{EndOfStream: true}
	drain.Meta.AllResponsesReceived = true
	input <- drain
	close(input)
	drainUntilClosed(t, output)
	require.NoError(t, <-done)
	return got, provider.GetSession()
}

// untilTurnEnds reads the stage's output up to the end of the reply's turn.
func untilTurnEnds(t *testing.T, output <-chan core.StreamElement) []core.StreamElement {
	t.Helper()
	var elems []core.StreamElement
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-output:
			elems = append(elems, e)
			if e.EndOfStream && e.Message != nil {
				return elems
			}
		case <-deadline:
			require.FailNow(t, "the turn never ended", "got %v", trace(elems))
		}
	}
}

// A caller turn the input guardrail blocks has its reply cut: the caller gets
// the guardrail's message in its place, the reply is stopped, the turn is
// taken out of the model's conversation, and the model is told it was. The
// allowed turn before it is answered and left alone.
func TestDuplexMock_InputGuardrailBlocksASpokenTurn(t *testing.T) {
	turns, sess := mockSpokenCall(t,
		mockCallTurn{said: "what time is it", reply: "It is noon."},
		mockCallTurn{said: "a wire transfer please", reply: "Sure, sending it now."},
	)

	assert.Equal(t, [][]string{
		{"user:what time is it", "end:It is noon.|stop", "text:It is noon."},
		{
			"user:a wire transfer please",
			"interrupt", "text:I can't help with transfers.",
			"end:I can't help with transfers.|safety",
		},
	}, turns)
	assert.Equal(t, []int64{2}, sess.InterruptedGenerations(), "the blocked turn's reply is stopped")
	assert.Equal(t, []string{"mock_input_2"}, sess.ForgottenTurns(), "the blocked turn is forgotten")
	assert.Equal(t, []string{blockedInputRemovedNote}, sess.ContextNotes(), "the model is told")
}

// Spoken turns the guardrail allows are answered, and nothing is stopped,
// forgotten or noted.
func TestDuplexMock_AllowedSpokenTurnsAreAnswered(t *testing.T) {
	turns, sess := mockSpokenCall(t,
		mockCallTurn{said: "what time is it", reply: "It is noon."},
		mockCallTurn{said: "thanks", reply: "Bye."},
	)

	assert.Equal(t, [][]string{
		{"user:what time is it", "end:It is noon.|stop", "text:It is noon."},
		{"user:thanks", "end:Bye.|stop", "text:Bye."},
	}, turns)
	assert.Empty(t, sess.InterruptedGenerations())
	assert.Empty(t, sess.ForgottenTurns())
	assert.Empty(t, sess.ContextNotes())
}
