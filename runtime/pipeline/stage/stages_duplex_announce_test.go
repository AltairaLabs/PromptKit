package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A guardrail that cuts a spoken reply has the model say the blocked message,
// so the caller hears why the reply stopped instead of silence. The spoken
// message is heard but is not a turn of its own: the blocked turn already
// carries its text.

// announcingSession is an interruptingSession that also speaks text on
// request (providers.Announcer), recording each request.
type announcingSession struct {
	*interruptingSession
	textOnly  bool
	announced []string
}

func (s *announcingSession) Announce(_ context.Context, text string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.announced = append(s.announced, text)
	return !s.textOnly, nil
}

func (s *announcingSession) announcements() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.announced...)
}

func announceStage(t *testing.T, reg *hooks.Registry) (*DuplexProviderStage, *announcingSession) {
	t.Helper()
	s, sess := cutStage(t, reg)
	a := &announcingSession{interruptingSession: sess}
	s.session = a
	return s, a
}

// A streaming block of a spoken reply has the blocked message spoken. The
// spoken reply that follows is played but adds no turn: the transcript holds
// the blocked turn once, and the next turn is the caller's next reply.
func TestDuplexAnnounce_SpokenReplyBlockIsSpoken(t *testing.T) {
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden ", pcm(20)),
		transcriptChunk("and more", pcm(30)),
		stopChunk(),
		transcriptChunk("I can't say that.", pcm(40)), // the model speaks the message
		stopChunk(),
		transcriptChunk("all fine", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"I can't say that."}, sess.announcements())
	assert.Equal(t, []string{
		"interrupt", "text:I can't say that.",
		"end:I can't say that.|safety",
		"audio:40",
		"text:all fine", "audio:12",
		"end:all fine|stop",
	}, trace(elems))
	assert.Equal(t, []string{"I can't say that.", "all fine"}, historyAssistantContents(s.history.snapshot()),
		"the spoken message is not a turn of its own")
	assert.Empty(t, turnEnds(elems), "the blocked turn completed: its spoken message ends no turn")
	for _, e := range elems {
		assert.False(t, e.EndOfStream && e.Message == nil,
			"an EndOfStream without a Message reaches the caller as an empty reply")
	}
}

// A session whose replies are not spoken says so: nothing is spoken, and the
// reply after the blocked one is a turn as usual.
func TestDuplexAnnounce_UnspokenSessionKeepsTheNextReply(t *testing.T) {
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	sess.textOnly = true

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden", nil), stopChunk(),
		transcriptChunk("all fine", nil), stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt", "text:I can't say that.", "end:I can't say that.|safety",
		"text:all fine", "end:all fine|stop",
	}, trace(elems))
}

// historyAssistantContents lists the content of each assistant message.
func historyAssistantContents(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == roleAssistant {
			out = append(out, m.Content)
		}
	}
	return out
}

// A provider that cannot stop a reply (Gemini Live) ends the blocked reply
// with its own interruption when the message is asked for: the reply after
// that is the spoken message, and the one after it is a turn as usual.
func TestDuplexAnnounce_BlockedReplyEndedByInterruption(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	sess := &unstoppableAnnouncer{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden ", pcm(20)),
		providers.StreamChunk{Interrupted: true}, // the provider abandons the blocked reply
		transcriptChunk("I can't say that.", pcm(40)),
		stopChunk(),
		transcriptChunk("all fine", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"I can't say that."}, sess.announcements(),
		"a session that cannot stop the blocked reply is asked at once")
	assert.Contains(t, trace(elems), "audio:40", "the spoken message is played")
	var replies []string
	for _, e := range elems {
		if m := e.Message; m != nil && m.Role == roleAssistant {
			replies = append(replies, m.Content+"|"+m.FinishReason)
		}
	}
	assert.Equal(t, []string{"I can't say that.|safety", "all fine|stop"}, replies,
		"the blocked turn and the next reply are turns; the spoken message is not")
	assert.Equal(t, []string{types.FinishReasonSafety}, turnEnds(elems),
		"the blocked turn, which never completed, ends where its spoken message does")
}

// turnEnds lists the finish reason of each EndOfTurn element: an assistant
// turn boundary with no Message of its own.
func turnEnds(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.EndOfTurn {
			fr := ""
			if e.Meta.FinishReason != nil {
				fr = *e.Meta.FinishReason
			}
			out = append(out, fr)
		}
	}
	return out
}

// unstoppableAnnouncer speaks text but cannot stop a reply, as Gemini Live.
type unstoppableAnnouncer struct {
	*recordingSession
	announced []string
}

func (s *unstoppableAnnouncer) Announce(_ context.Context, text string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.announced = append(s.announced, text)
	return true, nil
}

func (s *unstoppableAnnouncer) announcements() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.announced...)
}

// A session that can stop the blocked reply is asked for the message only
// once that reply has ended: asked earlier, the message would start under the
// reply being canceled.
func TestDuplexAnnounce_AskedOnceTheBlockedReplyEnds(t *testing.T) {
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	_, err := feedChunks(t, s, transcriptChunk("forbidden ", pcm(20)), transcriptChunk("and more", pcm(30)))
	require.NoError(t, err)

	assert.Empty(t, sess.announcements(), "the blocked reply has not ended")
}

// The spoken message is not an assistant turn: the turn count, which the
// guardrails' turn indexes follow, counts the blocked turn and the next
// reply only.
func TestDuplexAnnounce_MessageIsNotCountedAsATurn(t *testing.T) {
	s, _ := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	_, err := feedChunks(t, s,
		transcriptChunk("forbidden", pcm(20)), stopChunk(),
		transcriptChunk("I can't say that.", pcm(40)), stopChunk(),
		transcriptChunk("all fine", pcm(12)), stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, int64(2), s.assistantTurns.Load())
}

// The output guardrails check the spoken message too. One that says the
// forbidden word again is cut like any reply, and is not asked for again.
func TestDuplexAnnounce_MessageIsGuardedAndNotRepeated(t *testing.T) {
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden", pcm(20)), stopChunk(),
		transcriptChunk("forbidden again", pcm(40)), stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"I can't say that."}, sess.announcements())
	assert.NotContains(t, trace(elems), "audio:40", "the message's forbidden audio is dropped")
	assert.Equal(t, 2, sess.interruptCount(), "the message is cut like any blocked reply")
}

// A guardrail that truncates a reply part-way leaves the caller with what they
// heard: the tail it keeps is shown, not spoken.
func TestDuplexAnnounce_TruncationIsNotSpoken(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 16})
	require.NoError(t, err)
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))

	_, err = feedChunks(t, s,
		transcriptChunk("short start ", pcm(20)), transcriptChunk("then far too long", pcm(30)), stopChunk(),
	)
	require.NoError(t, err)

	assert.Empty(t, sess.announcements())
}

// taggingAnnouncer is an announcingSession that marks the chunks of the
// reply Announce asked for (providers.AnnouncementTagger), as OpenAI Realtime.
type taggingAnnouncer struct{ *announcingSession }

func (taggingAnnouncer) TagsAnnouncements() bool { return true }

// announced marks c as part of the reply an Announce asked for.
func announced(c providers.StreamChunk) providers.StreamChunk {
	meta := map[string]interface{}{providers.MetaKeyAnnouncement: true}
	for k, v := range c.Metadata {
		meta[k] = v
	}
	c.Metadata = meta
	return c
}

// A session that marks the spoken message's reply is matched by the mark, not
// by position: text the caller typed while the blocked reply played was asked
// for first, so its reply comes before the message. That reply is a turn as
// usual, with its text; the marked message is played and is not a turn.
func TestDuplexAnnounce_MarkedMessageAfterAReplyAskedForEarlier(t *testing.T) {
	s, sess := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	s.session = taggingAnnouncer{sess}

	elems, err := feedChunks(t, s,
		gen1(transcriptChunk("forbidden", pcm(20)), 1), gen1(stopChunk(), 1),
		gen1(transcriptChunk("Paris is the capital.", pcm(12)), 2), gen1(stopChunk(), 2),
		gen1(announced(transcriptChunk("I can't say that.", pcm(40))), 3), gen1(announced(stopChunk()), 3),
	)
	require.NoError(t, err)

	var replies []string
	for _, e := range elems {
		if m := e.Message; m != nil && m.Role == roleAssistant {
			replies = append(replies, m.Content+"|"+m.FinishReason)
		}
	}
	assert.Equal(t, []string{"I can't say that.|safety", "Paris is the capital.|stop"}, replies,
		"the reply asked for first is a turn; the marked message is not")
	assert.Contains(t, trace(elems), "audio:40", "the marked message is played")
	assert.Equal(t, int64(2), s.assistantTurns.Load())
}

// gen numbers a chunk as part of the provider's reply g.
func gen1(c providers.StreamChunk, g int64) providers.StreamChunk {
	c.Generation = g
	return c
}

// The spoken message is the first reply after the blocked one, by the
// provider's reply numbers: another reply that streams while it does (the
// caller spoke meanwhile) is a turn as usual.
func TestDuplexAnnounce_MessageIsMatchedByItsReplyNumber(t *testing.T) {
	s, _ := announceStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		gen1(transcriptChunk("forbidden", pcm(20)), 1), gen1(stopChunk(), 1),
		gen1(transcriptChunk("I can't say that.", pcm(40)), 2),
		gen1(transcriptChunk("all fine", pcm(12)), 3), gen1(stopChunk(), 3),
		gen1(stopChunk(), 2),
	)
	require.NoError(t, err)

	var replies []string
	for _, e := range elems {
		if m := e.Message; m != nil && m.Role == roleAssistant {
			replies = append(replies, m.Content)
		}
	}
	assert.Equal(t, []string{"I can't say that.", "all fine"}, replies)
}
