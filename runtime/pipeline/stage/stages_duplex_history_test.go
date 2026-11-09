package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// historyElem is a message element replayed from the state store.
func historyElem(role, text string) StreamElement {
	e := NewMessageElement(&types.Message{Role: role, Content: text})
	e.Meta.FromHistory = true
	return e
}

// historylessSession hides SeedHistory: a realtime session that cannot take
// prior conversation.
type historylessSession struct{ providers.StreamInputSession }

// historylessProvider opens historylessSessions over the mock provider's.
type historylessProvider struct{ *providersmock.StreamingProvider }

func (p historylessProvider) CreateStreamSession(
	ctx context.Context, cfg *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	s, err := p.StreamingProvider.CreateStreamSession(ctx, cfg)
	return historylessSession{s}, err
}

func runDuplexStage(t *testing.T, s *DuplexProviderStage, inputs []StreamElement) ([]StreamElement, error) {
	t.Helper()
	input := make(chan StreamElement, len(inputs)+1)
	for _, e := range inputs {
		input <- e
	}
	// End the session at once rather than wait out the final-response timer.
	drain := StreamElement{EndOfStream: true}
	drain.Meta.AllResponsesReceived = true
	input <- drain
	close(input)
	output := make(chan StreamElement, 16)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()
	var out []StreamElement
	for e := range output {
		out = append(out, e)
	}
	select {
	case err := <-done:
		return out, err
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not return")
		return nil, nil
	}
}

// The history ahead of the conversation's input is given to the new session,
// then the input is sent as before.
func TestDuplexProviderStage_SeedsLeadingHistory(t *testing.T) {
	provider := providersmock.NewStreamingProvider("t", "m", false)
	s := NewDuplexProviderStage(provider, baseConfig())
	text := "what is my name"
	_, err := runDuplexStage(t, s, []StreamElement{
		historyElem("user", "my name is Ada"), historyElem("assistant", "hi Ada"),
		{Text: &text, EndOfStream: true},
	})
	require.NoError(t, err)
	sessions := provider.GetSessions()
	require.Len(t, sessions, 1, "one session, created on the conversation's own input")
	assert.Equal(t, []providers.HistoryTurn{{Role: "user", Text: "my name is Ada"}, {Role: "assistant", Text: "hi Ada"}},
		sessions[0].GetHistory())
	assert.Equal(t, []string{"what is my name"}, sessions[0].GetTexts())
}

// History followed by no input of the conversation's own still opens and
// seeds the session.
func TestDuplexProviderStage_HistoryOnlyInput(t *testing.T) {
	provider := providersmock.NewStreamingProvider("t", "m", false)
	s := NewDuplexProviderStage(provider, baseConfig())
	_, err := runDuplexStage(t, s, []StreamElement{historyElem("user", "only history")})
	require.NoError(t, err)
	sessions := provider.GetSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, []providers.HistoryTurn{{Role: "user", Text: "only history"}}, sessions[0].GetHistory())
}

// A session that cannot take the history fails the stage with an error the
// consumer receives, and is closed, rather than continuing without memory.
// History with no text to replay needs no support.
func TestDuplexProviderStage_RefusesHistorylessSession(t *testing.T) {
	provider := providersmock.NewStreamingProvider("t", "m", false)
	s := NewDuplexProviderStage(historylessProvider{provider}, baseConfig())
	text := "hello"
	out, err := runDuplexStage(t, s, []StreamElement{historyElem("user", "remember me"), {Text: &text, EndOfStream: true}})
	require.ErrorContains(t, err, "cannot take the conversation's 1 prior messages")
	require.NotEmpty(t, out)
	require.ErrorContains(t, out[len(out)-1].Error, "cannot take")
	sessions := provider.GetSessions()
	require.Len(t, sessions, 1)
	select {
	case <-sessions[0].Done():
	default:
		t.Fatal("the session that could not be seeded was left open")
	}
	assert.Empty(t, sessions[0].GetTexts(), "nothing is sent to a session that lacks the history")

	provider2 := providersmock.NewStreamingProvider("t", "m", false)
	s2 := NewDuplexProviderStage(historylessProvider{provider2}, baseConfig())
	_, err = runDuplexStage(t, s2, []StreamElement{historyElem("assistant", ""), {Text: &text, EndOfStream: true}})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, provider2.GetSessions()[0].GetTexts())
}

// A seeding failure is reported the same way.
func TestDuplexProviderStage_SeedFailure(t *testing.T) {
	provider := providersmock.NewStreamingProvider("t", "m", false)
	s := NewDuplexProviderStage(closingSeedProvider{provider}, baseConfig())
	text := "hello"
	_, err := runDuplexStage(t, s, []StreamElement{historyElem("user", "x"), {Text: &text, EndOfStream: true}})
	require.ErrorContains(t, err, "seeding the session with the conversation's history")
}

// closingSeedProvider closes each session before handing it over, so seeding
// it fails.
type closingSeedProvider struct{ *providersmock.StreamingProvider }

func (p closingSeedProvider) CreateStreamSession(
	ctx context.Context, cfg *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess, err := p.StreamingProvider.CreateStreamSession(ctx, cfg)
	if err == nil {
		_ = sess.Close()
	}
	return sess, err
}

// Input that closes after only history still opens and seeds the session.
func TestDuplexProviderStage_InputClosesAfterHistory(t *testing.T) {
	provider := providersmock.NewStreamingProvider("t", "m", false)
	s := NewDuplexProviderStage(provider, baseConfig())
	input := make(chan StreamElement, 1)
	input <- historyElem("user", "only history")
	close(input)
	output := make(chan StreamElement, 4)
	// The stage then waits for a final response; end that wait at once.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() {
		for range output {
		}
	}()
	_ = s.Process(ctx, input, output)
	sessions := provider.GetSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, []providers.HistoryTurn{{Role: "user", Text: "only history"}}, sessions[0].GetHistory())
}


// Only message elements replayed from history are history: the first element
// of the conversation's own input ends it, whether it carries a message or
// not, and so does a history-flagged element with no message.
func TestDuplexProviderStage_HistoryEndsAtTheFirstOwnElement(t *testing.T) {
	text := "typed"
	ownMessage := NewMessageElement(&types.Message{Role: "user", Content: "own turn"})
	flaggedNoMessage := StreamElement{Text: &text}
	flaggedNoMessage.Meta.FromHistory = true
	cases := map[string]StreamElement{"own message": ownMessage, "flagged, no message": flaggedNoMessage}
	for name, next := range cases {
		t.Run(name, func(t *testing.T) {
			provider := providersmock.NewStreamingProvider("t", "m", false)
			s := NewDuplexProviderStage(provider, baseConfig())
			_, err := runDuplexStage(t, s, []StreamElement{historyElem("user", "remembered"), next,
				historyElem("assistant", "late history")})
			require.NoError(t, err)
			sessions := provider.GetSessions()
			require.Len(t, sessions, 1)
			assert.Equal(t, []providers.HistoryTurn{{Role: "user", Text: "remembered"}}, sessions[0].GetHistory())
		})
	}
}
