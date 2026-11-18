package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
)

// The model hears spoken input before its transcript exists, so an input
// guardrail can only block it after the fact. Cutting the reply is not
// enough: the caller's turn and the cut reply would stay in the model's
// context, and a later turn could revive what was blocked. A session that can
// take a turn out of its conversation (providers.TurnForgetter) is asked to,
// for the reply the block cut.

// forgettingSession is an announcingSession that also forgets turns on
// request, recording the generation of each.
type forgettingSession struct {
	*announcingSession
	forgot []int64
}

func (s *forgettingSession) ForgetTurn(_ context.Context, generation int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgot = append(s.forgot, generation)
	return nil
}

func (s *forgettingSession) forgotten() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.forgot...)
}

func forgetStage(t *testing.T, reg *hooks.Registry) (*DuplexProviderStage, *forgettingSession) {
	t.Helper()
	s, a := announceStage(t, reg)
	f := &forgettingSession{announcingSession: a}
	s.session = f
	return s, f
}

// A spoken turn blocked while its reply streams is forgotten with that reply,
// and the blocked message is still spoken.
func TestDuplexForget_SpokenInputBlockedMidReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	elems, err := feedChunks(t, s,
		genChunk(5, transcriptChunk("Sure, ", pcm(10))),
		finalTranscript("a wire transfer please"),
		genChunk(5, transcriptChunk("sending it", pcm(20))),
		genChunk(5, stopChunk()),
		genChunk(6, transcriptChunk("I can't help with transfers.", pcm(30))),
		genChunk(6, stopChunk()),
	)
	require.NoError(t, err)

	assert.Equal(t, []int64{5}, sess.forgotten(), "the blocked turn's reply is the one forgotten")
	assert.Equal(t, []int64{5}, sess.interruptedGens())
	assert.Equal(t, []string{"I can't help with transfers."}, sess.announcements(),
		"the blocked message is still spoken")
	assert.Contains(t, trace(elems), "audio:30", "the spoken message is played")
}

// A spoken turn blocked before its reply began is forgotten with the reply
// after the last one seen.
func TestDuplexForget_SpokenInputBlockedBeforeItsReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	_, err := feedChunks(t, s,
		finalTranscript("hello"), genChunk(1, transcriptChunk("hi", nil)), genChunk(1, stopChunk()),
		finalTranscript("a wire transfer"),
		genChunk(2, transcriptChunk("Sure", nil)), genChunk(2, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []int64{2}, sess.forgotten())
}

// A transcript that arrives after its reply completed (OpenAI Realtime) is
// forgotten with that completed reply.
func TestDuplexForget_LateTranscriptForgetsTheCompletedReply(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	_, err := feedChunks(t, s,
		genChunk(3, transcriptChunk("Sure", pcm(10))), genChunk(3, stopChunk()),
		finalTranscript("a wire transfer please"))
	require.NoError(t, err)

	assert.Equal(t, []int64{3}, sess.forgotten())
}

// An allowed turn, and a reply blocked for its own output, forget nothing:
// the caller's turn was fine, and the reply is already cut to what was heard.
func TestDuplexForget_OnlyABlockedInputIsForgotten(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "forbidden")),
		hooks.WithProviderHook(inputBannedWords(t, "wire")),
	))

	_, err := feedChunks(t, s,
		finalTranscript("hello"),
		genChunk(1, transcriptChunk("this is forbidden", pcm(10))), genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []int64{1}, sess.interruptedGens(), "the output block cut the reply")
	assert.Empty(t, sess.forgotten())
}

// A reply an output guardrail already cut is still forgotten when the
// caller's transcript then blocks its turn: the cut happens once, the forget
// is the input block's own.
func TestDuplexForget_InputBlockAfterOutputCutStillForgets(t *testing.T) {
	s, sess := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "forbidden")),
		hooks.WithProviderHook(inputBannedWords(t, "wire")),
	))

	_, err := feedChunks(t, s,
		genChunk(4, transcriptChunk("forbidden", pcm(10))),
		finalTranscript("a wire transfer"),
		genChunk(4, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []int64{4}, sess.interruptedGens())
	assert.Equal(t, []int64{4}, sess.forgotten())
}
