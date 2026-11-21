package sdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ErrInvalidHistory is returned by [Conversation.ReplaceHistory] and by Send
// or Stream with [WithHistory] for a history they refuse. The error text says
// which message is wrong and why.
var ErrInvalidHistory = errors.New("invalid conversation history")

// roleToolMessage is the role of a message carrying a tool's result.
const roleToolMessage = "tool"

// Default limits on a replaced history (WithMaxHistorySize).
const (
	defaultMaxHistoryMessages = 1000
	defaultMaxHistoryBytes    = 16 << 20
)

// WithHistory replaces the conversation's history with msgs before the turn
// this Send or Stream starts, as [Conversation.ReplaceHistory] does, with no
// other turn able to run between the two. The new message follows msgs. A
// history it refuses fails the Send before the turn runs, and nothing is
// replaced.
func WithHistory(msgs []types.Message) SendOption {
	return func(c *sendConfig) error {
		c.history = msgs
		c.historySet = true
		return nil
	}
}

// ReplaceHistory replaces the conversation's history with msgs: the next turn
// sends the model msgs, then its own new message, after the pack's system
// prompt. An empty msgs clears the history, as Clear does. To replace it and
// run the turn in one step, so no other turn can run between them, pass
// [WithHistory] to Send or Stream instead.
//
// It is for a host whose client keeps the conversation, as an AG-UI client
// does when it sends the whole history with each run. Accepting a client's
// history is a trust decision: the client can put any words in the user's,
// the assistant's or a tool's mouth, including replies the model never gave
// and tool results no tool returned, and the conversation's input guardrails
// judge only each new message, never one a replaced history adds. Use it only
// where the client is entitled to that, and never with history from a client
// that may not see or edit the conversation.
//
// The conversation's own copy wins over a client's for every message it
// holds. A message of msgs is taken as the conversation stores it when it has
// the same id (types.MetaMessageID) as a stored message, when it is the same
// message (role, text, tool calls, tool result) at the same position as a
// stored one while msgs repeats the stored history from its start, or when it
// is the same message as one the conversation keeps out of model context. The
// stored copy keeps everything the client's lacks: its metadata, cost,
// validations, the provider's tool-call metadata (such as Gemini's thought
// signatures) and media tool results. What the conversation keeps out of
// model context stays out: an interrupted reply, and a turn an input
// guardrail blocked, which is marked types.MetaBlockedInput where the
// client's order would let it in. Any other message is new to the
// conversation and is stored as given.
//
// The history is checked first, and nothing is replaced when it fails, with
// an error matching [ErrInvalidHistory]:
//   - A message's role is user, assistant or tool. A system message is
//     refused: the pack's system prompt is the only system instruction, and a
//     client's history cannot add one or replace it. (agui.MessagesFromAGUI
//     reads an AG-UI developer message as a system message, so it is refused
//     too.)
//   - Of the messages the model is given, a tool message carries a result
//     (ToolResult) answering a call the latest assistant message made, once;
//     every call has its result before the next user or assistant message,
//     and before the history ends.
//   - No message is larger than the conversation's maximum message size, and
//     the history holds at most 1000 messages and 16 MiB in all (see
//     WithMaxHistorySize). A message's size counts its text, its media's
//     inline data, its tool calls' arguments and its tool result's parts.
//
// A new message is stored without its reasoning (the conversation did not
// produce it, so it is not replayed to the provider), and given a stable id
// when it has none. A tool result without a name takes the name of the call
// it answers.
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
	return c.replaceHistoryLocked(ctx, msgs)
}

// replaceHistoryLocked replaces the history; the caller holds the turn.
func (c *Conversation) replaceHistoryLocked(ctx context.Context, msgs []types.Message) error {
	if err := c.checkHistorySize(msgs); err != nil {
		return err
	}
	stored, err := c.getBaseSession().Messages(ctx)
	if err != nil {
		return fmt.Errorf("replace history: loading the conversation: %w", err)
	}
	history, err := checkHistory(reconcileHistory(stored, msgs))
	if err != nil {
		return err
	}
	if saveErr := c.saveHistory(ctx, history); saveErr != nil {
		return saveErr
	}
	c.discardAbandonedResolutions()
	return nil
}

// startTurnInput applies a Send's WithHistory and WithClientTools before its
// turn runs; the caller holds the turn. The tools are checked first, so a
// refused tool replaces no history.
func (c *Conversation) startTurnInput(ctx context.Context, cfg *sendConfig) error {
	offered, err := c.checkClientTools(cfg.clientTools)
	if err != nil {
		return err
	}
	if cfg.historySet {
		if histErr := c.replaceHistoryLocked(ctx, cfg.history); histErr != nil {
			return histErr
		}
	}
	c.setTurnTools(offered)
	return nil
}

// checkHistorySize holds a history to the message size limit and the
// history limits.
func (c *Conversation) checkHistorySize(msgs []types.Message) error {
	maxMessages, maxBytes := defaultMaxHistoryMessages, defaultMaxHistoryBytes
	if c.config != nil && c.config.maxHistoryMessages > 0 {
		maxMessages, maxBytes = c.config.maxHistoryMessages, c.config.maxHistoryBytes
	}
	if len(msgs) > maxMessages {
		return fmt.Errorf("%w: %d messages, at most %d", ErrInvalidHistory, len(msgs), maxMessages)
	}
	total := 0
	for i := range msgs {
		size := messageContentSize(&msgs[i])
		if err := c.validateMessageSize(size); err != nil {
			return historyError(i, "%v", err)
		}
		total += size
	}
	if total > maxBytes {
		return fmt.Errorf("%w: %d bytes, at most %d", ErrInvalidHistory, total, maxBytes)
	}
	return nil
}

// historyEntry is a message of a replaced history, and whether it must stay
// out of model context because the conversation keeps its stored copy out.
type historyEntry struct {
	msg      types.Message
	stored   bool
	excluded bool
}

// reconcileHistory takes each message of msgs as the conversation stores it
// where it has a copy (see ReplaceHistory), and as given otherwise.
func reconcileHistory(stored, msgs []types.Message) []historyEntry {
	excluded := stage.ModelContextMask(stored)
	byID := make(map[string]int, len(stored))
	for i := range stored {
		if id := stored[i].ID(); id != "" {
			byID[id] = i
		}
	}
	used := make([]bool, len(stored))
	out := make([]historyEntry, 0, len(msgs))
	prefix := true
	for i := range msgs {
		j := storedCopy(stored, excluded, used, byID, &msgs[i], i, prefix)
		prefix = prefix && j == i
		if j < 0 {
			out = append(out, historyEntry{msg: msgs[i]})
			continue
		}
		used[j] = true
		out = append(out, historyEntry{msg: stored[j], stored: true, excluded: masked(excluded, j)})
	}
	return out
}

// storedCopy is the position of the stored copy of msg, the i'th message of
// a replaced history, or -1: the stored message with its id, the stored
// message at its position while the history repeats the stored one, or a
// stored message kept out of model context that is the same message.
func storedCopy(
	stored []types.Message, excluded, used []bool, byID map[string]int, msg *types.Message, i int, prefix bool,
) int {
	if j, ok := byID[msg.ID()]; ok && msg.ID() != "" && !used[j] && stored[j].Role == msg.Role {
		return j
	}
	if prefix && i < len(stored) && !used[i] && sameMessage(&stored[i], msg) {
		return i
	}
	for j := range stored {
		if masked(excluded, j) && !used[j] && sameMessage(&stored[j], msg) {
			return j
		}
	}
	return -1
}

// sameMessage reports whether two messages say the same thing: role, text,
// tool calls and the call a tool result answers.
func sameMessage(a, b *types.Message) bool {
	if a.Role != b.Role || a.GetContent() != b.GetContent() || len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for k := range a.ToolCalls {
		if a.ToolCalls[k].ID != b.ToolCalls[k].ID || a.ToolCalls[k].Name != b.ToolCalls[k].Name {
			return false
		}
	}
	return resultID(a) == resultID(b)
}

func resultID(m *types.Message) string {
	if m.ToolResult == nil {
		return ""
	}
	return m.ToolResult.ID
}

// checkHistory validates a reconciled history and returns it as it is
// stored: an excluded message the new order no longer keeps out of model
// context is marked blocked, and new messages lose their reasoning and get an
// id.
func checkHistory(entries []historyEntry) ([]types.Message, error) {
	out := make([]types.Message, len(entries))
	for i := range entries {
		m := entries[i].msg
		if m.Role != roleUser && m.Role != roleAssistant && m.Role != roleToolMessage {
			return nil, historyError(i, "role %q is not allowed: the pack's system prompt is the only system instruction",
				m.Role)
		}
		if !entries[i].stored {
			m.Source = ""
			m.Reasoning = nil
			m = types.EnsureMessageID(m)
		}
		out[i] = m
	}
	keepExcluded(out, entries)
	mask := stage.ModelContextMask(out)
	open := map[string]string{} // unanswered calls of the latest assistant message: id -> name
	for i := range out {
		if masked(mask, i) {
			continue
		}
		if err := checkPairing(i, &out[i], open); err != nil {
			return nil, err
		}
	}
	if id := firstOpenCall(open); id != "" {
		return nil, historyError(len(out)-1, "tool call %q has no result", id)
	}
	return out, nil
}

// keepExcluded marks blocked each message whose stored copy is kept out of
// model context but which the new history's order would let back in: a
// blocked user message the client moved away from its canned reply, say.
func keepExcluded(out []types.Message, entries []historyEntry) {
	mask := stage.ModelContextMask(out)
	for i := range out {
		if !entries[i].excluded || masked(mask, i) {
			continue
		}
		meta := make(map[string]interface{}, len(out[i].Meta)+1)
		for k, v := range out[i].Meta {
			meta[k] = v
		}
		meta[types.MetaBlockedInput] = true
		out[i].Meta = meta
	}
}

// checkPairing checks message i against the calls open before it, and
// updates them.
func checkPairing(i int, m *types.Message, open map[string]string) error {
	switch m.Role {
	case roleToolMessage:
		return answerCall(i, m, open)
	case roleAssistant:
		if id := firstOpenCall(open); id != "" {
			return historyError(i, "tool call %q has no result", id)
		}
		return openCalls(i, m, open)
	default:
		if id := firstOpenCall(open); id != "" {
			return historyError(i, "tool call %q has no result", id)
		}
		return nil
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

// masked reports whether mask marks position i; a nil mask marks none.
func masked(mask []bool, i int) bool {
	return i < len(mask) && mask[i]
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
