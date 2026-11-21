package sdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ErrInvalidHistory is returned by [Conversation.ReplaceHistory] for a history
// it refuses. The error text says which message is wrong and why.
var ErrInvalidHistory = errors.New("invalid conversation history")

// roleToolMessage is the role of a message carrying a tool's result.
const roleToolMessage = "tool"

// ReplaceHistory replaces the conversation's history with msgs: the next turn
// sends the model msgs, then its own new message, after the pack's system
// prompt. An empty msgs clears the history, as Clear does.
//
// It is for a host whose client keeps the conversation, as an AG-UI client
// does when it sends the whole history with each run. Accepting a client's
// history is a trust decision: the client can put any words in the user's,
// the assistant's or a tool's mouth, including replies the model never gave
// and tool results no tool returned. Use it only where the client is entitled
// to that, and never with history from a client that may not see or edit the
// conversation.
//
// The history is checked first, and nothing is replaced when it fails, with
// an error matching [ErrInvalidHistory]:
//   - A message's role is user, assistant or tool. A system message is
//     refused: the pack's system prompt is the only system instruction, and a
//     client's history cannot add one or replace it. (agui.MessagesFromAGUI
//     reads an AG-UI developer message as a system message, so it is refused
//     too.)
//   - A tool message carries a result (ToolResult) answering a call the latest
//     assistant message made, once; every call has its result before the next
//     user or assistant message, and before the history ends.
//   - No message is larger than the conversation's maximum message size.
//
// Each message is stored as given, except that its reasoning is dropped (the
// conversation did not produce it, so it is not replayed to the provider) and
// a message without a stable id is given one (types.MetaMessageID). A tool
// result without a name takes the name of the call it answers.
//
// The new history replaces the old in the conversation's state store, with
// the store's summaries of the old history, which do not describe the new; the
// conversation's metadata is kept. A store that cannot write a whole
// conversation (one without statestore.BulkWriter) cannot replace it. Answers
// recorded for a suspended turn are discarded, as when Send starts a new
// turn, and a call held for approval is not awaited from then on. A message
// index (WithMessageIndex) is not rebuilt: its retrieval still finds the old
// history's messages.
//
// It waits for a running turn to end, and fails for a duplex conversation and
// one that is closed.
func (c *Conversation) ReplaceHistory(ctx context.Context, msgs []types.Message) error {
	release, err := c.turn.acquire(ctx, c)
	if err != nil {
		return err
	}
	defer release()

	if stateErr := c.validateSendState(); stateErr != nil {
		return stateErr
	}
	history, err := c.checkHistory(msgs)
	if err != nil {
		return err
	}
	if saveErr := c.saveHistory(ctx, history); saveErr != nil {
		return saveErr
	}
	c.discardAbandonedResolutions()
	return nil
}

// checkHistory validates a ReplaceHistory history and returns it as it is
// stored.
func (c *Conversation) checkHistory(msgs []types.Message) ([]types.Message, error) {
	out := make([]types.Message, 0, len(msgs))
	open := map[string]string{} // unanswered calls of the latest assistant message: id -> name
	for i := range msgs {
		m := msgs[i]
		if err := c.checkHistoryMessage(i, &m, open); err != nil {
			return nil, err
		}
		m.Source = ""
		m.Reasoning = nil
		out = append(out, types.EnsureMessageID(m))
	}
	if id := firstOpenCall(open); id != "" {
		return nil, historyError(len(msgs)-1, "tool call %q has no result", id)
	}
	return out, nil
}

// checkHistoryMessage checks message i of a ReplaceHistory history against
// the calls open before it, and updates them.
func (c *Conversation) checkHistoryMessage(i int, m *types.Message, open map[string]string) error {
	if err := c.validateMessageSize(messageContentSize(m)); err != nil {
		return historyError(i, "%v", err)
	}
	switch m.Role {
	case roleUser, roleAssistant:
		if id := firstOpenCall(open); id != "" {
			return historyError(i, "tool call %q has no result", id)
		}
		if m.Role == roleAssistant {
			return openCalls(i, m, open)
		}
		return nil
	case roleToolMessage:
		return answerCall(i, m, open)
	default:
		return historyError(i, "role %q is not allowed: the pack's system prompt is the only system instruction", m.Role)
	}
}

// openCalls records the calls assistant message i makes.
func openCalls(i int, m *types.Message, open map[string]string) error {
	for _, tc := range m.ToolCalls {
		if tc.ID == "" {
			return historyError(i, "tool call %q has no id", tc.Name)
		}
		if _, dup := open[tc.ID]; dup {
			return historyError(i, "tool call id %q is used twice", tc.ID)
		}
		open[tc.ID] = tc.Name
	}
	return nil
}

// answerCall checks that tool message i answers an open call, closes it, and
// names the result after the call when it has no name.
func answerCall(i int, m *types.Message, open map[string]string) error {
	if m.ToolResult == nil || m.ToolResult.ID == "" {
		return historyError(i, "a tool message must carry the result of a call")
	}
	name, ok := open[m.ToolResult.ID]
	if !ok {
		return historyError(i, "tool result %q answers no open call of the latest assistant message",
			m.ToolResult.ID)
	}
	delete(open, m.ToolResult.ID)
	if m.ToolResult.Name == "" {
		result := *m.ToolResult
		result.Name = name
		m.ToolResult = &result
	}
	return nil
}

// firstOpenCall names an unanswered call, or "" when there is none. Any one
// will do for the error, so it is the smallest id, for a stable message.
func firstOpenCall(open map[string]string) string {
	first := ""
	for id := range open {
		if first == "" || id < first {
			first = id
		}
	}
	return first
}

func historyError(i int, format string, args ...any) error {
	return fmt.Errorf("%w: message %d: %s", ErrInvalidHistory, i, fmt.Sprintf(format, args...))
}

// saveHistory writes history as the conversation's messages, keeping the rest
// of its stored state except the summaries.
func (c *Conversation) saveHistory(ctx context.Context, history []types.Message) error {
	store := c.sessionStore
	if store == nil && c.config != nil {
		store = c.config.stateStore
	}
	writer, ok := store.(statestore.BulkWriter)
	if !ok {
		return errors.New("replace history: the state store cannot replace a conversation (no BulkWriter)")
	}
	id := c.getBaseSession().ID()
	state, err := store.Load(ctx, id)
	switch {
	case errors.Is(err, statestore.ErrNotFound) || (err == nil && state == nil):
		state = &statestore.ConversationState{ID: id, Metadata: map[string]any{}}
	case err != nil:
		return fmt.Errorf("replace history: loading the conversation: %w", err)
	}
	if c.config != nil && state.UserID == "" {
		state.UserID = c.config.userID
	}
	state.Messages = history
	state.Summaries = nil
	if err := writer.Save(ctx, state); err != nil {
		return fmt.Errorf("replace history: %w", err)
	}
	return nil
}
