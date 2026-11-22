package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedReply is a canned reply as runBeforeCallHooks (direction input) or an
// output block (direction output) records it.
func guardedReply(content, direction string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Validations: []types.ValidationResult{{
			ValidatorType: "content_excludes",
			Details:       map[string]any{"direction": direction},
		}},
	}
}

func blockedInputReply(content string) types.Message {
	return guardedReply(content, hooks.DirectionInput)
}

// markedReply is a canned reply as a block marks it, with no validation (a
// hook that names no validator).
func markedReply(content string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Meta: map[string]interface{}{types.MetaBlockedInput: true},
	}
}

func roleLines(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}

// turnResult is a tool result message answering call id.
func turnResult(id, text string) types.Message {
	r := types.NewTextToolResult(id, "lookup", text)
	return types.Message{Role: core.RoleTool, Content: text, ToolResult: &r}
}

func interruptedReply(content string) types.Message {
	return types.Message{Role: "assistant", Content: content, FinishReason: types.FinishReasonInterrupted}
}

func TestExcludeBlockedInputTurns(t *testing.T) {
	tests := []struct {
		name string
		msgs []types.Message
		want []string
	}{
		{
			name: "drops the blocked user message and its canned reply",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"}, {Role: core.RoleAssistant, Content: "a1"},
				{Role: core.RoleUser, Content: "forbidden"}, blockedInputReply("refused"),
				{Role: core.RoleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:a1", "user:q2"},
		},
		{
			name: "drops a marked reply that records no validation",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "forbidden"}, markedReply("refused"),
				{Role: core.RoleUser, Content: "q2"},
			},
			want: []string{"user:q2"},
		},
		{
			name: "keeps a reply whose mark is not true",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				func() types.Message {
					m := markedReply("a1")
					m.Meta[types.MetaBlockedInput] = false
					return m
				}(),
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "keeps a marked message that is neither a reply nor a request",
			msgs: []types.Message{func() types.Message {
				m := markedReply("r1")
				m.Role = core.RoleTool
				return m
			}()},
			want: []string{"tool:r1"},
		},
		{
			name: "drops a marked user message alone, keeping the reply before it",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"}, {Role: core.RoleAssistant, Content: "sure"},
				{Role: core.RoleUser, Content: "forbidden", Meta: map[string]interface{}{types.MetaBlockedInput: true}},
				{Role: core.RoleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:sure", "user:q2"},
		},
		{
			name: "a request answered by the reply before it takes that reply",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"}, {Role: core.RoleAssistant, Content: "a1"},
				{Role: core.RoleAssistant, Content: "sure"},
				{Role: core.RoleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, BlockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "a request answered by the reply before it never takes a tool call or a user message",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				{Role: core.RoleAssistant, ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "t"}}},
				{Role: core.RoleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, BlockedReplyBeforeMetaKey: true,
				}},
				{Role: core.RoleUser, Content: "forbidden again", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, BlockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1", "assistant:"},
		},
		{
			name: "a marked user message never takes the user message before it",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				{Role: core.RoleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, BlockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1"},
		},
		{
			name: "keeps an output block's replacement",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"}, guardedReply("withheld", hooks.DirectionOutput),
				{Role: core.RoleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:withheld", "user:q2"},
		},
		{
			name: "keeps a safety reply without a validation",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				{Role: core.RoleAssistant, Content: "filtered", FinishReason: types.FinishReasonSafety},
			},
			want: []string{"user:q1", "assistant:filtered"},
		},
		{
			name: "keeps an input validation on a reply that did not stop for safety",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				func() types.Message { m := blockedInputReply("a1"); m.FinishReason = "stop"; return m }(),
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "only an assistant message is a canned reply",
			msgs: []types.Message{func() types.Message {
				m := blockedInputReply("q1")
				m.Role = core.RoleUser
				return m
			}()},
			want: []string{"user:q1"},
		},
		{
			name: "drops only the reply when no user message precedes it",
			msgs: []types.Message{
				{Role: core.RoleUser, Content: "q1"},
				{Role: core.RoleAssistant, ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "t"}}},
				turnResult("c1", "r"),
				blockedInputReply("refused"),
				{Role: core.RoleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:", "tool:r", "user:q2"},
		},
		{
			name: "a blocked reply that opens the history drops alone",
			msgs: []types.Message{blockedInputReply("refused"), {Role: core.RoleUser, Content: "q2"}},
			want: []string{"user:q2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, roleLines(ExcludeBlockedInputTurns(tt.msgs)))
		})
	}
}

func TestExcludeBlockedInputTurns_KeepsAReplyWithToolCalls(t *testing.T) {
	reply := blockedInputReply("calling")
	reply.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "t"}}
	msgs := []types.Message{{Role: core.RoleUser, Content: "q1"}, reply}
	got := ExcludeBlockedInputTurns(msgs)
	require.Len(t, got, 2, "a reply carrying tool calls is not a canned turn; dropping it would orphan its results")
	assert.Equal(t, "c1", got[1].ToolCalls[0].ID)
}

func TestExcludeFromModelContext_DropsBothInterruptedAndBlocked(t *testing.T) {
	msgs := []types.Message{
		{Role: core.RoleUser, Content: "q1"}, interruptedReply("half"),
		{Role: core.RoleUser, Content: "forbidden"}, blockedInputReply("refused"),
		{Role: core.RoleUser, Content: "q2"},
	}
	assert.Equal(t, []string{"user:q1", "user:q2"}, roleLines(ExcludeFromModelContext(msgs)))
}

// cutThenLateBlocked is a stored duplex history where a barge-in cut the reply
// to a spoken input whose late transcript was then blocked: the reply before
// the blocked request is the cut one, not the answer to the turn before.
func cutThenLateBlocked(next ...types.Message) []types.Message {
	return append([]types.Message{
		{Role: core.RoleUser, Content: "hello"},
		{Role: core.RoleAssistant, Content: "Hi!"},
		interruptedReply("Sure, I'll"),
		{Role: core.RoleUser, Content: "a wire transfer please", Meta: map[string]interface{}{
			types.MetaBlockedInput: true, BlockedReplyBeforeMetaKey: true,
		}},
	}, next...)
}

// The blocked turn is found before interrupted replies are taken out: done
// the other way round, the reply before the blocked request becomes the
// previous turn's answer, and that is dropped instead.
func TestExcludeFromModelContext_FindsBlockedTurnsBeforeDroppingInterrupted(t *testing.T) {
	assert.Equal(t, []string{"user:hello", "assistant:Hi!"}, roleLines(ExcludeFromModelContext(cutThenLateBlocked())))
}
