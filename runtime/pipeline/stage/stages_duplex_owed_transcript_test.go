package stage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A turn whose transcript never came (the provider's transcription failed,
// or the caller's audio held no words) leaves its turn owed one. The next
// turn's transcript can then arrive before that turn's reply begins: it is
// the new turn's, and a block cuts, or holds, the reply that answers it. It
// is not the owed turn's late transcript, whose reply is long done.

// answering is a reply chunk of generation g that names the caller input it
// answers (providers.ReplyToInputKey).
func answering(g int64, input string, c providers.StreamChunk) providers.StreamChunk {
	c = genChunk(g, c)
	if c.Metadata == nil {
		c.Metadata = map[string]interface{}{}
	}
	c.Metadata[providers.ReplyToInputKey] = input
	return c
}

func TestDuplexOwedTranscript_NextTurnsBlockedTranscriptHoldsItsReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "pineapple")),
		hooks.WithProviderHook(inputBannedWords(t, "uncle"))))

	elems, err := feedChunks(t, s,
		// Turn A: no transcript ever comes; its reply is cut for its output.
		answering(1, "item_A", transcriptChunk("Sure: pineapple", pcm(10))),
		answering(1, "item_A", stopChunk()),
		// Turn B: its transcript comes before its reply's first chunk.
		spoken("Who's the uncle in Encanto that nobody talks about?", "item_B"),
		answering(2, "item_B", transcriptChunk("That would be Bruno", pcm(10))),
		answering(2, "item_B", stopChunk()))
	require.NoError(t, err)

	tr := trace(elems)
	assert.NotContains(t, tr, "text:That would be Bruno", "the blocked turn's reply is not heard: %v", tr)
	assert.Equal(t, []string{
		"user:Who's the uncle in Encanto that nobody talks about?",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, tr[len(tr)-4:])
	assert.Equal(t, []string{"item_B"}, sess.forgotten())
	assert.Equal(t, []int64{1, 2}, sess.interruptedGens(), "turn A's cut, then turn B's")
}

// The same with a slow judge (a model round trip): the next turn's reply
// waits for the verdict before any of it goes out.
func TestDuplexOwedTranscript_SlowVerdictHoldsTheNextTurnsReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(&slowInputHook{ProviderHook: inputBannedWords(t, "uncle"), delay: 30 * time.Millisecond})))
	s.inputVerdictInline = time.Millisecond

	elems, err := feedChunksLateVerdicts(t, s,
		answering(1, "item_A", transcriptChunk("Hi", pcm(10))),
		answering(1, "item_A", stopChunk()),
		spoken("Who's the uncle in Encanto?", "item_B"),
		answering(2, "item_B", transcriptChunk("That would be Bruno", pcm(10))),
		answering(2, "item_B", stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Hi", "audio:10", "end:Hi|stop",
		"user:Who's the uncle in Encanto?",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, []string{"item_B"}, sess.forgotten())
}

// A late transcript of the turn its reply answered is still that turn's.
func TestDuplexOwedTranscript_LateTranscriptOfTheAnsweredTurnStaysLate(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "uncle"))))

	elems, err := feedChunks(t, s,
		answering(1, "item_A", transcriptChunk("Sure", pcm(10))),
		answering(1, "item_A", stopChunk()),
		spoken("Who's the uncle?", "item_A"))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure", "audio:10", "end:Sure|stop",
		"user:Who's the uncle?", "interrupt",
	}, trace(elems))
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
	assert.Equal(t, []int64{1}, sess.interruptedGens())
}

// A transcript when no turn is owed one is the caller's new turn, not a late
// one, even when it names its input.
func TestDuplexOwedTranscript_NewTurnIsNotLate(t *testing.T) {
	s, _ := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "uncle"))))

	elems, err := feedChunks(t, s,
		spoken("hello", "item_A"),
		answering(1, "item_A", transcriptChunk("Hi", pcm(10))), answering(1, "item_A", stopChunk()))
	require.NoError(t, err)

	require.NotEmpty(t, elems)
	require.NotNil(t, elems[0].Message)
	assert.Equal(t, "hello", elems[0].Message.Content)
	assert.False(t, elems[0].Meta.LateTranscript, "a new turn's transcript is not marked late")
}
