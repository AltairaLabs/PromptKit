package sdk

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// BlockedMessage is a guardrail firing whose message is about to replace a
// turn, as a WithBlockedMessage callback is given it.
type BlockedMessage struct {
	// ValidatorType is the firing check's type ("banned_words"); empty for a
	// hook that names none.
	ValidatorType string
	// ValidatorID is the pack validator's id, when it declares one.
	ValidatorID string
	// Direction is "input" (the caller's turn was blocked) or "output" (the
	// reply was).
	Direction string
	// Reason is the check's explanation of the firing, such as an LLM judge's
	// reasoning. It can describe the policy, so the caller never sees it unless
	// the callback puts it in its text.
	Reason string
	// Text is the message the guardrail chose: the validator's message, else
	// the check's own replacement, else the default blocked message.
	Text string
	// TurnIndex is the 0-based index of the turn the firing belongs to: 0 for
	// the conversation's first turn, on the text, VAD and realtime paths alike.
	TurnIndex int
	// Caller is who the conversation acts for (WithCaller).
	Caller types.Caller
	// ConversationID is the conversation's ID.
	ConversationID string
}

// WithBlockedMessage sets a callback that chooses the text of a blocked
// message: a localized string, one that includes the firing's Reason, or
// b.Text unchanged. The text it returns replaces the blocked turn: the caller
// sees it, the conversation stores it, and in a realtime voice session the cue
// stage speaks it. An empty return keeps b.Text.
//
// It is called on the text path (Send, Stream), in VAD mode and in a realtime
// session, wherever a guardrail blocks a turn with a message: an input block,
// a streamed reply cut part-way, and a completed reply. A length guardrail
// that truncates a reply, or enforces without changing it, blocks with no
// message and does not call it. A callback that panics keeps b.Text. On the
// text path and in VAD mode it runs on the turn's goroutine, so a slow
// callback holds the turn; in a realtime session it is given at most the cue
// stage's on-demand budget (1.5s), after which its ctx is canceled and b.Text
// is used. The firing's validation records the text it chose in Details
// under "blocked_message" (stage.BlockedMessageDetail). A spoken text
// that differs from the pack's messages is rendered when first needed, within
// the cue stage's on-demand budget; past it the earcon plays, with the text
// shown, and the clip is kept for the next time.
func WithBlockedMessage(fn func(ctx context.Context, b BlockedMessage) string) Option {
	return func(c *config) error {
		c.blockedMessage = fn
		return nil
	}
}

// blockedMessageFunc is the runtime form of the WithBlockedMessage callback,
// adding the conversation's ID; nil when none is set.
func (c *Conversation) blockedMessageFunc() stage.BlockedMessageFunc {
	fn := c.config.blockedMessage
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, b stage.BlockedMessage) string {
		return fn(ctx, BlockedMessage{
			ValidatorType:  b.ValidatorType,
			ValidatorID:    b.ValidatorID,
			Direction:      b.Direction,
			Reason:         b.Reason,
			Text:           b.Text,
			TurnIndex:      b.TurnIndex,
			Caller:         b.Caller,
			ConversationID: c.ID(),
		})
	}
}
