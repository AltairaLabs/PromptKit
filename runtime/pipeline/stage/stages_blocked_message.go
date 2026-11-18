package stage

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// BlockedMessage is a guardrail firing whose message is about to replace a
// turn, as a BlockedMessageFunc is given it.
type BlockedMessage struct {
	// ValidatorType is the firing check's type ("banned_words"); empty for a
	// hook that names none.
	ValidatorType string
	// ValidatorID is the pack validator's id, when it declares one.
	ValidatorID string
	// Direction is hooks.DirectionInput or hooks.DirectionOutput.
	Direction string
	// Reason is the check's explanation of the firing. It can describe the
	// policy, so it is not shown unless the function puts it in its text.
	Reason string
	// Text is the message the guardrail chose: the validator's message, else
	// the check's own replacement, else the default blocked message.
	Text string
	// TurnIndex is the 0-based index of the turn the firing belongs to: 0 for
	// the conversation's first turn, on the text, VAD and realtime paths alike.
	TurnIndex int
	// Caller is who the conversation acts for (ProviderConfig.Caller).
	Caller types.Caller
}

// BlockedMessageFunc chooses the text of a blocked message: what replaces the
// turn, is shown to the caller, stored, and spoken by a cue. An empty return
// keeps b.Text, as does a panic. On a realtime session (DuplexProviderStage)
// it is given at most the cue stage's OnDemandBudget (1.5s by default; its ctx
// is canceled then) before b.Text is used, so it cannot stall the session's
// output.
type BlockedMessageFunc func(ctx context.Context, b BlockedMessage) string

// BlockedMessageDetail is the ValidationResult.Details key that records the
// text a BlockedMessageFunc chose for a firing, when it chose one. The
// guardrail's own replacement, if any, stays under its own key.
const BlockedMessageDetail = "blocked_message"

// blockedMessagePanicOnce logs the first panic of a BlockedMessageFunc.
var blockedMessagePanicOnce sync.Once

// blockedFiring is a firing that blocks a turn with text.
type blockedFiring struct {
	d         *hooks.Decision
	direction string
	text      string
	turn      int
	caller    types.Caller
}

// chooseBlockedText is the text firing f blocks a turn with: fn's choice, or
// f.text when fn is nil, returns "", panics, or (with timeout > 0) has not
// returned within timeout. A choice is recorded in the decision's metadata
// under BlockedMessageDetail, so a validation built from it afterwards
// carries it.
func chooseBlockedText(ctx context.Context, fn BlockedMessageFunc, timeout time.Duration, f blockedFiring) string {
	if fn == nil {
		return f.text
	}
	b := BlockedMessage{Direction: f.direction, Reason: f.d.Reason, Text: f.text, TurnIndex: f.turn, Caller: f.caller}
	b.ValidatorType, _ = f.d.Metadata["validator_type"].(string)
	if decl, ok := f.d.Metadata[hooks.MetadataKeyValidatorDeclaration].(*packspec.Validator); ok && decl != nil {
		b.ValidatorID = decl.ID
	}
	chosen := callBlockedMessage(ctx, fn, timeout, b)
	if chosen == "" {
		return f.text
	}
	// A copy: the hook that returned the decision may hold its map.
	meta := make(map[string]any, len(f.d.Metadata)+1)
	maps.Copy(meta, f.d.Metadata)
	meta[BlockedMessageDetail] = chosen
	f.d.Metadata = meta
	return chosen
}

// callBlockedMessage calls fn, "" when it panics or, with timeout > 0, when it
// has not returned within timeout (its context is then canceled).
func callBlockedMessage(ctx context.Context, fn BlockedMessageFunc, timeout time.Duration, b BlockedMessage) string {
	if timeout <= 0 {
		return safeBlockedMessage(ctx, fn, b)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	got := make(chan string, 1)
	go func() { got <- safeBlockedMessage(cctx, fn, b) }()
	select {
	case text := <-got:
		return text
	case <-cctx.Done():
		logger.Warn("blocked message: the host's function did not return in time; the guardrail's message is used",
			"timeout", timeout, "validator_type", b.ValidatorType)
		return ""
	}
}

// safeBlockedMessage is fn's text, "" when it panics.
func safeBlockedMessage(ctx context.Context, fn BlockedMessageFunc, b BlockedMessage) (text string) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			blockedMessagePanicOnce.Do(func() {
				logger.Error("blocked message: the host's function panicked; the guardrail's message is used",
					"panic", fmt.Sprint(r))
			})
		}
	}()
	return fn(ctx, b)
}

// replacedText reports whether enforced replaces original rather than keeping
// a prefix of it (a truncation, or content left unchanged): only a replacement
// is a blocked message. The same rule decides whether a streamed reply is
// judged again on completion.
func replacedText(original, enforced string) bool {
	return !strings.HasPrefix(original, enforced)
}
