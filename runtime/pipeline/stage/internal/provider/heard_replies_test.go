package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/heard"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func contents(msgs []types.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

// The provider stage's next turn sends the model what the caller heard of a
// reply the speech-out stage cut, once that stage has handled the barge-in's
// Interrupt; and a heard prefix stays in the request (types.ExcludeInterrupted
// keeps it).
func TestProviderStage_NextTurnSeesWhatTheCallerHeard(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	hr := heard.NewHeardReplies()
	st := NewProviderStageWithTurnState(prov, nil, nil,
		&ProviderConfig{Streaming: true, HeardReplies: hr}, nil, nil, core.NewTurnState())

	input := make(chan core.StreamElement)
	output := make(chan core.StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- st.Process(context.Background(), input, output) }()
	send := func(e core.StreamElement) { within(t, "a send", func() bool { input <- e; return true }) }

	send(core.NewMessageElement(&types.Message{Role: core.RoleUser, Content: "tell me a story"}))
	send(core.NewEndOfTurnElement())
	for e := recvWithin(t, output); !e.EndOfTurn; e = recvWithin(t, output) {
	}
	send(core.NewInterruptElement()) // the caller barges in on "reply-1"
	recvWithin(t, output)

	cut := types.Message{Role: core.RoleAssistant, Content: "rep", FinishReason: types.FinishReasonInterrupted,
		Meta: map[string]interface{}{types.MetaPlayoutCut: types.PlayoutCutBargeIn, types.MetaHeardPrefix: true}}
	go func() {
		time.Sleep(20 * time.Millisecond) // the speech-out stage handles the Interrupt
		heard.Record(hr, "reply-1", &cut)
		heard.Settle(hr)
	}()
	send(core.NewMessageElement(&types.Message{Role: core.RoleUser, Content: "stop"}))
	send(core.NewEndOfTurnElement())
	close(input)
	require.NoError(t, recvWithin(t, done))

	require.Len(t, prov.requests, 2)
	var assistant []string
	for _, m := range prov.requests[1] {
		if m.Role == core.RoleAssistant {
			assistant = append(assistant, m.Content)
		}
	}
	assert.Equal(t, []string{"rep"}, assistant, "the next turn sees what was heard, not what was written")
}

// A barge-in keeps the rounds of a turn that completed: each assistant
// message whose every tool call was answered, with its results, up to the
// first round that did not complete.
func TestCompletedRounds(t *testing.T) {
	call := func(ids ...string) types.Message {
		m := types.Message{Role: core.RoleAssistant, Content: "Let me check."}
		for _, id := range ids {
			m.ToolCalls = append(m.ToolCalls, types.MessageToolCall{ID: id, Name: "t"})
		}
		return m
	}
	result := func(id string) types.Message {
		return types.Message{Role: core.RoleTool, ToolResult: &types.MessageToolResult{ID: id}}
	}
	// A scan that makes no progress loops forever: every call is bounded.
	kept := func(msgs []types.Message) []types.Message {
		return within(t, "completedRounds", func() []types.Message { return completedRounds(msgs) })
	}
	fragment := types.Message{Role: core.RoleAssistant, Content: "It is", FinishReason: types.FinishReasonInterrupted}
	text := types.Message{Role: core.RoleAssistant, Content: "Sure."}

	assert.Nil(t, kept(nil))
	assert.Nil(t, kept([]types.Message{call("c1")}), "a call with no result is not a completed round")
	assert.Nil(t, kept([]types.Message{call("c1", "c2"), result("c1")}), "every call must be answered")
	assert.Nil(t, kept([]types.Message{fragment}), "an unfinished fragment is not a round")
	assert.Equal(t, []types.Message{call("c1"), result("c1")},
		kept([]types.Message{call("c1"), result("c1"), call("c2")}), "up to the round that did not complete")
	assert.Equal(t, []types.Message{call("c1", "c2"), result("c2"), result("c1")},
		kept([]types.Message{call("c1", "c2"), result("c2"), result("c1"), fragment}))
	assert.Equal(t, []types.Message{call("c1"), result("c1"), text},
		kept([]types.Message{call("c1"), result("c1"), text}))
}
